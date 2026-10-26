//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func deepSeekNativeModelsAccount(kind, key, version string) *Account {
	credentials := map[string]any{
		"api_protocol":             APIProtocolAnthropic,
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "deepseek", Version: version},
	}
	if kind == AccountTypeOAuth {
		credentials["oauth_provider"], credentials["oauth_region"] = PlatformDeepseek, "cn"
		credentials["access_token"] = key
	} else {
		credentials["api_key"] = key
	}
	return &Account{ID: 8201, Platform: PlatformDeepseek, Type: kind, Credentials: credentials}
}

func deepSeekModelsResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

// The official Messages route uses /models for both credential types. Rows
// requiring newer clients or unusable Messages capabilities are never offered.
func TestDeepSeekHarnessNativeDiscoveryCredentialsAndGates(t *testing.T) {
	const listing = `{"data":[
	 {"id":"deepseek-flash","name":"DeepSeek Flash","api_capabilities":{"anthropic_messages":{}},"context_window":1048576,"max_output_tokens":8192},
	 {"id":"rc3-model","client_requirements":{"deepseek-harness":{"min_version":"0.2.1-rc.3"}},"api_capabilities":{"anthropic_messages":{}},"input_modalities":["text","image"]},
	 {"id":"too-new","client_requirements":{"deepseek-harness":{"min_version":"0.2.1"}},"api_capabilities":{"anthropic_messages":{}}},
	 {"id":"unknown","api_capabilities":{"anthropic_messages":{}}},
	 {"id":"no-messages","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"chat_completions":{}}},
	 {"id":"bad-capacity","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"anthropic_messages":{}},"context_window":-1},
	 {"id":"no-text","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"anthropic_messages":{}},"output_modalities":["image"]}
	]}`
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, version := range []string{"0.2.1-rc.2", "0.2.1-rc.3"} {
			t.Run(kind+"/"+version, func(t *testing.T) {
				account := deepSeekNativeModelsAccount(kind, "owner-key", version)
				upstream := &httpUpstreamRecorder{resp: deepSeekModelsResponse(listing)}
				svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
				models, body, observedOwner, err := svc.fetchUpstreamModelList(context.Background(), account)
				require.NoError(t, err)
				require.NotNil(t, observedOwner)
				require.Equal(t, account.ID, observedOwner.ID)
				require.Equal(t, "owner-key", observedOwner.GetOpenAIProtocolAPIKey())
				want := []string{"deepseek-flash"}
				if version == "0.2.1-rc.3" {
					want = append(want, "rc3-model")
				}
				require.ElementsMatch(t, want, models)
				require.Equal(t, "https://api.deepseek.com/models", upstream.lastReq.URL.String())
				require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
				require.Equal(t, "deepseek-harness/"+version+" (+https://github.com/deepseek-ai/deepseek-harness)", upstream.lastReq.UserAgent())
				require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
				if kind == AccountTypeOAuth {
					require.Equal(t, "owner-key", upstream.lastReq.Header.Get("x-dsh-auth-token"))
					require.Empty(t, upstream.lastReq.Header.Get("x-api-key"))
				} else {
					require.Equal(t, "owner-key", upstream.lastReq.Header.Get("x-api-key"))
					require.Empty(t, upstream.lastReq.Header.Get("x-dsh-auth-token"))
				}
				_, metadata, err := extractDeepSeekHarnessCatalog(body, "")
				require.NoError(t, err)
				require.Equal(t, "DeepSeek Flash", metadata["deepseek-flash"].DisplayName)
				require.EqualValues(t, 1048576, metadata["deepseek-flash"].ContextWindow)
			})
		}
	}
}

