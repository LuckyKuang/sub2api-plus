//go:build unit

package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type strictProviderProxyCase struct {
	platform, model, userAgent, oauthAuthName, oauthAuthValue string
}

var strictProviderProxyCases = []strictProviderProxyCase{
	{PlatformOpenAI, "gpt-5.4", "codex_cli_rs/0.165.0", "Authorization", "Bearer old-token"},
	{PlatformGrok, "grok-4.5", "grok-shell/", "Authorization", "Bearer strict-token"},
	{PlatformDeepseek, "deepseek-chat", "deepseek-harness/", "x-dsh-auth-token", "strict-token"},
	{PlatformKimi, "kimi-k2.5", "kimi-code-cli/", "Authorization", "Bearer strict-token"},
	{PlatformMiniMax, "MiniMax-M2.7", "MiniMaxAgent", "Authorization", "Bearer strict-token"},
	{PlatformStepFun, "step-3.7-flash", "stepcode (", "Authorization", "Bearer test-key"},
	{PlatformZhipu, "glm-5", "ZCode/", "Authorization", "Bearer pat-strict-token-login-1"},
}

func strictProviderProxyAccount(t *testing.T, tc strictProviderProxyCase, kind string, proxy *Proxy) *Account {
	t.Helper()
	if kind == AccountTypeAPIKey {
		return &Account{ID: 8821, Platform: tc.platform, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 1,
			ProxyID: &proxy.ID, Proxy: proxy, Credentials: map[string]any{"api_key": "strict-key", "api_protocol": "chat_completions"}}
	}
	var account *Account
	switch tc.platform {
	case PlatformOpenAI:
		account = codexMetadataOwnerAccount("old", false)
	case PlatformGrok:
		account = healthyGrokOAuthGatewayTestAccount(8821, "strict-token")
	case PlatformStepFun:
		account = stepFunTestAccount(kind, "cn")
	case PlatformZhipu:
		account, _ = zhipuNativeCredentialAccount("bigmodel", zhipuNativeCredentialCase{plan: ZhipuPlanIndividualCodingPlan})
		account.Credentials["oauth_access_token"] = "strict-token-login"
		account.Credentials[credentialAPIBaseURLs].(map[string]any)[APIProtocolChatCompletions] = "https://open.bigmodel.cn/api/coding/paas/v4"
		account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "3.16.2"}
	default:
		version := "2.3.1"
		if tc.platform == PlatformZhipu {
			version = "3.16.2"
		}
		account = nativeAcquiredOwnerAccount(tc.platform, proxy, "strict-token", version)
	}
	account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
	account.ProxyID, account.Proxy = &proxy.ID, proxy
	return account
}

// These are public gateway adapter tests, separate from the shared HTTPUpstream
// contract. Each real provider's OAuth/API-key preparation reaches an actual
// failing proxy. A reachable destination records TCP connections, including an
// accidental direct attempt that never completes a valid HTTP request.
func TestSevenProviderGatewayProxyFailureNeverDialsDirect(t *testing.T) {
	for _, tc := range strictProviderProxyCases {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, failure := range []string{"timeout", "refused"} {
				t.Run(tc.platform+"/"+kind+"/"+failure, func(t *testing.T) {
					var directConnections, proxyRequests, physicalSends atomic.Int64
					destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
					destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
						if state == http.StateNew {
							directConnections.Add(1)
						}
					}
					destination.Start()
					defer destination.Close()
					release := make(chan struct{})
					proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						proxyRequests.Add(1)
						<-release
					}))
					defer proxyServer.Close()
					defer close(release)
					parsed, err := url.Parse(proxyServer.URL)
					require.NoError(t, err)
					port, err := strconv.Atoi(parsed.Port())
					require.NoError(t, err)
					proxy := &Proxy{ID: 8822, Protocol: "http", Host: parsed.Hostname(), Port: port, Status: StatusActive}
					if failure == "refused" {
						proxyServer.Close()
					}
					account := strictProviderProxyAccount(t, tc, kind, proxy)
					upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, route string, accountID int64, _ int) (*http.Response, error) {
						physicalSends.Add(1)
						require.Equal(t, account.ID, accountID)
						require.Equal(t, proxyServer.URL, route, "the real provider adapter must forward the configured route")
						if kind == AccountTypeOAuth {
							require.True(t, strings.HasPrefix(request.UserAgent(), tc.userAgent), "%s selected its official client family: %s", tc.platform, request.UserAgent())
							require.Equal(t, tc.oauthAuthValue, request.Header.Get(tc.oauthAuthName))
						} else {
							require.Equal(t, "Bearer strict-key", request.Header.Get("Authorization"))
						}
						// A short per-send test deadline models an established proxy
						// that stops responding; no arbitrary sleep decides correctness.
						requestCtx, cancel := context.WithTimeout(request.Context(), 100*time.Millisecond)
						defer cancel()
						wire := request.Clone(requestCtx)
						target, err := url.Parse(destination.URL + request.URL.Path)
						if err != nil {
							return nil, err
						}
						wire.URL, wire.Host = target, target.Host
						transport := &http.Transport{}
						if route != "" {
							proxyURL, err := url.Parse(route)
							if err != nil {
								return nil, err
							}
							transport.Proxy = http.ProxyURL(proxyURL)
						}
						defer transport.CloseIdleConnections()
						return (&http.Client{Transport: transport}).Do(wire)
					}}
					repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{account.ID: account}}}
					oauth := NewCNOAuthService(nil, nil, nil)
					var tokens *ZhipuProjectTokenManager
					if tc.platform == PlatformZhipu && kind == AccountTypeOAuth {
						tokens = newProjectTokenTestService(t, &projectTokenTestClient{})
					}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo,
						cnOAuthService: oauth, projectTokens: tokens, grokTokenProvider: NewGrokTokenProvider(repo, nil)}
					if tc.platform == PlatformOpenAI && kind == AccountTypeOAuth {
						svc.openAITokenProvider = NewOpenAITokenProvider(nil, &codexMetadataOwnerTokenCache{acquire: func(ctx context.Context) string {
							recordRequestCredentialOwner(ctx, account, "old-token")
							return "old-token"
						}}, nil)
					}
					body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":false}`, tc.model))
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request.URL.Path = "/v1/chat/completions"
					ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
					result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
					require.Error(t, err, "proxy failure must surface to the caller")
					require.Nil(t, result, "no provider result can exist without a successful physical send")
					require.Positive(t, physicalSends.Load(), "the public entrypoint must exercise this provider adapter: %v", err)
					if failure == "timeout" {
						require.Positive(t, proxyRequests.Load(), "timeout must occur at the configured proxy")
					}
					require.Zero(t, directConnections.Load(), "the failed proxy must never grant direct/local egress")
				})
			}
		}
	}
}
