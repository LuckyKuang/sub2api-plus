//go:build unit

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func nativeMessagesProxyGateway(t *testing.T) *service.OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true, AllowPrivateHosts: true}}}
	cfg.Gateway.ResponseHeaderTimeout = 1
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	t.Cleanup(func() {
		for _, entry := range upstream.clients {
			entry.client.CloseIdleConnections()
		}
	})
	return service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func nativeMessagesProxyContext(t *testing.T, body []byte) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer inbound-private-token")
	c.Request.Header.Set("X-Api-Key", "inbound-private-key")
	return c
}

// DeepSeek's official API-key adapter uses Messages plus x-api-key
// (llm-deepseek-api-key/src/index.ts); MiniMax BYOK declares anthropic-messages
// with apiKey authentication (model-resolver-byok.ts and pi providers/anthropic.ts).
// These tests exercise the public gateway and the actual production HTTPUpstream.
func TestNativeMessagesAPIKeyProductionProxyContract(t *testing.T) {
	for _, platform := range []string{service.PlatformDeepseek, service.PlatformMiniMax} {
		for _, mode := range []string{"success", "unbound-direct", "timeout", "refused", "authentication-required", "missing-bound-route"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				var directTCP, proxyHTTP atomic.Int64
				model := "deepseek-chat"
				if platform == service.PlatformMiniMax {
					model = "MiniMax-M2.7"
				}
				body := []byte(`{"model":"` + model + `","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"stream":false}`)
				respond := func(w http.ResponseWriter) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"msg_proxy","type":"message","role":"assistant","model":"`+model+`","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
				}
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { respond(w) }))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directTCP.Add(1)
					}
				}
				destination.Start()
				defer destination.Close()
				release := make(chan struct{})
				var releaseOnce sync.Once
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyHTTP.Add(1)
					if mode == "timeout" {
						<-release
						return
					}
					if mode == "authentication-required" {
						w.Header().Set("Proxy-Authenticate", `Basic realm="contract"`)
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					}
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, "/anthropic/v1/messages", r.URL.Path, "native Messages must not become Chat Completions")
					require.Equal(t, "private-provider-key", r.Header.Get("X-Api-Key"))
					require.Empty(t, r.Header.Get("Authorization"))
					require.Empty(t, r.Header.Get("X-Dsh-Auth-Token"), "API keys are not native account login tokens")
					require.Equal(t, "2023-06-01", r.Header.Get("Anthropic-Version"))
					wire, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.JSONEq(t, string(body), string(wire), "the native request preserves the Messages body")
					require.NotEmpty(t, r.UserAgent())
					respond(w)
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
				proxyURL, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				port, err := strconv.Atoi(proxyURL.Port())
				require.NoError(t, err)
				id := int64(771)
				account := &service.Account{ID: 772, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1, ProxyID: &id,
					Proxy:       &service.Proxy{ID: id, Protocol: "http", Host: proxyURL.Hostname(), Port: port, Status: service.StatusActive, Username: "owner", Password: "private-proxy-password"},
					Credentials: map[string]any{"api_key": "private-provider-key", "api_protocol": "anthropic", "base_url": destination.URL + "/anthropic"}}
				if mode == "unbound-direct" {
					account.ProxyID = nil
					account.Proxy = nil
				}
				if mode == "missing-bound-route" {
					account.Proxy = nil
				}
				if mode == "refused" {
					proxy.Close()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				inbound := nativeMessagesProxyContext(t, body)
				inbound.Request = inbound.Request.WithContext(ctx)
				result, err := nativeMessagesProxyGateway(t).ForwardAsAnthropic(ctx, inbound, account, body, "", "")
				if mode == "success" || mode == "unbound-direct" {
					require.NoError(t, err)
					require.NotNil(t, result)
					if mode == "unbound-direct" {
						require.Positive(t, directTCP.Load())
						require.Zero(t, proxyHTTP.Load())
					} else {
						require.Positive(t, proxyHTTP.Load())
						require.Zero(t, directTCP.Load())
					}
					return
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, directTCP.Load(), "a configured proxy cannot grant direct/local egress on any failure")
				if mode == "timeout" || mode == "authentication-required" {
					require.Positive(t, proxyHTTP.Load())
				}
				if mode == "missing-bound-route" {
					require.Zero(t, proxyHTTP.Load())
				}
				for _, secret := range []string{"private-provider-key", "private-proxy-password", "inbound-private-token", "inbound-private-key"} {
					require.NotContains(t, err.Error(), secret)
				}
			})
		}
	}
}

// Assert both official preset defaults independently; the local transport test
// above changes only the host so a direct attempt is observable without internet.
func TestNativeMessagesAPIKeyOfficialDefaults(t *testing.T) {
	for _, tc := range []struct{ platform, base string }{
		{service.PlatformDeepseek, "https://api.deepseek.com/anthropic"},
		{service.PlatformMiniMax, "https://api.minimaxi.com/anthropic"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			a := &service.Account{Platform: tc.platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-provider-key", "api_protocol": "anthropic"}}
			require.Equal(t, tc.base, strings.TrimRight(a.GetAnthropicProtocolBaseURL(), "/"))
			serialized, err := json.Marshal(a.Credentials)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), "chat_completions")
		})
	}
}
