//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

// The official wrapped WebSocket error maps status/status_code to an HTTP
// error before parsing terminal provider codes. Only 400 (or no status) may
// interpret budget/audio codes as deterministic input failures.
func TestOpenAIInputTerminalEventRespectsExplicitHTTPStatus(t *testing.T) {
	for _, code := range officialCodexInputTerminalCodes {
		for _, path := range []string{"status", "status_code", "error.status", "error.status_code", "response.error.status", "response.error.status_code"} {
			for _, status := range []int{400, 401, 403, 404, 426, 429, 500, 503} {
				t.Run(fmt.Sprintf("%s/%s/%d", code, path, status), func(t *testing.T) {
					body := []byte(fmt.Sprintf(`{"type":"error","error":{"code":%q,"message":"Request rejected"}}`, code))
					if path == "response.error.status" || path == "response.error.status_code" {
						body = []byte(fmt.Sprintf(`{"type":"response.failed","response":{"status":"failed","error":{"code":%q,"message":"Request rejected"}}}`, code))
					}
					body, err := sjson.SetBytes(body, path, status)
					require.NoError(t, err)
					want := ""
					if status == 400 {
						want = code
					}
					require.Equal(t, want, openAIInputTerminalCode(0, body))
					_, terminal := openAIRequestTerminalEventStatus(body)
					require.Equal(t, status == 400, terminal)
					// The actual HTTP rejection status remains authoritative over
					// optional declarations inside its error body.
					require.Equal(t, code, openAIInputTerminalCode(400, body))
					require.Empty(t, openAIInputTerminalCode(401, body))
				})
			}
		}
	}
}

func TestOpenAIInputTerminalWrappedStatusKeepsWebSocketErrorPolicy(t *testing.T) {
	for _, code := range officialCodexInputTerminalCodes {
		for _, path := range []string{"status", "status_code", "error.status", "error.status_code"} {
			for _, status := range []int{401, 403, 426, 429, 500, 503} {
				t.Run(fmt.Sprintf("%s/%s/%d", code, path, status), func(t *testing.T) {
					// Misleading type/message must not suppress a real status or
					// manufacture rate limits for a non-429 wrapped failure.
					body := []byte(fmt.Sprintf(`{"type":"error","error":{"code":%q,"type":"rate_limit_error","message":"rate limit exceeded; please retry"}}`, code))
					body, err := sjson.SetBytes(body, path, status)
					require.NoError(t, err)
					require.Equal(t, status, openAIWSErrorHTTPStatus(body))
					codeRaw, errType, message := parseOpenAIWSErrorEventFields(body)
					require.Equal(t, status == 429, isOpenAIWSRateLimitEvent(body, codeRaw, errType, message))
					reason, retry := classifyOpenAIWSErrorEvent(body)
					if status == 426 {
						require.Equal(t, "upgrade_required", reason)
						require.True(t, retry)
					} else if status >= 500 {
						require.Equal(t, "upstream_error_event", reason)
						require.True(t, retry)
						require.Equal(t, status, openAIWSPayloadTransientStatus(body))
					} else {
						require.False(t, retry)
						require.Equal(t, status, openAIStreamFailureStatus(body, message))
					}
				})
			}
		}
	}
}

func TestOpenAIInputTerminalWrappedStatusForwarding(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, code := range officialCodexInputTerminalCodes {
			for _, status := range []int{401, 426, 429} {
				for _, path := range []string{"status", "status_code"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", kind, code, path, status), func(t *testing.T) {
						body := []byte(fmt.Sprintf(`{"type":"error","error":{"code":%q,"message":"Request rejected"}}`, code))
						body, err := sjson.SetBytes(body, path, status)
						require.NoError(t, err)
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
						cfg := newOpenAIWSV2TestConfig()
						cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
						conn := &openAIWSCaptureConn{events: [][]byte{body}}
						pool := newOpenAIWSConnPool(cfg)
						pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: conn})
						t.Cleanup(pool.Close)
						repo := &rateLimit429AccountRepoStub{}
						upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fallback\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}}
						svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool, rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
						svc.rateLimitService.SetAccountRuntimeBlocker(svc)
						account := &Account{ID: 5889, Platform: PlatformOpenAI, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
						_, err = svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-6.1-sol","stream":false,"input":"hello"}`))
						if status == 426 {
							require.NoError(t, err)
							require.Equal(t, 200, recorder.Code)
							require.Len(t, upstream.requests, 1, "426 must reach HTTP without replaying the WS turn")
							require.Equal(t, "codex_cli_rs/0.167.0 (Ubuntu 24.04; x86_64) xterm-256color", upstream.lastReq.UserAgent())
							require.Equal(t, "codex_cli_rs", upstream.lastReq.Header.Get("Originator"))
							require.Equal(t, "0.167.0", upstream.lastReq.Header.Get("Version"))
						} else {
							require.Error(t, err)
							require.Equal(t, status, recorder.Code)
							require.Empty(t, upstream.requests, "401/429 cannot authorize an HTTP protocol fallback")
						}
						require.False(t, isOpenAIRequestTerminalError(err), "wrapped status is account/transport evidence, not an input rejection")
						require.Len(t, conn.writes, 1)
						if status == 429 {
							if kind == AccountTypeAPIKey {
								require.Equal(t, 1, repo.rateLimitCalls, "API-key cooldown must not be suppressed by an audio/budget code")
							} else {
								require.Zero(t, repo.rateLimitCalls, "OAuth keeps its bounded same-owner retry window")
								require.True(t, svc.shouldRetryOpenAIOAuth429OnSameAccount(account, 429, false))
							}
						}
					})
				}
			}
		}
	}
}
