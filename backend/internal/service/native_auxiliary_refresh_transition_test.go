//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

type auxiliaryRefreshTransitionRepo struct {
	refreshAPIAccountRepo
	refreshWrites, quotaWrites, unconditionalWrites int
	refreshCASAttempts                              int
	persistFailure                                  bool
}

func sameAuxiliaryRefreshStoredOwner(current, expected *Account) bool {
	if current == nil || expected == nil {
		return false
	}
	currentDocument, currentErr := json.Marshal(current.Credentials)
	expectedDocument, expectedErr := json.Marshal(expected.Credentials)
	return current != nil && expected != nil && current.Status == StatusActive && current.ID == expected.ID &&
		current.Platform == expected.Platform && current.Type == expected.Type &&
		currentErr == nil && expectedErr == nil && string(currentDocument) == string(expectedDocument) &&
		reflect.DeepEqual(current.ProxyID, expected.ProxyID) && reflect.DeepEqual(current.Proxy, expected.Proxy)
}

func (r *auxiliaryRefreshTransitionRepo) UpdateOAuthRefreshCredentialsIfUnchanged(_ context.Context, expected *Account, credentials map[string]any) (bool, error) {
	r.refreshCASAttempts++
	if r.persistFailure {
		return false, errors.New("independent storage failure")
	}
	if !sameAuxiliaryRefreshStoredOwner(r.account, expected) {
		return false, nil
	}
	r.account = snapshotRequestCredentialAccount(r.account)
	r.account.Credentials = shallowCopyMap(credentials)
	r.refreshWrites++
	return true, nil
}

func (r *auxiliaryRefreshTransitionRepo) UpdateCNProviderQuotaIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	if !sameAuxiliaryRefreshStoredOwner(r.account, expected) {
		return false, nil
	}
	r.quotaWrites++
	return true, nil
}

func (r *auxiliaryRefreshTransitionRepo) UpdateCredentials(context.Context, int64, map[string]any) error {
	r.unconditionalWrites++
	return errors.New("unconditional credential mutation is forbidden")
}

func (r *auxiliaryRefreshTransitionRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.unconditionalWrites++
	return errors.New("unconditional quota mutation is forbidden")
}

