//go:build unit

package repository

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This fixture enters the public forwarding service without production Wire.
// Supply its required persistent-ID port so proxy positive controls reach the
// actual sender; storage availability is covered independently.
type sevenProviderStrictOutboundIDStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *sevenProviderStrictOutboundIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := namespace + ":" + digest
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	s.values[key] = candidate
	return candidate, nil
}

func sevenProviderStrictChatGateway(t *testing.T) *service.OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		Enabled: false, AllowInsecureHTTP: true, AllowPrivateHosts: true,
	}}}
	cfg.Gateway.ResponseHeaderTimeout = 1
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	t.Cleanup(func() {
		for _, entry := range upstream.clients {
			entry.client.CloseIdleConnections()
		}
	})
	return service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

// The public forwarding entry and production HTTPUpstream must enforce the
// saved binding before any provider or replacement-proxy request. A reachable
// logical destination detects even unsuccessful direct TCP attempts. Positive
// controls establish that this exact fixture can send through a valid proxy
// and can send directly only when the account has no bound proxy.
func TestSevenProviderChatProductionRejectsBrokenProxyBindingsBeforeSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{
		service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek,
		service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu,
	} {
		for _, mode := range []string{"valid-bound", "unbound-direct", "missing-bound-route", "mismatched-proxy-owner", "malformed-proxy-host", "incomplete-proxy-auth"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				var directTCP, proxySends atomic.Int64
				response := func(w http.ResponseWriter) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"chat-proxy-contract","object":"chat.completion","model":"business-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				}
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { response(w) }))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directTCP.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxySends.Add(1)
					response(w)
				}))
				t.Cleanup(proxy.Close)
				parsed, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				port, err := strconv.Atoi(parsed.Port())
				require.NoError(t, err)
				proxyID := int64(7901)
				account := &service.Account{
					ID: 7902, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1,
					ProxyID: &proxyID,
					Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port, Status: service.StatusActive,
						Username: "private-proxy-owner", Password: "private-proxy-password"},
					Credentials: map[string]any{"api_key": "private-provider-key", "base_url": destination.URL,
						"api_protocol": "chat_completions", "model_mapping": map[string]any{"business-chat": "business-chat"}},
					Extra: map[string]any{"openai_responses_supported": false},
				}
				if platform == service.PlatformZhipu {
					account.Credentials[service.SettingKeyOutboundIdentity] = service.OutboundIdentitySelection{
						Preset: zcode.Preset, Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"},
					}
				}
				switch mode {
				case "unbound-direct":
					account.ProxyID = nil // A residual hydrated object cannot create a binding.
				case "missing-bound-route":
					account.Proxy = nil
				case "mismatched-proxy-owner":
					account.Proxy.ID = proxyID + 1
				case "malformed-proxy-host":
					account.Proxy.Host = "invalid proxy host"
				case "incomplete-proxy-auth":
					account.Proxy.Password = ""
				}
				body := []byte(`{"model":"business-chat","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
				recorder := httptest.NewRecorder()
				inbound, _ := gin.CreateTestContext(recorder)
				ctx, cancel := context.WithTimeout(service.WithOutboundIDStore(context.Background(), &sevenProviderStrictOutboundIDStore{values: map[string]string{}}), time.Second)
				defer cancel()
				inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
				inbound.Request.Header.Set("Content-Type", "application/json")
				inbound.Request.Header.Set("Authorization", "Bearer private-inbound-token")
				result, forwardErr := sevenProviderStrictChatGateway(t).ForwardAsChatCompletions(ctx, inbound, account, body, "", "")
				if mode == "valid-bound" || mode == "unbound-direct" {
					require.NoError(t, forwardErr)
					require.NotNil(t, result)
					require.Contains(t, recorder.Body.String(), `"content":"ok"`)
					if mode == "valid-bound" {
						require.Equal(t, int64(1), proxySends.Load())
						require.Zero(t, directTCP.Load())
					} else {
						require.Equal(t, int64(1), directTCP.Load())
						require.Zero(t, proxySends.Load())
					}
					return
				}
				require.Zero(t, directTCP.Load(), "missing or broken binding cannot grant direct TCP egress")
				require.Zero(t, proxySends.Load(), "a mismatched hydrated proxy cannot replace the saved owner")
				require.Error(t, forwardErr, "an invalid bound route must fail before the provider send")
				require.Nil(t, result)
				for _, secret := range []string{"private-provider-key", "private-proxy-owner", "private-proxy-password", "private-inbound-token"} {
					require.NotContains(t, forwardErr.Error(), secret)
				}
			})
		}
	}
}