func TestDeepSeekHarnessDiscoveryKeepsCompatibleProtocols(t *testing.T) {
	for _, tc := range []struct{ name, protocol, base, target string }{
		{"chat", APIProtocolChatCompletions, "", "https://api.deepseek.com/v1/models"},
		{"responses", APIProtocolResponses, "", "https://api.deepseek.com/v1/models"},
		{"adaptive", APIProtocolAdaptive, "", "https://api.deepseek.com/v1/models"},
		{"compatible", APIProtocolChatCompletions, "https://compatible.example/v1", "https://compatible.example/v1/models"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := deepSeekNativeModelsAccount(AccountTypeAPIKey, "compatible-key", "0.2.1-rc.3")
			account.Credentials["api_protocol"], account.Credentials["base_url"] = tc.protocol, tc.base
			upstream := &httpUpstreamRecorder{resp: deepSeekModelsResponse(`{"data":[{"id":"compatible-model"}]}`)}
			svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			models, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, []string{"compatible-model"}, models)
			require.Equal(t, tc.target, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer compatible-key", upstream.lastReq.Header.Get("Authorization"))
			require.Empty(t, upstream.lastReq.Header.Get("x-api-key"))
		})
	}
	custom := deepSeekNativeModelsAccount(AccountTypeAPIKey, "custom-key", "0.2.1-rc.3")
	custom.Credentials["base_url"] = "https://api.deepseek.com.attacker.example/anthropic"
	require.False(t, usesDeepSeekHarnessModels(custom), "a compatible origin must not acquire official Harness catalog semantics")
}

func TestDeepSeekHarnessNativeDiscoveryRequiresAPIKey(t *testing.T) {
	account := deepSeekNativeModelsAccount(AccountTypeAPIKey, "  ", "0.2.1-rc.3")
	upstream := &httpUpstreamRecorder{}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	_, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
	require.ErrorContains(t, err, "No DeepSeek API key")
	require.Empty(t, upstream.requests)
}

// Building a second request after an API key/login rotation must not rewrite
// the old attempt's credential, proxy, or client version admission snapshot.
func TestDeepSeekHarnessDiscoveryFreezesCredentialOwnerOnRotation(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			account := deepSeekNativeModelsAccount(kind, "old-key", "0.2.1-rc.2")
			oldProxy := &Proxy{ID: 91, Protocol: "http", Host: "old-proxy.example", Port: 8080}
			account.ProxyID, account.Proxy = &oldProxy.ID, oldProxy
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			upstream := &httpUpstreamRecorder{resp: deepSeekModelsResponse(`{}`)}
			svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			oldRequest, err := svc.buildUpstreamModelsRequest(ctx, account)
			require.NoError(t, err)
			oldOwner, err := requestCredentialAccount(oldRequest.Context(), account)
			require.NoError(t, err)
			newProxy := &Proxy{ID: 92, Protocol: "http", Host: "new-proxy.example", Port: 8080}
			account.ProxyID, account.Proxy = &newProxy.ID, newProxy
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "deepseek", Version: "0.2.1-rc.3"}
			if kind == AccountTypeOAuth {
				account.Credentials["access_token"] = "new-key"
			} else {
				account.Credentials["api_key"] = "new-key"
			}
			newRequest, err := svc.buildUpstreamModelsRequest(ctx, account)
			require.NoError(t, err)
			for _, tc := range []struct {
				request             *http.Request
				key, version, proxy string
			}{{oldRequest, "old-key", "0.2.1-rc.2", oldProxy.URL()}, {newRequest, "new-key", "0.2.1-rc.3", newProxy.URL()}} {
				response, err := svc.doUpstreamModelsRequest(tc.request, account)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.NoError(t, OutboundPreparationError(upstream.lastReq.Context()))
				require.Equal(t, tc.proxy, upstream.lastProxyURL)
				identity, ok := outboundidentity.FromContext(upstream.lastReq.Context())
				require.True(t, ok)
				require.Equal(t, tc.version, identity.Version)
				header := "x-api-key"
				if kind == AccountTypeOAuth {
					header = "x-dsh-auth-token"
				}
				require.Equal(t, tc.key, upstream.lastReq.Header.Get(header))
			}
			require.Equal(t, "0.2.1-rc.2", oldOwner.Credentials[outboundIdentityCredential].(OutboundIdentitySelection).Version)
		})
	}
}

