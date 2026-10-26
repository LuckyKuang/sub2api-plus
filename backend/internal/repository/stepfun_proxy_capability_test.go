//go:build unit

package repository

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The gateway intentionally detaches inference from an inbound deadline to
// finish billing. Bound each test send after that detach while retaining the
// complete request context and the real production HTTP client/transport.
type stepBoundProductionUpstream struct{ service.HTTPUpstream }

func (u stepBoundProductionUpstream) Do(request *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(request.Context(), 200*time.Millisecond)
	defer cancel()
	return u.HTTPUpstream.Do(request.Clone(ctx), proxy, id, concurrency)
}

func (u stepBoundProductionUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(request.Context(), 200*time.Millisecond)
	defer cancel()
	return u.HTTPUpstream.DoWithTLS(request.Clone(ctx), proxy, id, concurrency, profile)
}

func stepBoundProductionGateway(t *testing.T) *service.OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true, AllowPrivateHosts: true}}}
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	t.Cleanup(func() {
		for _, entry := range upstream.clients {
			entry.client.CloseIdleConnections()
		}
	})
	return service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, stepBoundProductionUpstream{upstream}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestStepFunProductionProxyFailureAcrossProfilesAndBridges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, profile := range []struct{ name, kind, region, mode, host, path string }{
		{"oauth_plan_cn", "oauth", "cn", "coding", "api.stepfun.com", "/step_plan/v1/chat/completions"},
		{"oauth_plan_global", "oauth", "global", "coding", "api.stepfun.ai", "/step_plan/v1/chat/completions"},
		{"apikey_payg_cn", "apikey", "cn", "payg", "api.stepfun.com", "/v1/chat/completions"},
		{"apikey_payg_global", "apikey", "global", "payg", "api.stepfun.ai", "/v1/chat/completions"},
		{"apikey_plan_cn", "apikey", "cn", "coding", "api.stepfun.com", "/step_plan/v1/chat/completions"},
		{"apikey_plan_global", "apikey", "global", "coding", "api.stepfun.ai", "/step_plan/v1/chat/completions"},
	} {
		for _, ingress := range []string{"chat", "responses", "messages"} {
			for _, failure := range []string{"timeout", "refused", "407"} {
				t.Run(profile.name+"/"+ingress+"/"+failure, func(t *testing.T) {
					var directTCP, directAttempts, proxyRequests atomic.Int64
					destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
					destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
						if state == http.StateNew {
							directTCP.Add(1)
						}
					}
					destination.Start()
					t.Cleanup(destination.Close)
					release := make(chan struct{})
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
						proxyRequests.Add(1)
						if request.Method == http.MethodConnect {
							require.Equal(t, profile.host+":443", request.Host)
							require.Empty(t, request.Header.Get("Authorization"), "tunnel negotiation cannot expose the model credential")
						}
						if failure == "timeout" {
							<-release
							return
						}
						w.Header().Set("Proxy-Authenticate", `Basic realm="step-contract"`)
						w.WriteHeader(http.StatusProxyAuthRequired)
					}))
					t.Cleanup(proxy.Close)
					t.Cleanup(func() { close(release) })
					proxyURL, err := url.Parse(proxy.URL)
					require.NoError(t, err)
					port, err := strconv.Atoi(proxyURL.Port())
					require.NoError(t, err)
					proxyID := int64(883201)
					account := &service.Account{ID: 883202, Platform: service.PlatformStepFun, Type: profile.kind, Status: service.StatusActive, Concurrency: 1,
						ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: proxyURL.Hostname(), Port: port, Status: service.StatusActive, Username: "step-proxy-owner", Password: "step-private-proxy-password"},
						Credentials: map[string]any{"api_key": "step-private-model-key", "region": profile.region, "account_mode": profile.mode, "api_protocol": "chat_completions"}}
					if profile.kind == "oauth" {
						account.Credentials = map[string]any{"access_token": "step-private-model-key", "oauth_provider": "stepfun", "oauth_region": profile.region, "account_mode": profile.mode, "api_protocol": "chat_completions"}
					} else {
						base := strings.TrimSuffix(profile.path, "/chat/completions")
						account.Credentials["base_url"] = destination.URL + base
					}
					if failure == "refused" {
						proxy.Close()
					}
					ctx, cancel := context.WithTimeout(service.WithOutboundIDStore(context.Background(), &sevenProviderStrictOutboundIDStore{values: map[string]string{}}), 150*time.Millisecond)
					defer cancel()
					// The trace observes actual production transport connection starts,
					// including failed direct dials to an official OAuth hostname. Abort
					// immediately if a regression attempts that route; fake credentials
					// and test execution must never contact the public provider.
					ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{ConnectStart: func(_, address string) {
						if address != proxyURL.Host {
							directAttempts.Add(1)
							cancel()
						}
					}})
					body := []byte(`{"model":"step-3.7-flash","stream":false,"max_tokens":64,"input":"hello","messages":[{"role":"user","content":"hello"}]}`)
					path := map[string]string{"chat": "/v1/chat/completions", "responses": "/v1/responses", "messages": "/v1/messages"}[ingress]
					recorder := httptest.NewRecorder()
					inbound, _ := gin.CreateTestContext(recorder)
					inbound.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
					inbound.Request.Header.Set("Authorization", "Bearer step-private-inbound-key")
					gateway := stepBoundProductionGateway(t)
					service.ProvideCNOAuthService(nil, nil, nil, nil, nil, gateway, &service.AccountTestService{}, &service.CNProviderQuotaService{}, nil)
					var result *service.OpenAIForwardResult
					switch ingress {
					case "responses":
						result, err = gateway.Forward(ctx, inbound, account, body)
					case "messages":
						result, err = gateway.ForwardAsAnthropic(ctx, inbound, account, body, "", "")
					default:
						result, err = gateway.ForwardAsChatCompletions(ctx, inbound, account, body, "", "")
					}
					require.Error(t, err)
					require.Nil(t, result, "a failed proxy cannot produce billable successful usage")
					require.Zero(t, directAttempts.Load(), "production transport must not even start a direct connection")
					require.Zero(t, directTCP.Load())
					if failure == "refused" {
						require.Zero(t, proxyRequests.Load())
					} else {
						require.Positive(t, proxyRequests.Load(), "failure must be observed at the saved proxy")
					}
					for _, secret := range []string{"step-private-model-key", "step-private-proxy-password", "step-private-inbound-key"} {
						require.NotContains(t, fmt.Sprint(err), secret)
						require.NotContains(t, recorder.Body.String(), secret)
					}
				})
			}
		}
	}
}
