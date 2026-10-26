//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func openAIAuxiliaryProxy(t *testing.T, mode string, handler http.HandlerFunc) *Account {
	t.Helper()
	proxy := httptest.NewServer(handler)
	t.Cleanup(proxy.Close)
	parsed, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	id := int64(7941)
	account := &Account{ID: 7942, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-private-auxiliary", "base_url": "https://models.example.test/v1"},
		ProxyID:     &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: parsed.Hostname(), Port: port,
			Username: "private-proxy-user", Password: "private-proxy-password", Status: StatusActive}}
	setOpenAIStrictPreflightProxyMode(account, mode)
	return account
}

type auxiliaryProbeOwnerRepo struct {
	AccountRepository
	current                    *Account
	legacyWrites, atomicWrites int
	failStorage                bool
}

func (r *auxiliaryProbeOwnerRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.current, nil
}

func (r *auxiliaryProbeOwnerRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.legacyWrites++
	return nil
}

func (r *auxiliaryProbeOwnerRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	if r.failStorage {
		return false, errors.New("injected storage failure")
	}
	if !reflect.DeepEqual(expected.Credentials, r.current.Credentials) || !reflect.DeepEqual(expected.ProxyID, r.current.ProxyID) || !reflect.DeepEqual(expected.Proxy, r.current.Proxy) {
		return false, nil
	}
	r.atomicWrites++
	if r.current.Extra == nil {
		r.current.Extra = map[string]any{}
	}
	for key, value := range updates {
		r.current.Extra[key] = value
	}
	return true, nil
}

func (r *auxiliaryProbeOwnerRepo) ApplyCredentialStateMutation(ctx context.Context, expected *Account, mutation CredentialStateMutation) (bool, error) {
	return r.UpdateCNControlExtraIfUnchanged(ctx, expected, mutation.Extra)
}

// Reauthorization or a same-row proxy edit while a probe is in flight cannot
// force a replacement account to use the old owner's observed protocol.
func TestResponsesCapabilityProbeCannotPublishAnotherOwnersVerdict(t *testing.T) {
	for _, scenario := range []string{"unchanged", "relinked-key", "same-proxy-edited", "unbound", "storage-failure"} {
		t.Run(scenario, func(t *testing.T) {
			account := openAIAuxiliaryProxy(t, "valid-bound", func(http.ResponseWriter, *http.Request) {})
			repo := &auxiliaryProbeOwnerRepo{current: account, failStorage: scenario == "storage-failure"}
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
				require.Equal(t, "Bearer sk-private-auxiliary", req.Header.Get("Authorization"))
				require.Contains(t, proxy, "private-proxy-user")
				switch scenario {
				case "relinked-key":
					account.Credentials["api_key"] = "new-private-owner"
				case "same-proxy-edited":
					account.Proxy.Password = "new-private-proxy-password"
				case "unbound":
					account.ProxyID = nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"endpoint absent"}`))}, nil
			}}
			svc := &AccountTestService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
			svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
			require.Zero(t, repo.legacyWrites, "probe observations require atomic credential-owner persistence")
			if scenario == "unchanged" {
				require.Equal(t, 1, repo.atomicWrites)
				require.Equal(t, false, account.Extra["openai_responses_supported"])
			} else {
				require.Zero(t, repo.atomicWrites)
				require.Empty(t, account.Extra)
			}
		})
	}
}

// A capability probe can permanently change protocol routing. Invalid bound
// routes must neither send the private key nor publish a capability verdict.
func TestResponsesCapabilityProbeRejectsBrokenProxyBeforeSendOrVerdict(t *testing.T) {
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			account := openAIAuxiliaryProxy(t, mode, func(http.ResponseWriter, *http.Request) { t.Error("recorder owns this HTTP boundary") })
			writes := make(chan map[string]any, 1)
			repo := &responsesProbeTestRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{*account}}, updateExtraCalls: writes}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"function_call","name":"probe_ping"}]}`))}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
			svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
			if mode != "valid-bound" && mode != "explicitly-unbound" {
				require.Empty(t, upstream.requests, "a broken binding cannot reach the network boundary")
				require.Empty(t, writes, "a failed route cannot become a routing capability observation")
				return
			}
			require.Len(t, upstream.requests, 1)
			require.Len(t, writes, 1)
			require.Equal(t, "Bearer sk-private-auxiliary", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, DefaultOpenAICodexUserAgent, upstream.lastReq.Header.Get("User-Agent"))
			if mode == "valid-bound" {
				require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
			} else {
				require.Empty(t, upstream.lastProxyURL)
			}
		})
	}
}

