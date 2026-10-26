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

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

type nativeAcquiredOwnerWire struct {
	proxy   string
	headers http.Header
}

func nativeAcquiredOwnerProxy(t *testing.T, name string, captures chan<- nativeAcquiredOwnerWire, rejectOld bool) *Proxy {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captures <- nativeAcquiredOwnerWire{proxy: name, headers: r.Header.Clone()}
		if rejectOld && r.Header.Get("Authorization") == "Bearer old-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Proxy{ID: 71, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive}
}

// The adapters validate the real HTTPS provider destination. At the final
// transport boundary only the scheme is changed so the local HTTP proxy can
// capture the fully rendered wire request without a provider connection.
func nativeAcquiredOwnerUpstream(t *testing.T, selectedID int64, concurrency int) *codexModelsHTTPUpstreamStub {
	t.Helper()
	return &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxyURL string, accountID int64, actualConcurrency int) (*http.Response, error) {
		require.Equal(t, selectedID, accountID)
		require.Equal(t, concurrency, actualConcurrency)
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		wire := req.Clone(req.Context())
		target := *req.URL
		target.Scheme = "http"
		wire.URL = &target
		transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
		defer transport.CloseIdleConnections()
		return (&http.Client{Transport: transport}).Do(wire)
	}}
}

func nativeAcquiredOwnerAccount(platform string, proxy *Proxy, token string, version string) *Account {
	account := &Account{ID: 7901, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 3,
		ProxyID: &proxy.ID, Proxy: proxy,
		Credentials: map[string]any{
			"oauth_provider": platform, "oauth_region": "cn", domesticOAuthLoginIDCredential: "old-login",
			"access_token": token, "refresh_token": "old-refresh", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	if platform == PlatformKimi {
		account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "kimi", Version: version}
	}
	if platform == PlatformZhipu {
		account.Credentials["oauth_provider"] = "bigmodel"
		account.Credentials["oauth_access_token"] = token + "-login"
		account.Credentials["plan_kind"] = ZhipuPlanIndividualCodingPlan
		account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: version}
	}
	return account
}

// AGENTS.md and OUTBOUND_IDENTITY.md require adapters to preserve the acquired
// grant even if the selected row is relinked before native preparation. These
// assertions are at a real HTTP proxy and cover the bearer/PAT, UA, companion
// version and actual proxy together, while resource IDs stay on the selection.
func TestNativeAdaptersUseAcquiredOwnerAfterSelectedRelink(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		for _, entry := range []string{"gateway", "account-test"} {
			for _, relink := range []string{"same-provider", "different-provider-type"} {
				t.Run(platform+"/"+entry+"/"+relink, func(t *testing.T) {
					captures := make(chan nativeAcquiredOwnerWire, 4)
					oldProxy := nativeAcquiredOwnerProxy(t, "old", captures, false)
					newProxy := nativeAcquiredOwnerProxy(t, "new", captures, false)
					oldVersion, newVersion := "2.3.0", "2.3.2"
					if platform == PlatformZhipu {
						oldVersion, newVersion = "3.16.2", "3.16.3"
					}
					selected := nativeAcquiredOwnerAccount(platform, oldProxy, "old-token", oldVersion)
					target := selected.GetAnthropicProtocolBaseURL() + "/v1/messages"
					if platform == PlatformZhipu {
						target = zhipuCodingBaseURL("bigmodel") + "/chat/completions"
					}
					ctx := WithOutboundIdentityScope(t.Context(), nil)
					ctx = WithAccountOutboundIdentity(ctx, selected)
					recordRequestCredentialOwner(ctx, selected, "old-token")
					ctx = withRequestCredentialOwner(ctx, selected, "old-token")
					replacement := nativeAcquiredOwnerAccount(platform, newProxy, "new-token", newVersion)
					replacement.Credentials[domesticOAuthLoginIDCredential] = "new-login"
					if relink == "different-provider-type" {
						replacement.Platform, replacement.Type = PlatformOpenAI, AccountTypeAPIKey
						replacement.Credentials = map[string]any{"api_key": "new-api-key"}
					}
					*selected = *replacement
					oauth := NewCNOAuthService(nil, nil, nil)
					var tokens *ZhipuProjectTokenManager
					if platform == PlatformZhipu {
						tokens = newProjectTokenTestService(t, &projectTokenTestClient{})
					}
					upstream := nativeAcquiredOwnerUpstream(t, selected.ID, 3)
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(`{"model":"native-model","messages":[]}`))
					require.NoError(t, err)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/vnd.foreign")
					req.Header.Set("X-Grok-Session-Id", "untrusted")
					var response *http.Response
					if entry == "gateway" {
						response, err = (&OpenAIGatewayService{httpUpstream: upstream, cnOAuthService: oauth, projectTokens: tokens}).doOpenAIUpstream(req, newProxy.URL(), selected)
					} else {
						response, err = (&AccountTestService{httpUpstream: upstream, cnOAuthService: oauth, projectTokens: tokens}).doOpenAIAccountTestUpstream(req, newProxy.URL(), selected, false)
					}
					require.NoError(t, err)
					require.NotNil(t, response)
					_ = response.Body.Close()
					wire := <-captures
					require.Equal(t, "old", wire.proxy)
					require.Equal(t, "application/json", wire.headers.Get("Accept"))
					require.Empty(t, wire.headers.Get("X-Grok-Session-Id"))
					switch platform {
					case PlatformDeepseek:
						require.Equal(t, "old-token", wire.headers.Get("x-dsh-auth-token"))
						require.Empty(t, wire.headers.Get("Authorization"))
						// OUTBOUND_IDENTITY.md: the official product comment is part
						// of the single DeepSeek UA declaration.
						require.Equal(t, "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)", wire.headers.Get("User-Agent"))
					case PlatformKimi:
						require.Equal(t, "Bearer old-token", wire.headers.Get("Authorization"))
						require.Equal(t, "kimi-code-cli/2.3.0", wire.headers.Get("User-Agent"))
						require.Equal(t, "2.3.0", wire.headers.Get("X-Msh-Version"))
					case PlatformMiniMax:
						require.Equal(t, "Bearer old-token", wire.headers.Get("Authorization"))
						require.Equal(t, "MiniMaxAgent", wire.headers.Get("User-Agent"))
					case PlatformZhipu:
						require.Equal(t, "Bearer pat-old-token-login-1", wire.headers.Get("Authorization"))
						// OUTBOUND_IDENTITY.md pins this complete Chat Completions
						// SDK fingerprint independently of the product version.
						require.Equal(t, "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22", wire.headers.Get("User-Agent"))
					}
					require.Empty(t, captures, "relink cannot cause an additional provider request")
				})
			}
		}
	}
}

