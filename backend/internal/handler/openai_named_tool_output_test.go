//go:build unit || !integration

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Official context-fragments/src/additional_context.rs at Codex commit
// 6d8f311b00e823052622eac2b463919ad86a666d emits this item before user input.
const codexNamedOutputAuditInput = `[{"type":"function_call_output","namespace":"agent","name":"notifications","output":"untrusted external context"},{"type":"message","id":"msg_input_8c8317ec-e000-4eef-8a92-72c18e1cdbae","role":"user","content":[{"type":"input_text","text":"audit current request"},{"type":"future_media","opaque":true}]}]`

func TestCodexNamedToolOutputHTTPValidation(t *testing.T) {
	for _, test := range []struct {
		name, input string
		allowed     bool
	}{
		{"official context", codexNamedOutputAuditInput, true},
		{"protocol namespace", `[{"type":"function_call_output","namespace":"slack","name":"notifications","output":"Alice mentioned you."}]`, true},
		{"ordinary output", `[{"type":"function_call_output","output":"result"}]`, false},
		{"blank paired call", `[{"type":"function_call_output","call_id":"","namespace":"agent","name":"notifications","output":"result"}]`, false},
		{"uncovered paired sibling", `[{"type":"function_call_output","namespace":"agent","name":"notifications","output":"context"},{"type":"function_call_output","call_id":"call_1","output":"result"}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			h := &OpenAIGatewayHandler{}
			require.Equal(t, test.allowed, h.validateFunctionCallOutputRequest(c,
				[]byte(`{"model":"gpt-5.1","input":`+test.input+`}`), zap.NewNop()))
			if test.allowed {
				require.False(t, c.Writer.Written())
			} else {
				require.Equal(t, http.StatusBadRequest, c.Writer.Status())
			}
		})
	}
}

func TestCodexNamedToolOutputCanonicalAuditAcrossTransports(t *testing.T) {
	for _, transport := range []string{"http", "websocket", "nested_websocket"} {
		t.Run(transport, func(t *testing.T) {
			body := `{"model":"gpt-5.1","input":` + codexNamedOutputAuditInput + `}`
			switch transport {
			case "websocket":
				body = `{"type":"response.create",` + body[1:]
			case "nested_websocket":
				body = `{"type":"response.create","response":` + body + `}`
			}
			moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, []byte(body))
			require.Equal(t, "audit current request", moderation.Text)
			snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: service.ContentModerationProtocolOpenAIResponses, Body: []byte(body), Stage: transport,
			})
			require.NoError(t, err)
			require.Equal(t, "audit current request", snapshot.ScanText)
			require.NotContains(t, snapshot.ScanText, "untrusted external context")
		})
	}
}

func TestCodexNamedToolOutputAuditBlocksHTTPBeforeSideEffects(t *testing.T) {
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, captureSnapshot: true,
		decisions: []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionBlock}}}
	var acquired atomic.Int32
	cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
		acquired.Add(1)
		return false, errors.New("unexpected pre-audit concurrency")
	}}
	h := newOpenAIHandlerForPreviousResponseIDValidation(t, cache)
	h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
	h.cfg = &config.Config{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gpt-5.1","input":`+codexNamedOutputAuditInput+`}`))
	c.Request.Header.Set("Content-Type", "application/json")
	groupID := int64(3)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}, User: &service.User{ID: 7, Concurrency: 1}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
	h.Responses(c)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
	require.Equal(t, "audit current request", engine.capturedScanText())
	require.Zero(t, acquired.Load())
	_, selected := c.Get(opsAccountIDKey)
	require.False(t, selected, "neither OAuth nor API-key selection may precede the audit gate")
}

func TestCodexNamedToolOutputWebSocketAuditPrecedesIngressAdmission(t *testing.T) {
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
			var admittedAfterAudit atomic.Bool
			cache := &concurrencyCacheMock{
				acquireIngressLeaseFn: func(context.Context, int64, int, string) (bool, error) {
					admittedAfterAudit.Store(engine.evaluates.Load() == 1 && engine.capturedScanText() == "audit current request")
					return true, nil
				},
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, errors.New("stop after audited ingress admission")
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
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":`+codexNamedOutputAuditInput+`}`)))
			for {
				_, _, err = conn.Read(ctx)
				if err != nil {
					break
				}
			}
			require.NotErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, int64(1), engine.evaluates.Load())
			require.Equal(t, "audit current request", engine.capturedScanText())
			if blocked {
				require.Zero(t, atomic.LoadInt32(&cache.acquireIngressCalled))
			} else {
				require.True(t, admittedAfterAudit.Load())
				require.Equal(t, int32(1), atomic.LoadInt32(&cache.acquireIngressCalled))
			}
		})
	}
}

func TestCodexNamedToolOutputWebSocketPassesToProvider(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: `{"type":"response.create","model":"gpt-5.1","input":` + codexNamedOutputAuditInput + `}`,
				ingressMode:  mode,
			})
			require.Len(t, got.upstreamPayloads, 1)
			payload := got.upstreamPayloads[0]
			require.Equal(t, "function_call_output", gjson.GetBytes(payload, "input.0.type").String())
			require.Equal(t, "agent", gjson.GetBytes(payload, "input.0.namespace").String())
			require.Equal(t, "notifications", gjson.GetBytes(payload, "input.0.name").String())
			require.Equal(t, "untrusted external context", gjson.GetBytes(payload, "input.0.output").String())
			require.False(t, gjson.GetBytes(payload, "input.0.call_id").Exists())
		})
	}
}
