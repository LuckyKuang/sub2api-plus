package handler

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesWebSocket_IngressLeaseFollowsPromptAudit(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		name := "allowed"
		if blocked {
			name = "blocked"
		}
		t.Run(name, func(t *testing.T) {
			engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, captureSnapshot: true}
			if blocked {
				engine.decisions = []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionBlock}}
			}
			var acquiredAfterAudit atomic.Bool
			cache := &concurrencyCacheMock{
				acquireIngressLeaseFn: func(context.Context, int64, int, string) (bool, error) {
					acquiredAfterAudit.Store(engine.evaluates.Load() == 1 && strings.Contains(engine.capturedScanText(), "audit this first"))
					return true, nil
				},
			}
			h := newOpenAIHandlerForPreviousResponseIDValidation(t, cache)
			h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
			h.cfg = &config.Config{}
			h.cfg.Gateway.OpenAIWS.MaxIngressConnectionsPerAPIKey = 1
			server := newOpenAIWSHandlerTestServer(t, h, middleware.AuthSubject{UserID: 1, Concurrency: 1})
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			require.Zero(t, atomic.LoadInt32(&cache.acquireIngressCalled))
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"audit this first"}]}],"future_option":true}`)))
			for {
				_, _, err = conn.Read(ctx)
				if err != nil {
					break
				}
			}
			require.NotErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, int64(1), engine.evaluates.Load())
			require.Contains(t, engine.capturedScanText(), "audit this first")
			if blocked {
				require.Zero(t, atomic.LoadInt32(&cache.acquireIngressCalled))
				require.Zero(t, atomic.LoadInt32(&cache.releaseIngressCalled))
			} else {
				require.True(t, acquiredAfterAudit.Load())
				require.Equal(t, int32(1), atomic.LoadInt32(&cache.acquireIngressCalled))
				// The user slot is denied by this fixture. Even that early return
				// must release the successfully acquired connection lease.
				require.Eventually(t, func() bool { return atomic.LoadInt32(&cache.releaseIngressCalled) == 1 }, time.Second, 10*time.Millisecond)
			}
		})
	}
}