// A legitimate same-login refresh may return a new account identity candidate.
// Its final headers and proxy must use that exact returned owner, rather than
// the pre-refresh identity context or a concurrently relinked selected row.
func TestNativeRefreshUsesFinalOwnerIdentityAndProxy(t *testing.T) {
	for _, mode := range []string{"before-expiry", "401-recovery", "models", "models-rebound-type", "quota"} {
		t.Run(mode, func(t *testing.T) {
			captures := make(chan nativeAcquiredOwnerWire, 4)
			oldProxy := nativeAcquiredOwnerProxy(t, "old", captures, mode == "401-recovery")
			newProxy := nativeAcquiredOwnerProxy(t, "new", captures, false)
			selected := nativeAcquiredOwnerAccount(PlatformKimi, oldProxy, "old-token", "2.3.0")
			if mode != "401-recovery" {
				selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			ctx = WithAccountOutboundIdentity(ctx, selected)
			recordRequestCredentialOwner(ctx, selected, "old-token")
			ctx = withRequestCredentialOwner(ctx, selected, "old-token")
			current := snapshotRequestCredentialAccount(selected)
			current.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "kimi", Version: "2.3.1"}
			repo := &refreshAPIAccountRepo{account: current}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
				refreshes++
				require.NoError(t, req.ParseForm())
				require.Equal(t, "old-refresh", req.Form.Get("refresh_token"))
				require.Equal(t, "kimi-code-cli/2.3.1", req.UserAgent(), "the issuer call must use the owner reread under the coordinator")
				require.Equal(t, "2.3.1", req.Header.Get("X-Msh-Version"))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"access_token":"refreshed-token","refresh_token":"rotated-refresh","expires_in":3600,"token_type":"Bearer"}`))}, nil
			})}
			replacement := nativeAcquiredOwnerAccount(PlatformKimi, newProxy, "new-token", "2.3.2")
			replacement.Credentials[domesticOAuthLoginIDCredential] = "new-login"
			if mode == "models-rebound-type" {
				replacement.Platform, replacement.Type = PlatformOpenAI, AccountTypeAPIKey
				replacement.Credentials = map[string]any{"api_key": "new-api-key"}
			}
			*selected = *replacement
			upstream := nativeAcquiredOwnerUpstream(t, selected.ID, 3)
			path := "/v1/messages"
			method := http.MethodPost
			if mode == "models" || mode == "models-rebound-type" || mode == "quota" {
				method = http.MethodGet
				path = "/v1/models"
			}
			if mode == "quota" {
				path = "/v1/usages"
			}
			target := current.GetAnthropicProtocolBaseURL() + path
			req, err := http.NewRequestWithContext(ctx, method, target, nil)
			require.NoError(t, err)
			var response *http.Response
			if mode == "models" || mode == "models-rebound-type" {
				response, err = (&AccountTestService{httpUpstream: upstream, cnOAuthService: oauth}).doUpstreamModelsRequest(req, selected)
			} else if mode == "quota" {
				response, err = oauth.roundTripNativeOAuthWithPrepare(req, selected, oauth.prepareKimiQuotaRequest, func(attempt *http.Request) (*http.Response, error) {
					return (&AccountTestService{httpUpstream: upstream, cnOAuthService: oauth}).doOpenAIAccountTestUpstreamPrepared(attempt, newProxy.URL(), selected, false)
				})
			} else {
				response, err = (&OpenAIGatewayService{httpUpstream: upstream, cnOAuthService: oauth}).doOpenAIUpstream(req, newProxy.URL(), selected)
			}
			require.NoError(t, err)
			require.NotNil(t, response)
			_ = response.Body.Close()
			require.Equal(t, 1, refreshes)
			require.Equal(t, 1, repo.successCASCalls)
			require.Equal(t, "refreshed-token", repo.account.GetCredential("access_token"))
			require.Equal(t, "rotated-refresh", repo.account.GetCredential("refresh_token"))
			if mode == "401-recovery" {
				first := <-captures
				require.Equal(t, "old", first.proxy)
				require.Equal(t, "Bearer old-token", first.headers.Get("Authorization"))
				require.Equal(t, "kimi-code-cli/2.3.0", first.headers.Get("User-Agent"))
			}
			final := <-captures
			require.Equal(t, "old", final.proxy)
			require.Equal(t, "Bearer refreshed-token", final.headers.Get("Authorization"))
			require.Equal(t, "kimi-code-cli/2.3.1", final.headers.Get("User-Agent"))
			require.Equal(t, "2.3.1", final.headers.Get("X-Msh-Version"))
			require.Empty(t, captures)
		})
	}
}
