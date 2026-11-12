//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

const upstream021PageBody = `{"model":"gpt-5.1","input":[{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","output":"{\"page_id\":\"page_1\",\"text\":\"audit page text\"}","future_sibling":{"opaque":true}}],"future_options":{"opaque":true}}`

func TestUpstream021PageSnapshotCanonicalAudits(t *testing.T) {
	const expected = "<external_codex_apps_open_page>\n{\"page_id\":\"page_1\",\"text\":\"audit page text\"}\n</external_codex_apps_open_page>"
	for _, frame := range []string{"http", "websocket", "websocket_nested"} {
		body := upstream021PageBody
		if frame == "websocket" {
			body = `{"type":"response.create",` + body[1:]
		}
		if frame == "websocket_nested" {
			body = `{"type":"response.create","response":` + body + `}`
		}
		input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, []byte(body))
		// Moderation folds whitespace under its documented text normalization.
		require.Equal(t, `<external_codex_apps_open_page> {"page_id":"page_1","text":"audit page text"} </external_codex_apps_open_page>`, input.Text)
		snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIResponses, Body: []byte(body)})
		require.NoError(t, err)
		require.Equal(t, expected, snapshot.ScanText)
	}
	// Anchored tool output remains context, even when its name resembles page injection.
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(upstream021PageBody), &body))
	body["input"].([]any)[0].(map[string]any)["call_id"] = "call_real"
	paired, err := json.Marshal(body)
	require.NoError(t, err)
	require.Empty(t, service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, paired).Text)
	snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIResponses, Body: paired})
	require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
	require.Empty(t, snapshot.ScanText)
}

func TestUpstream021PageSnapshotRealAuditsBeforeSideEffects(t *testing.T) {
	fixture := []byte(upstream021PageBody)
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			for _, websocket := range []bool{false, true} {
				for _, recognized := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/websocket=%t/recognized=%t", kind, engine, websocket, recognized), func(t *testing.T) {
						body := fixture
						scans := 1
						if !recognized {
							var err error
							body, err = sjson.SetRawBytes(body, "input.0", []byte(`{"type":"future_item","opaque":true}`))
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
							require.Contains(t, observed[0], "audit page text")
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
