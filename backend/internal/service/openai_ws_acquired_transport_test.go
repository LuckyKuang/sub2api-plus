//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Rewrite only the test destination; production headers and the proxy selected
// by the gateway still cross the real native WebSocket transport unchanged.
type acquiredOwnerRedirectingWSDialer struct{ base openAIWSClientDialer }

func (d *acquiredOwnerRedirectingWSDialer) Dial(ctx context.Context, _ string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	return d.base.Dial(ctx, "ws://codex-wire.invalid/responses", headers, proxy)
}

func TestCodexWSProxyAndIdentityUseAcquiredCredentialOwner(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeDedicated, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			for _, stage := range []struct {
				principal string
				relink    bool
			}{{principal: "old"}, {principal: "old", relink: true}, {principal: "new"}} {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				captured := make(chan codexMetadataOwnerWire, 1)
				completed := []byte(`{"type":"response.completed","response":{"id":"resp_proxy_owner","status":"completed","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`)
				proxyHandler := func(proxy string) http.HandlerFunc {
					return func(w http.ResponseWriter, r *http.Request) {
						captured <- codexMetadataOwnerWire{headers: r.Header.Clone(), proxy: proxy}
						if mode == OpenAIWSIngressModeHTTPBridge {
							_, _ = io.Copy(io.Discard, r.Body)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = w.Write(append(append([]byte("data: "), completed...), []byte("\n\n")...))
							return
						}
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							t.Error(err)
							return
						}
						defer func() { _ = conn.CloseNow() }()
						if _, _, err := conn.Read(ctx); err != nil {
							t.Error(err)
							return
						}
						if err := conn.Write(ctx, coderws.MessageText, completed); err != nil {
							t.Error(err)
							return
						}
						_, _, _ = conn.Read(ctx)
					}
				}
				oldProxy := httptest.NewServer(proxyHandler("old"))
				defer oldProxy.Close()
				newProxy := httptest.NewServer(proxyHandler("new"))
				defer newProxy.Close()
				newAccount := func(principal string) *Account {
					account := codexMetadataOwnerAccount(principal, false)
					account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
					account.Extra["responses_websockets_v2_enabled"] = true
					account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
					rawProxy, ua := oldProxy.URL, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
					if principal == "new" {
						rawProxy, ua = newProxy.URL, "codex_vscode/0.165.0 (Windows 11; aarch64) WindowsTerminal"
					}
					parsed, err := url.Parse(rawProxy)
					require.NoError(t, err)
					port, err := strconv.Atoi(parsed.Port())
					require.NoError(t, err)
					account.Proxy = &Proxy{ID: 12, Protocol: "http", Host: parsed.Hostname(), Port: port}
					account.ProxyID = &account.Proxy.ID
					account.Credentials["user_agent"] = ua
					return account
				}
				cfg := newOpenAIWSExecutionScopeTestConfig()
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				cfg.Gateway.OpenAIWS.IngressModeDefault = mode
				dialer := &acquiredOwnerRedirectingWSDialer{base: newDefaultOpenAIWSClientDialer()}
				svc := &OpenAIGatewayService{cfg: cfg, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
					cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector(), openaiWSStateStore: NewOpenAIWSStateStore(nil),
					openaiWSPassthroughDialer: dialer}
				pool := svc.getOpenAIWSConnPool()
				pool.setClientDialerForTest(dialer)
				defer svc.CloseOpenAIWSPool()
				svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(request *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
					request = request.Clone(request.Context())
					request.URL.Scheme, request.URL.Host, request.Host = "http", "codex-wire.invalid", "codex-wire.invalid"
					proxy, err := url.Parse(proxyURL)
					if err != nil {
						return nil, err
					}
					transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
					defer transport.CloseIdleConnections()
					return (&http.Client{Transport: transport}).Do(request)
				}}
				selected := newAccount(stage.principal)
				svc.openAITokenProvider = NewOpenAITokenProvider(nil, &codexMetadataOwnerTokenCache{acquire: func(ctx context.Context) string {
					token := stage.principal + "-token"
					recordRequestCredentialOwner(ctx, selected, token)
					if stage.relink {
						replacement := newAccount("new")
						selected.Credentials, selected.Extra, selected.ProxyID, selected.Proxy = replacement.Credentials, replacement.Extra, replacement.ProxyID, replacement.Proxy
					}
					return token
				}}, nil)
				results := make(chan error, 1)
				ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						results <- err
						return
					}
					defer func() { _ = conn.CloseNow() }()
					_, first, err := conn.Read(ctx)
					if err != nil {
						results <- err
						return
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r.Clone(ctx)
					c.Set("api_key", &APIKey{ID: 9811})
					forwardCtx := WithOutboundIdentityScope(WithOutboundIDStore(ctx, newTestOutboundIDStore()), c)
					token, _, err := svc.GetAccessToken(forwardCtx, selected)
					if err == nil {
						err = svc.ProxyResponsesWebSocketFromClient(forwardCtx, c, conn, selected, token, first, nil)
					}
					results <- err
				}))
				defer ingress.Close()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(ingress.URL, "http"), nil)
				require.NoError(t, err)
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","stream":true,"input":"hello"}`)))
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(event), "response.completed")
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
				require.NoError(t, <-results)
				wire := <-captured
				require.Equal(t, stage.principal, wire.proxy, "actual WS/HTTP transport uses the acquired proxy")
				require.Equal(t, "Bearer "+stage.principal+"-token", wire.headers.Get("Authorization"))
				require.Equal(t, stage.principal+"-principal", wire.headers.Get("ChatGPT-Account-Id"))
				if stage.principal == "old" {
					require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", wire.headers.Get("User-Agent"))
					require.Equal(t, "codex_cli_rs", wire.headers.Get("Originator"))
				} else {
					require.Equal(t, "codex_vscode", wire.headers.Get("Originator"))
				}
				require.Equal(t, "0.165.0", wire.headers.Get("Version"))
			}
		})
	}
}
