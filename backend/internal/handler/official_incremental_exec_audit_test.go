//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestCodexIncrementalExecCanonicalAudits(t *testing.T) {
	body, err := os.ReadFile("../testdata/official_agents/codex_incremental_exec.json")
	require.NoError(t, err)
	for _, frame := range []string{"http", "websocket", "websocket_nested"} {
		payload := body
		if frame == "websocket" {
			payload, err = sjson.SetBytes(body, "type", "response.create")
			require.NoError(t, err)
		} else if frame == "websocket_nested" {
			payload = []byte(`{"type":"response.create","response":` + string(body) + `}`)
		}
		t.Run(frame, func(t *testing.T) {
			moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, payload)
			require.Equal(t, "audit current request", moderation.Text)
			prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIResponses, Body: payload})
			require.NoError(t, err)
			require.Equal(t, "audit current request", prompt.ScanText, "developer catalogs and historical user turns are context")
		})
	}
}

func TestCodexIncrementalExecRealAuditsBeforeSideEffects(t *testing.T) {
	fixture, err := os.ReadFile("../testdata/official_agents/codex_incremental_exec.json")
	require.NoError(t, err)
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			for _, websocket := range []bool{false, true} {
				for _, recognized := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/websocket=%t/recognized=%t", kind, engine, websocket, recognized), func(t *testing.T) {
						body := fixture
						scans := 1
						if !recognized {
							body, err = sjson.SetRawBytes(body, "input.5", []byte(`{"type":"future_item","opaque":true}`))
							require.NoError(t, err)
							scans = 0
						}
						coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, scans)
						account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: kind, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture-oauth", "api_key": "fixture-api-key"}}
						h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
						boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) {
							boundary.userSlots.Add(1)
							return false, nil // stop admitted unknown content before selection/provider work
						}
						if websocket {
							server := newOpenAIWSHandlerTestServer(t, h, middleware.AuthSubject{UserID: 1, Concurrency: 1})
							defer server.Close()
							ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
							defer cancel()
							conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
							require.NoError(t, err)
							defer func() { _ = conn.CloseNow() }()
							body, err = sjson.SetBytes(body, "type", "response.create")
							require.NoError(t, err)
							require.NoError(t, conn.Write(ctx, coderws.MessageText, body))
							for {
								_, _, err = conn.Read(ctx)
								if err != nil {
									break
								}
							}
							require.NotErrorIs(t, err, context.DeadlineExceeded)
						} else {
							c, w := codexCLIIngressContext(string(body), true)
							c.Request.URL.Path = "/v1/responses"
							h.Responses(c)
							if recognized {
								require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
							} else {
								require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
							}
						}
						observed := supplier.snapshot()
						require.Len(t, observed, scans)
						if recognized {
							require.Contains(t, observed[0], "audit current request")
							require.NotContains(t, observed[0], "Updated tool definition")
							assertCodexCLIIngressNoSideEffects(t, boundary)
						} else {
							require.Equal(t, int32(1), boundary.userSlots.Load(), "unknown content must pass audit and reach admission")
							require.Zero(t, boundary.repositoryReads.Load())
							require.Zero(t, boundary.accountSlots.Load())
							require.Empty(t, boundary.wire.calls())
						}
					})
				}
			}
		}
	}
}