// Only Kimi and MiniMax implement native refresh. The separately implemented
// Kimi models/quota and MiniMax four-request billing chain must all stop after
// a refresh loses ownership, instead of sending the old grant through a new
// proxy or publishing the provider's already-consumed rotation unconditionally.
func TestNativeAuxiliaryRefreshContainsConcurrentOwnerChanges(t *testing.T) {
	for _, operation := range []string{"kimi-models", "kimi-quota", "minimax-quota"} {
		for _, timing := range []string{"pre-expiry", "401-recovery"} {
			for _, change := range []string{"unchanged", "new-login", "proxy-endpoint", "proxy-auth", "proxy-unbind", "disabled", "persist-failure", "cancelled"} {
				t.Run(operation+"/"+timing+"/"+change, func(t *testing.T) {
					platform := PlatformKimi
					if operation == "minimax-quota" {
						platform = PlatformMiniMax
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					var requests []nativeAcquiredOwnerWire
					var requestMu sync.Mutex
					proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						requestMu.Lock()
						requests = append(requests, nativeAcquiredOwnerWire{proxy: req.URL.Path, headers: req.Header.Clone()})
						requestMu.Unlock()
						if timing == "401-recovery" && req.Header.Get("Authorization") == "Bearer old-token" {
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
						var reply *http.Response
						if platform == PlatformMiniMax {
							var err error
							reply, err = miniMaxQuotaTestReply(req)
							if err != nil {
								t.Error(err)
								w.WriteHeader(http.StatusInternalServerError)
								return
							}
						} else {
							reply = cnRecoveryResponse(http.StatusOK, `{"usages":{"limit_5h":{"used_ratio":0.1}},"data":[]}`)
						}
						defer func() { _ = reply.Body.Close() }()
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(reply.StatusCode)
						_, _ = io.Copy(w, reply.Body)
					}))
					defer proxyServer.Close()
					endpoint, err := url.Parse(proxyServer.URL)
					require.NoError(t, err)
					port, err := strconv.Atoi(endpoint.Port())
					require.NoError(t, err)
					proxy := &Proxy{ID: 90, Protocol: "http", Host: endpoint.Hostname(), Port: port, Status: StatusActive}
					selected := nativeAcquiredOwnerAccount(platform, proxy, "old-token", "2.3.1")
					selected.Credentials["account_mode"] = AccountModeCoding
					if timing == "pre-expiry" {
						selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
					}
					repo := &auxiliaryRefreshTransitionRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}, persistFailure: change == "persist-failure"}
					oauth := NewCNOAuthService(nil, repo, nil)
					oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
					refreshes := 0
					oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
						refreshes++
						require.NoError(t, req.ParseForm())
						require.Equal(t, "old-refresh", req.Form.Get("refresh_token"))
						replacement := snapshotRequestCredentialAccount(repo.account)
						switch change {
						case "new-login":
							replacement.Credentials[domesticOAuthLoginIDCredential] = "replacement-login"
							replacement.Credentials["access_token"] = "replacement-token"
							replacement.Credentials["refresh_token"] = "replacement-refresh"
						case "proxy-endpoint":
							replacement.Proxy.Host = "replacement-proxy.invalid"
						case "proxy-auth":
							replacement.Proxy.Username, replacement.Proxy.Password = "replacement-user", "replacement-password"
						case "proxy-unbind":
							replacement.ProxyID, replacement.Proxy = nil, nil
						case "disabled":
							replacement.Status = StatusDisabled
						case "cancelled":
							cancel()
						}
						repo.account = replacement
						// MiniMax's official refresh grant must carry agent.default;
						// otherwise the credential client rejects it before the owner
						// persistence boundary this scenario is intended to exercise.
						return cnRecoveryResponse(http.StatusOK, `{"access_token":"refreshed-token","refresh_token":"rotated-refresh","expires_in":3600,"token_type":"Bearer","scope":"agent.default"}`), nil
					})}
					upstream := nativeAcquiredOwnerUpstream(t, selected.ID, selected.Concurrency)
					if operation == "kimi-models" {
						req, buildErr := http.NewRequestWithContext(ctx, http.MethodGet, selected.GetAnthropicProtocolBaseURL()+"/v1/models", nil)
						require.NoError(t, buildErr)
						response, sendErr := (&AccountTestService{httpUpstream: upstream, cnOAuthService: oauth}).doUpstreamModelsRequest(req, selected)
						err = sendErr
						if response != nil {
							_ = response.Body.Close()
						}
					} else {
						cfg := cnProbeAllowlistConfig("api.kimi.com", "agent.minimaxi.com", "www.minimaxi.com")
						quota := NewCNProviderQuotaService(repo, nil, upstream, cfg)
						quota.cnOAuthService = oauth
						var result *CNProviderQuotaProbeResult
						var sendErr error
						if change == "cancelled" {
							// Cancel the refresh operation itself. The public quota
							// singleflight deliberately detaches a single waiter's
							// cancellation so other waiters can finish their probe.
							result, sendErr = quota.queryUsageForAccount(ctx, selected)
						} else {
							result, sendErr = quota.QueryUsageForAccount(ctx, selected)
						}
						err = sendErr
						if change == "unchanged" {
							require.NoError(t, sendErr, "an unchanged grant must complete the native quota chain")
							require.NotNil(t, result)
							require.True(t, result.Success, "quota result: %+v", result)
							require.True(t, result.Persisted)
						}
					}
					require.Equal(t, 1, refreshes, "the obsolete rotation must not be retried")
					if change == "cancelled" {
						require.Zero(t, repo.refreshCASAttempts, "cancellation must stop before credential persistence")
					} else {
						require.Equal(t, 1, repo.refreshCASAttempts, "a valid refresh grant must reach the owner CAS; a malformed fixture must not masquerade as a fenced owner")
					}
					require.Zero(t, repo.unconditionalWrites)
					requestMu.Lock()
					capturedRequests := append([]nativeAcquiredOwnerWire(nil), requests...)
					requestMu.Unlock()
					if change == "unchanged" {
						require.NoError(t, err)
						require.Equal(t, 1, repo.refreshWrites)
						freshRequests := 1
						if platform == PlatformMiniMax {
							freshRequests = 4
						}
						oldRequests := 0
						if timing == "401-recovery" {
							oldRequests = 1
							require.NotEmpty(t, capturedRequests)
							require.Equal(t, "Bearer old-token", capturedRequests[0].headers.Get("Authorization"))
						}
						require.Len(t, capturedRequests, oldRequests+freshRequests)
						for _, request := range capturedRequests[oldRequests:] {
							require.Equal(t, "Bearer refreshed-token", request.headers.Get("Authorization"))
							if platform == PlatformKimi {
								require.Equal(t, "kimi-code-cli/2.3.1", request.headers.Get("User-Agent"))
							}
						}
						if strings.HasSuffix(operation, "quota") {
							require.Equal(t, 1, repo.quotaWrites)
						}
					} else {
						require.Error(t, err)
						require.Zero(t, repo.refreshWrites)
						require.Zero(t, repo.quotaWrites)
						wantRequests := 0
						if timing == "401-recovery" {
							wantRequests = 1
						}
						require.Len(t, capturedRequests, wantRequests, "losing refresh must not start a new auxiliary request")
					}
				})
			}
		}
	}
}

func (r *auxiliaryRefreshTransitionRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	return r.UpdateCNProviderQuotaIfUnchanged(ctx, expected, updates)
}

func (r *auxiliaryRefreshTransitionRepo) UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, expected, credentials)
}