// Models has its own cache and transport. Admission must run before cache
// lookup and Agent Identity bootstrap, for OAuth and API keys independently.
func TestCodexManifestRejectsBrokenProxyBeforeCacheOrSend(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, mode := range openAIStrictPreflightProxyModes {
			t.Run(accountType+"/"+mode, func(t *testing.T) {
				var direct, proxied atomic.Int64
				respond := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"models":[]}`) }
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); respond(w, r) }))
				t.Cleanup(origin.Close)
				account := openAIAuxiliaryProxy(t, mode, func(w http.ResponseWriter, r *http.Request) { proxied.Add(1); respond(w, r) })
				account.Type = accountType
				account.Credentials["access_token"] = "private-manifest-token"
				previous := chatgptCodexModelsURL
				chatgptCodexModelsURL = origin.URL
				t.Cleanup(func() { chatgptCodexModelsURL = previous })
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[]}`))}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				result, err := svc.FetchCodexModelsManifest(context.Background(), account, "", "")
				if mode != "valid-bound" && mode != "explicitly-unbound" {
					require.Zero(t, direct.Load(), "bound OAuth models cannot silently dial directly")
					require.Zero(t, proxied.Load(), "another proxy cannot replace the captured owner")
					require.Empty(t, upstream.requests)
					require.Nil(t, result)
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				if accountType == AccountTypeAPIKey {
					require.Len(t, upstream.requests, 1)
					if mode == "valid-bound" {
						require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
					} else {
						require.Empty(t, upstream.lastProxyURL)
					}
				} else if mode == "valid-bound" {
					require.Equal(t, int64(1), proxied.Load())
					require.Zero(t, direct.Load())
				} else {
					require.Equal(t, int64(1), direct.Load())
					require.Zero(t, proxied.Load())
				}
			})
		}
	}
}

func TestAgentRegistrationRejectsBrokenProxyBeforeAuthPlaneSend(t *testing.T) {
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			var direct, proxied atomic.Int64
			respond := func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"task_id":"task-from-original-owner"}`)
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); respond(w, r) }))
			t.Cleanup(origin.Close)
			previous := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = origin.URL
			t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = previous })
			account := openAIAuxiliaryProxy(t, mode, func(w http.ResponseWriter, r *http.Request) { proxied.Add(1); respond(w, r) })
			key, privateKey := newTestAgentIdentityKey(t)
			account.Type = AccountTypeOAuth
			account.Credentials = map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID, "agent_private_key": privateKey}
			identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, nil)
			taskID, err := registerAgentIdentityTaskWithIdentity(context.Background(), account, identity)
			if mode != "valid-bound" && mode != "explicitly-unbound" {
				require.Zero(t, direct.Load(), "bound registration cannot send the signed private assertion directly")
				require.Zero(t, proxied.Load())
				require.Empty(t, taskID)
				require.Error(t, err)
				require.NotContains(t, err.Error(), privateKey)
				require.NotContains(t, err.Error(), "private-proxy-password")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "task-from-original-owner", taskID)
			if mode == "valid-bound" {
				require.Equal(t, int64(1), proxied.Load())
				require.Zero(t, direct.Load())
			} else {
				require.Equal(t, int64(1), direct.Load())
				require.Zero(t, proxied.Load())
			}
		})
	}
}

// Grok has a separate discovery/public-catalog path; it does not call this
// OpenAI-compatible helper. Its independent discovery regression is maintained
// in grok_observed_models_strict_proxy_test.go.
func TestOpenAICompatiblePublicModelListsRejectBrokenProxyBeforeCacheOrSend(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, mode := range openAIStrictPreflightProxyModes {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				account := openAIAuxiliaryProxy(t, mode, func(http.ResponseWriter, *http.Request) {})
				account.Platform = platform
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				result, err := svc.FetchOpenAIModelsList(context.Background(), account)
				if mode != "valid-bound" && mode != "explicitly-unbound" {
					require.Empty(t, upstream.requests, "model discovery must not bypass a broken bound route")
					require.Nil(t, result)
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				if mode == "valid-bound" {
					require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
				} else {
					require.Empty(t, upstream.lastProxyURL)
				}
			})
		}
	}
}

func TestSevenProviderAdminModelListRejectsBrokenProxyBeforeCredentialOrSend(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, mode := range openAIStrictPreflightProxyModes {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				account := openAIAuxiliaryProxy(t, mode, func(http.ResponseWriter, *http.Request) {})
				account.Platform = platform
				upstream := &httpUpstreamRecorder{err: errors.New("isolated model transport failure")}
				svc := &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
				models, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
				require.Error(t, err)
				require.Empty(t, models)
				if mode != "valid-bound" && mode != "explicitly-unbound" {
					require.Empty(t, upstream.requests, "admin discovery must reject broken bindings before credential or HTTP work")
					return
				}
				require.Len(t, upstream.requests, 1)
				if mode == "valid-bound" {
					require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
				} else {
					require.Empty(t, upstream.lastProxyURL)
				}
			})
		}
	}
}

func TestCodexManifestShadowUsesCredentialOwnersCapturedProxy(t *testing.T) {
	var direct, proxied atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { direct.Add(1); _, _ = io.WriteString(w, `{"models":[]}`) }))
	t.Cleanup(origin.Close)
	previous := chatgptCodexModelsURL
	chatgptCodexModelsURL = origin.URL
	t.Cleanup(func() { chatgptCodexModelsURL = previous })
	parent := openAIAuxiliaryProxy(t, "valid-bound", func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		require.Equal(t, "Bearer parent-private-token", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"models":[]}`)
	})
	parent.Type = AccountTypeOAuth
	parent.Credentials = map[string]any{"access_token": "parent-private-token", "chatgpt_account_id": "parent-account"}
	shadow := &Account{ID: 7943, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID, Concurrency: 1}
	svc := &OpenAIGatewayService{accountRepo: &stubOpenAIAccountRepo{accounts: []Account{*parent}}}
	manifest, err := svc.FetchCodexModelsManifest(context.Background(), shadow, "", "")
	require.NoError(t, err)
	require.NotNil(t, manifest)
	require.Zero(t, direct.Load(), "the shadow's unbound route cannot authorize direct use of its parent's credential")
	require.Equal(t, int64(1), proxied.Load())
}
