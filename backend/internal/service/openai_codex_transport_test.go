//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Requirements: official Codex 1b6e0d8145 default_client::default_headers and
// ModelProviderInfo::create_openai_provider; AGENTS.md credential-owner priority.
// Assertions are literal wire declarations, not values produced by the resolver.
func TestCodexProviderIdentityHTTPAndWebSocketWire(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			t.Run(kind+"/passthrough="+map[bool]string{true: "on", false: "off"}[passthrough], func(t *testing.T) {
				captured := make(chan http.Header, 2)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					captured <- r.Header.Clone()
					if websocket.IsWebSocketUpgrade(r) {
						conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer upstream.Close()
				cfg := codexTransportTestConfig()
				svc := &OpenAIGatewayService{cfg: cfg}
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind, Concurrency: 1, Credentials: map[string]any{
					"api_key": "fixture", "access_token": "fixture", "base_url": upstream.URL,
					"user_agent": "codex_vscode/0.163.0 (Ubuntu 24.04; x86_64) vscode",
				}}
				body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello","instructions":"test"}`)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.163.0 (Ubuntu 24.04; x86_64) xterm-256color")
				c.Request.Header.Set("Originator", "codex_work_desktop")
				c.Request.Header.Set("Version", "0.1.0")
				c.Request.Header.Set("session_id", "fixture-session")
				c.Request.Header.Set("conversation_id", "fixture-conversation")
				c.Request.Header.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
				var req *http.Request
				var err error
				if passthrough {
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "fixture")
				} else {
					req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "fixture", true, "", true)
				}
				require.NoError(t, err)
				target, _ := url.Parse(upstream.URL)
				req.URL.Scheme, req.URL.Host, req.Host = target.Scheme, target.Host, target.Host
				resp, err := http.DefaultClient.Do(prepareAccountOutboundRequest(req, account))
				require.NoError(t, err)
				_ = resp.Body.Close()
				headers, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "fixture", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", "", "gpt-6.1-sol", "")
				require.NoError(t, err)
				conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(context.Background(), "ws"+strings.TrimPrefix(upstream.URL, "http"), headers, "")
				require.NoError(t, err)
				_ = conn.Close()
				for range 2 {
					h := <-captured
					require.Equal(t, "codex_vscode/0.163.0 (Ubuntu 24.04; x86_64) vscode", h.Get("User-Agent"))
					require.Equal(t, "codex_vscode", h.Get("Originator"), "inbound official thread labels cannot override the account")
					require.Equal(t, "0.163.0", h.Get("Version"))
					require.Equal(t, "Bearer fixture", h.Get("Authorization"))
					require.Empty(t, h.Get("session_id"))
					require.Empty(t, h.Get("conversation_id"))
					if strings.EqualFold(h.Get("Upgrade"), "websocket") {
						require.Equal(t, "responses_websockets=2026-02-06", h.Get("OpenAI-Beta"))
					} else {
						require.Empty(t, h.Get("OpenAI-Beta"))
					}
					if kind == AccountTypeAPIKey {
						require.Empty(t, h.Get("ChatGPT-Account-ID"))
					}
				}
			})
		}
	}
}

func codexTransportTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 2
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 2
	cfg.Gateway.OpenAIWS.FallbackCooldownSeconds = 30
	return cfg
}