type deepSeekCatalogFenceRepo struct {
	upstreamModelMetadataRepoStub
	current            *Account
	owner              *Account
	conditionalUpdates map[string]any
	conditionalCalls   int
}

func (r *deepSeekCatalogFenceRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, owner *Account, updates map[string]any) (bool, error) {
	r.conditionalCalls++
	r.owner, r.conditionalUpdates = snapshotRequestCredentialAccount(owner), updates
	current := r.current
	if owner == nil || current == nil || owner.ID != current.ID || owner.Platform != current.Platform || owner.Type != current.Type ||
		!reflect.DeepEqual(owner.ProxyID, current.ProxyID) || !reflect.DeepEqual(owner.Proxy, current.Proxy) {
		return false, nil
	}
	expectedJSON, expectedErr := json.Marshal(owner.Credentials)
	currentJSON, currentErr := json.Marshal(current.Credentials)
	return expectedErr == nil && currentErr == nil && string(expectedJSON) == string(currentJSON), nil
}

// An old discovery response cannot publish metadata after the credential owner
// changes. CAS rejection must not fall back to the unconditional account write.
func TestDeepSeekHarnessCatalogPersistenceIsCredentialConditional(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, rotate := range []bool{false, true} {
			name := kind + "/unchanged"
			if rotate {
				name = kind + "/rotated"
			}
			t.Run(name, func(t *testing.T) {
				account := deepSeekNativeModelsAccount(kind, "old-key", "0.2.1-rc.2")
				repo := &deepSeekCatalogFenceRepo{current: account}
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					if req.URL.String() == modelsDevRegistryURL {
						return deepSeekModelsResponse(`{"deepseek":{"api":"https://api.deepseek.com","models":{"deepseek-flash":{"id":"deepseek-flash","reasoning":false,"limit":{"context":1048576},"modalities":{"input":["text"]}}}}}`), nil
					}
					if rotate {
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "deepseek", Version: "0.2.1-rc.3"}
						if kind == AccountTypeOAuth {
							account.Credentials["access_token"] = "new-key"
						} else {
							account.Credentials["api_key"] = "new-key"
						}
					}
					return deepSeekModelsResponse(`{"data":[{"id":"deepseek-flash","name":"Native Flash","context_window":1048576,"api_capabilities":{"anthropic_messages":{}}},{"id":"rc3-only","client_requirements":{"deepseek-harness":{"min_version":"0.2.1-rc.3"}},"api_capabilities":{"anthropic_messages":{}}}]}`), nil
				}}
				svc := &AccountTestService{httpUpstream: upstream, accountRepo: repo, cfg: upstreamModelSyncTestConfig()}
				catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
				require.Equal(t, 1, repo.conditionalCalls)
				require.Nil(t, repo.updates, "never issue an unconditional metadata write for a native owner")
				require.Contains(t, repo.conditionalUpdates, UpstreamModelMetadataExtraKey)
				ownerCredentials, marshalErr := json.Marshal(repo.owner.Credentials)
				require.NoError(t, marshalErr)
				require.Equal(t, "0.2.1-rc.2", gjson.GetBytes(ownerCredentials, "outbound_identity.version").String())
				if kind == AccountTypeOAuth {
					require.Equal(t, "old-key", repo.owner.GetCredential("access_token"))
				} else {
					require.Equal(t, "old-key", repo.owner.GetCredential("api_key"))
				}
				if rotate {
					var syncErr *UpstreamModelSyncError
					require.ErrorAs(t, err, &syncErr)
					require.Equal(t, UpstreamModelSyncErrorInternal, syncErr.Kind, "a lost owner CAS must report failure instead of publishing stale metadata")
					require.Nil(t, catalog)
					require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
				} else {
					require.NoError(t, err)
					require.Equal(t, []string{"deepseek-flash"}, catalog.Models)
					require.Equal(t, "Native Flash", catalog.Metadata["deepseek-flash"].DisplayName)
					require.NotContains(t, catalog.Metadata, "rc3-only")
					require.NotNil(t, account.GetUpstreamModelMetadataSnapshot())
				}
			})
		}
	}
}
