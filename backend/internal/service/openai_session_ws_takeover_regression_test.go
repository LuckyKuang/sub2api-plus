//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// V12/V13/V18/V20/V23: cross-auth migration uses the actual ingress socket and
// final upstream handshake/frame (ctx_pool), or the built HTTP wire (bridge).
// The prior owner is disabled, and a delivered successful terminal must follow
// persistent commit. Opaque history must never reach the candidate transport.
func TestCodexWSMigrationDirectionsRegression(t *testing.T) {
	var logicalCache string
	for _, from := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, to := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModePassthrough} {
				for _, outcome := range []string{"accepted", "opaque", "nested-opaque", "reference", "missing-tool", "failed", "empty", "empty-shell", "commit-failure", "rate-limit"} {
					t.Run(fmt.Sprintf("%s-to-%s/%s/%s", from, to, mode, outcome), func(t *testing.T) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						cfg := &config.Config{}
						cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.OAuthEnabled, cfg.Gateway.OpenAIWS.APIKeyEnabled = true, true, true
						cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2, cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true, true
						cfg.Gateway.OpenAIWS.IngressModeDefault = mode
						cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
						cfg.Gateway.OpenAIWS.ReadTimeoutSeconds, cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 2, 2
						accounts := []Account{
							{ID: 101, Platform: PlatformOpenAI, Type: from, Status: StatusError},
							{ID: 102, Platform: PlatformOpenAI, Type: to, Status: StatusActive, Schedulable: true, Concurrency: 1,
								Credentials: map[string]any{"access_token": "b-token", "api_key": "b-token", "chatgpt_account_id": "b-chatgpt"},
								Extra: map[string]any{"responses_supported": true, "responses_websockets_v2_enabled": true,
									"openai_oauth_responses_websockets_v2_mode": mode, "openai_apikey_responses_websockets_v2_mode": mode}},
						}
						store := newFakeOpenAISessionRouteStore()
						_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
						require.NoError(t, err)
						wsWire := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_b","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
						dialer := &affinityWSWireDialer{openAIWSQueueDialer: &openAIWSQueueDialer{conns: []openAIWSClientConn{wsWire}}}
						pool := newOpenAIWSConnPool(cfg)
						pool.setClientDialerForTest(dialer)
						defer pool.Close()
						httpWire := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
						var routeStore OpenAISessionRouteStore = store
						if outcome == "commit-failure" {
							routeStore = &affinityFaultStore{fakeOpenAISessionRouteStore: store, stage: "commit"}
						}
						if outcome == "failed" || outcome == "empty" || outcome == "empty-shell" || outcome == "rate-limit" {
							event := `{"type":"response.failed","response":{"status":"failed","error":{"message":"fixture failure"}}}`
							if outcome == "empty" {
								event = `{"type":"response.completed","response":{"id":"resp_empty","status":"completed","output":[]}}`
							}
							if outcome == "empty-shell" {
								event = `{"type":"response.completed","response":{"id":"resp_empty","status":"completed","output":[{"type":"message","role":"assistant","content":[]}]}}`
							}
							if outcome == "rate-limit" {
								event = `{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"temporary rate limit"}}`
							}
							wsWire.events = [][]byte{[]byte(`{"type":"response.created","response":{"id":"resp_b"}}`), []byte(event)}
							httpWire.resp.Body = io.NopCloser(strings.NewReader("data: " + event + "\n\n"))
						}
						var direct *stagedPassthroughConn
						if mode == OpenAIWSIngressModePassthrough {
							direct = newStagedPassthroughConn()
							dialer.conns[0] = direct
							for _, event := range wsWire.events {
								direct.Send(string(event))
							}
						}
						svc := &OpenAIGatewayService{cfg: cfg, sessionRouteStore: routeStore, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
							cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
							openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSPool: pool, openaiWSPassthroughDialer: dialer, toolCorrector: NewCodexToolCorrector(), httpUpstream: httpWire}
						selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
						require.NoError(t, err)
						defer selection.ReleaseFunc()
						attemptCtx := ContextWithSelectionProfitGate(ctx, selection)
						resultCh := make(chan error, 1)
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							conn, acceptErr := coderws.Accept(w, r, nil)
							if acceptErr != nil {
								resultCh <- acceptErr
								return
							}
							defer func() { _ = conn.CloseNow() }()
							_, first, readErr := conn.Read(ctx)
							if readErr != nil {
								resultCh <- readErr
								return
							}
							c, _ := gin.CreateTestContext(httptest.NewRecorder())
							c.Request = r.Clone(attemptCtx)
							c.Request.Header.Set("session-id", "session")
							c.Request.Header.Set("x-codex-turn-state", "a-private-state")
							resultCh <- svc.ProxyResponsesWebSocketFromClient(attemptCtx, c, conn, selection.Account, "b-token", first, nil)
						}))
						defer server.Close()
						client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
						require.NoError(t, err)
						defer func() { _ = client.CloseNow() }()
						body := `{"type":"response.create","model":"gpt-5.1","input":"complete root","prompt_cache_key":"logical-session","store":false}`
						if outcome == "opaque" {
							body = `{"type":"response.create","model":"gpt-5.1","input":"delta","previous_response_id":"resp_a"}`
						}
						if outcome == "nested-opaque" {
							body = `{"type":"response.create","response":{"model":"gpt-5.1","input":"delta","previous_response_id":"resp_a"}}`
						}
						if outcome == "reference" {
							body = `{"type":"response.create","model":"gpt-5.1","input":[{"type":"item_reference","id":"call_1"},{"type":"function_call_output","call_id":"call_1","output":"result"}]}`
						}
						if outcome == "missing-tool" {
							body = `{"type":"response.create","model":"gpt-5.1","input":[{"type":"function_call_output","call_id":"call_1","output":"result"}]}`
						}
						require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(body)))
						if outcome != "accepted" {
							proxyErr := <-resultCh
							require.Error(t, proxyErr)
							if outcome == "rate-limit" && mode != OpenAIWSIngressModeHTTPBridge {
								var retry *UpstreamFailoverError
								require.True(t, errors.As(proxyErr, &retry), "pre-output throttling must retain its recovery classification: %v", proxyErr)
								require.True(t, retry.RetryableOnSameAccount)
							}
							if outcome == "opaque" || outcome == "nested-opaque" || outcome == "reference" || outcome == "missing-tool" {
								require.Zero(t, dialer.DialCount())
								require.Empty(t, httpWire.requests)
							}
							route, getErr := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
							require.NoError(t, getErr)
							require.EqualValues(t, 101, route.OwnerAccountID)
							return
						}
						for {
							_, message, readErr := client.Read(ctx)
							require.NoError(t, readErr)
							if gjson.GetBytes(message, "type").String() == "response.completed" {
								observed, routeErr := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
								require.NoError(t, routeErr)
								require.EqualValues(t, 102, observed.OwnerAccountID, "commit precedes delivered terminal")
								break
							}
						}
						_ = client.Close(coderws.StatusNormalClosure, "done")
						require.NoError(t, <-resultCh)
						route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
						require.NoError(t, err)
						require.EqualValues(t, 102, route.OwnerAccountID)
						require.EqualValues(t, 2, route.Generation)
						var headers http.Header
						var cacheKey string
						switch mode {
						case OpenAIWSIngressModeHTTPBridge:
							require.Len(t, httpWire.requests, 1)
							headers = httpWire.lastReq.Header
							require.Contains(t, gjson.GetBytes(httpWire.lastBody, "input").Raw, "complete root")
							cacheKey = gjson.GetBytes(httpWire.lastBody, "prompt_cache_key").String()
						case OpenAIWSIngressModePassthrough:
							select {
							case sent := <-direct.writes:
								require.Equal(t, "complete root", gjson.GetBytes(sent, "input").String())
								cacheKey = gjson.GetBytes(sent, "prompt_cache_key").String()
							case <-ctx.Done():
								t.Fatal("missing passthrough upstream write")
							}
							headers = dialer.DialHeaders()[0]
						default:
							require.Len(t, wsWire.writes, 1)
							require.Equal(t, "complete root", wsWire.writes[0]["input"])
							headers = dialer.DialHeaders()[0]
							cacheKey, _ = wsWire.writes[0]["prompt_cache_key"].(string)
						}
						if mode != OpenAIWSIngressModeHTTPBridge {
							require.Len(t, dialer.urls, 1)
							if to == AccountTypeOAuth {
								require.Equal(t, "wss://chatgpt.com/backend-api/codex/responses", dialer.urls[0])
							} else {
								require.Equal(t, "wss://api.openai.com/v1/responses", dialer.urls[0])
							}
						}
						require.NotEmpty(t, cacheKey)
						if logicalCache == "" {
							logicalCache = cacheKey
						}
						require.Equal(t, logicalCache, cacheKey, "ordinary logical cache survives auth/transport migration")
						require.Equal(t, "Bearer b-token", headers.Get("Authorization"))
						require.Empty(t, headers.Get("x-codex-turn-state"))
						if to == AccountTypeOAuth {
							require.Equal(t, "b-chatgpt", headers.Get("chatgpt-account-id"))
						} else {
							require.Empty(t, headers.Get("chatgpt-account-id"))
						}
						accounts[0].Status, accounts[0].Schedulable = StatusActive, true
						next, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
						require.NoError(t, err)
						require.EqualValues(t, 102, next.Account.ID)
						next.ReleaseFunc()
					})
				}
			}
		}
	}
}

// Capture the final target after account/auth-specific URL resolution.
type affinityWSWireDialer struct {
	*openAIWSQueueDialer
	urls []string
}

func (d *affinityWSWireDialer) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.urls = append(d.urls, target)
	return d.openAIWSQueueDialer.Dial(ctx, target, headers, proxy)
}