// User-approved opt-in: ordinary HTTP stays HTTP, explicit preference upgrades
// complete streaming inputs, pre-submit handshake failure may fall back, and
// an ambiguous disconnect after response.create must never generate twice.
func TestCodexHTTPWebSocketTransportWire(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name            string
			enabled, prefer bool
			mode            string
			restriction     string
			body            string
			ws, http        int32
			failed          bool
			lite            bool
		}{
			{name: "default HTTP", enabled: true, mode: "success", http: 1},
			{name: "opt in", enabled: true, prefer: true, mode: "success", ws: 1},
			{name: "startup force HTTP", enabled: true, prefer: true, restriction: "force_http", http: 1},
			{name: "account opt out", enabled: true, prefer: true, restriction: "account_off", http: 1},
			{name: "v2 deployment off", enabled: true, prefer: true, restriction: "v2_off", http: 1},
			{name: "account force HTTP", enabled: true, prefer: true, restriction: "account_force_http", http: 1},
			{name: "master disabled", prefer: true, mode: "success", http: 1},
			{name: "handshake rejected", enabled: true, prefer: true, mode: "reject", ws: 1, http: 1},
			{name: "Lite handshake rejected", enabled: true, prefer: true, mode: "reject", ws: 1, http: 1, lite: true},
			{name: "Lite opaque metadata stays HTTP", enabled: true, prefer: true, http: 1, lite: true, body: `{"model":"gpt-6.1-sol","stream":true,"input":"hello","client_metadata":["future-shape"]}`},
			{name: "disconnect after submit", enabled: true, prefer: true, mode: "disconnect", ws: 1, failed: true},
			{name: "continuation stays HTTP", enabled: true, prefer: true, mode: "success", body: `{"model":"gpt-6.1-sol","stream":true,"previous_response_id":"resp_existing","input":"continue"}`, http: 1},
			{name: "background stays HTTP", enabled: true, prefer: true, body: `{"model":"gpt-6.1-sol","stream":true,"background":true,"input":"hello"}`, http: 1},
			{name: "stream continuation stays HTTP", enabled: true, prefer: true, body: `{"model":"gpt-6.1-sol","stream":true,"stream_id":"stream_existing","input":"hello"}`, http: 1},
			{name: "legacy continuation stays HTTP", enabled: true, prefer: true, body: `{"model":"gpt-6.1-sol","stream":true,"previous_response_id":"resp_existing","messages":[{"role":"user","content":"continue"}]}`, http: 1},
		} {
			t.Run(tc.name+map[bool]string{true: "/passthrough", false: "/transform"}[passthrough], func(t *testing.T) {
				var wsCalls, httpCalls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if websocket.IsWebSocketUpgrade(r) {
						wsCalls.Add(1)
						if tc.mode == "reject" {
							w.WriteHeader(http.StatusUpgradeRequired)
							return
						}
						conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
						if err != nil {
							return
						}
						defer conn.Close()
						_, raw, err := conn.ReadMessage()
						if err != nil {
							return
						}
						var payload map[string]any
						_ = json.Unmarshal(raw, &payload)
						if payload["type"] != "response.create" {
							return
						}
						if tc.mode == "disconnect" {
							return
						}
						_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"OK","output_index":0,"content_index":0}`))
						_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_ok","model":"gpt-6.1-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`))
						return
					}
					httpCalls.Add(1)
					if tc.lite {
						require.Equal(t, "true", r.Header.Get("x-openai-internal-codex-responses-lite"))
						if tc.body != "" {
							raw, err := io.ReadAll(r.Body)
							require.NoError(t, err)
							var body map[string]json.RawMessage
							require.NoError(t, json.Unmarshal(raw, &body))
							require.JSONEq(t, `["future-shape"]`, string(body["client_metadata"]))
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_http\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
				}))
				defer upstream.Close()
				cfg := codexTransportTestConfig()
				settings := &SettingService{}
				// Supplying a real repo makes cached runtime settings authoritative.
				settings.settingRepo = &openAIIdentitySettingRepoStub{values: map[string]string{}}
				settings.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: tc.enabled, HTTPWSEnabled: tc.prefer})
				svc := newOpenAIRejectedFieldTestService(nil)
				svc.cfg = cfg
				svc.openaiWSResolver = NewOpenAIWSProtocolResolver(cfg)
				svc.settingService = settings
				svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					return http.DefaultClient.Do(req)
				}}
				defer svc.CloseOpenAIWSPool()
				account := newOpenAIRejectedFieldTestAccount()
				account.Credentials["base_url"] = upstream.URL
				account.Extra["openai_apikey_responses_websockets_v2_enabled"] = true
				account.Extra["openai_passthrough"] = passthrough
				switch tc.restriction {
				case "force_http":
					cfg.Gateway.OpenAIWS.ForceHTTP = true
				case "v2_off":
					cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = false
				case "account_off":
					account.Extra["openai_apikey_responses_websockets_v2_enabled"] = false
				case "account_force_http":
					account.Extra["openai_ws_force_http"] = true
				}
				body := []byte(tc.body)
				if len(body) == 0 {
					body = []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`)
				}
				c := newOpenAIRejectedFieldTestContext(body)
				if tc.lite {
					c.Request.Header.Set("x-openai-internal-codex-responses-lite", "true")
				}
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				_, err := svc.Forward(context.Background(), c, account, body)
				if tc.failed {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tc.ws, wsCalls.Load())
				require.Equal(t, tc.http, httpCalls.Load(), "never replay a submitted WS generation")
				if tc.mode == "reject" {
					c = newOpenAIRejectedFieldTestContext(body)
					if tc.lite {
						c.Request.Header.Set("x-openai-internal-codex-responses-lite", "true")
					}
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					_, err = svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Equal(t, int32(1), wsCalls.Load(), "cooldown avoids repeated handshake latency")
					require.Equal(t, int32(2), httpCalls.Load())
				}
			})
		}
	}
}

func TestCodexTransportSettingsDefaultsAndFailure(t *testing.T) {
	missing := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	require.Equal(t, CodexTransportSettings{WSEnabled: true}, missing.GetCodexTransportSettings(context.Background()))
	missing.storeCodexTransportSettings(CodexTransportSettings{})
	require.Equal(t, CodexTransportSettings{}, missing.GetCodexTransportSettings(context.Background()), "save is immediately visible")
	unavailable := &SettingService{settingRepo: &unavailableCodexProfileRepo{}}
	require.Equal(t, CodexTransportSettings{}, unavailable.GetCodexTransportSettings(context.Background()))
}

func TestCodexTransportDisabledPoolNoReuseDialOrPrewarm(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	pool := newOpenAIWSConnPoolWithPolicy(codexTransportTestConfig(), func(context.Context, *Account) bool { return allowed.Load() })
	defer pool.Close()
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://fixture.invalid/responses"}
	lease, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	allowed.Store(false)
	_, err = pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, errCodexUpstreamWSDisabled)
	require.False(t, lease.conn.isClosed(), "accepted turn is allowed to finish")
	lease.Release()
	pool.ensureTargetIdleAsync(account.ID)
	pool.runBackgroundCleanupSweep(time.Now())
	require.True(t, lease.conn.isClosed(), "idle connection is drained after the turn")
	_, err = pool.dialConn(context.Background(), req)
	require.ErrorIs(t, err, errCodexUpstreamWSDisabled)
	dialer.mu.Lock()
	count := dialer.dialCount
	dialer.mu.Unlock()
	require.Equal(t, 1, count, "disabled means no reuse, dial or prewarm")
}

// A disabled native upstream may bridge only a request with recoverable history.
// An opaque first continuation must fail before billing/concurrency hooks and
// before either HTTP generation or WS dialing (first-frame audit is in handler).
func TestCodexTransportDisabledBridgeRejectsOpaqueContinuation(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh continuation", true: "foreign continuation after completed turn"}[resumed], func(t *testing.T) {
			settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
			settings.storeCodexTransportSettings(CodexTransportSettings{})
			var httpCalls, beforeTurns, audits atomic.Int32
			svc := &OpenAIGatewayService{cfg: codexTransportTestConfig(), settingService: settings, toolCorrector: NewCodexToolCorrector()}
			svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				httpCalls.Add(1)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_known\",\"status\":\"completed\",\"output\":[]}}\n\n"))}, nil
			}}
			account := newOpenAIRejectedFieldTestAccount()
			account.Extra["openai_apikey_responses_websockets_v2_enabled"] = true
			opaque := []byte(`{"type":"response.create","model":"gpt-6.1-sol","previous_response_id":"resp_unknown","input":"continue"}`)
			first := opaque
			if resumed {
				first = []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hello"}`)
			}
			errCh := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					errCh <- err
					return
				}
				defer conn.CloseNow()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				errCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, account.GetOpenAIProtocolAPIKey(), first,
					&OpenAIWSIngressHooks{
						BeforeTurn:    func(int) error { beforeTurns.Add(1); return nil },
						BeforeRequest: func(int, []byte, string) error { audits.Add(1); return nil },
					})
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer client.CloseNow()
			if resumed {
				_, raw, err := client.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(raw), "response.completed")
				require.NoError(t, client.Write(ctx, coderws.MessageText, opaque))
			}
			select {
			case err := <-errCh:
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusServiceRestart, closeErr.statusCode)
			case <-ctx.Done():
				t.Fatal("bridge failed to reject opaque continuation")
			}
			want := int32(0)
			if resumed {
				want = 1
			}
			require.Equal(t, want, beforeTurns.Load(), "rejected turn cannot acquire billing/concurrency")
			require.Equal(t, want, httpCalls.Load(), "rejected continuation cannot generate")
			require.Equal(t, want, audits.Load(), "follow-up ingress is audited before the transport decision")
			require.Nil(t, svc.openaiWSPool, "must not even create a WS pool")
		})
	}
}
