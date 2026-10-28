//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type grokControlStrictRepo struct {
	AccountRepository
	mu      sync.Mutex
	account *Account
	writes  int
}

func (r *grokControlStrictRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return snapshotRequestCredentialAccount(r.account), nil
}

func (r *grokControlStrictRepo) CompareAndSwapGrokObservedModels(_ context.Context, owner *Account, expected, next json.RawMessage) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !grokCatalogOwnerMatches(r.account, owner) {
		return false, nil
	}
	var current any
	if r.account.Extra != nil {
		current = r.account.Extra[grokObservedModelsExtraKey]
	}
	encoded, err := json.Marshal(current)
	if err != nil || !jsonValuesEqual(encoded, expected) {
		return false, err
	}
	var value any
	if err := json.Unmarshal(next, &value); err != nil {
		return false, err
	}
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	r.account.Extra[grokObservedModelsExtraKey] = value
	r.writes++
	return true, nil
}

func (r *grokControlStrictRepo) ApplyCredentialStateMutation(_ context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || owner.ID != r.account.ID || !reflect.DeepEqual(owner.Credentials, r.account.Credentials) || !reflect.DeepEqual(owner.ProxyID, r.account.ProxyID) || !reflect.DeepEqual(owner.Proxy, r.account.Proxy) {
		return false, nil
	}
	r.writes++
	return true, nil
}

func (r *grokControlStrictRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, owner *Account, _ map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !auxiliaryProbeOwnersMatch(r.account, owner) {
		return false, nil
	}
	r.writes++
	return true, nil
}

func (*grokControlStrictRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	panic("observed model results require the captured grant CAS")
}

func grokControlStrictConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}
}

func grokControlStrictUpstream(sends *atomic.Int32) *codexModelsHTTPUpstreamStub {
	return &codexModelsHTTPUpstreamStub{do: func(request *http.Request, route string, _ int64, _ int) (*http.Response, error) {
		sends.Add(1)
		transport := &http.Transport{}
		if route != "" {
			parsed, err := url.Parse(route)
			if err != nil {
				return nil, err
			}
			transport.Proxy = http.ProxyURL(parsed)
		}
		// Response bodies are small; the transport has no idle pool after this
		// operation, including failed attempts.
		defer transport.CloseIdleConnections()
		return (&http.Client{Transport: transport}).Do(request)
	}}
}

func executeGrokControlStrict(ctx context.Context, operation string, account *Account, repo AccountRepository, proxyRepo ProxyRepository, upstream HTTPUpstream, destination string) bool {
	if operation == "observed_models" {
		svc := NewGrokQuotaService(repo, proxyRepo, nil, upstream, grokControlStrictConfig())
		return svc.syncGrokObservedModels(ctx, account) == nil
	}
	svc := &OpenAIGatewayService{cfg: grokControlStrictConfig(), httpUpstream: upstream}
	return svc.fetchGrokRequestCompressionCapability(ctx, account, destination+"/v1/settings")
}

// These are two independently implemented control sends. A shared HTTP proxy
// transport test cannot prove either sender supplies its configured route.
func TestGrokIndependentControlProxyFailuresNeverDialDirect(t *testing.T) {
	for _, operation := range []string{"observed_models", "compression_settings"} {
		for _, route := range []string{"missing", "lookup_failed", "mismatched", "invalid", "timeout", "refused", "authentication_required", "proxy", "unbound"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				var directConnections, proxyCalls, sends atomic.Int32
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":[{"id":"grok-test"}],"accept_request_encodings":["zstd"]}`))
				}))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directConnections.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				proxyID := int64(102)
				account := grokAuxiliaryAccount(AccountTypeOAuth, destination.URL+"/v1", "captured-token", nil)
				account.ProxyID = &proxyID
				var proxyRepo ProxyRepository
				switch route {
				case "missing":
				case "lookup_failed":
					proxyRepo = &openAISelectedProxyRepo{err: errors.New("proxy lookup unavailable")}
				case "mismatched":
					account.Proxy = &Proxy{ID: 999, Protocol: "http", Host: "127.0.0.1", Port: 80}
				case "invalid":
					account.Proxy = &Proxy{ID: proxyID, Protocol: "file", Host: "127.0.0.1", Port: 80}
				case "unbound":
					account.ProxyID = nil
					account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 1}
				case "refused":
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, err)
					account.Proxy = openAIAuthPlaneStrictProxy(t, "http://"+listener.Addr().String(), proxyID)
					require.NoError(t, listener.Close())
				default:
					release := make(chan struct{})
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						proxyCalls.Add(1)
						if route == "timeout" {
							<-release
							return
						}
						if route == "authentication_required" {
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"data":[{"id":"grok-test"}],"accept_request_encodings":["zstd"]}`))
					}))
					t.Cleanup(proxy.Close)
					t.Cleanup(func() { close(release) })
					account.Proxy = openAIAuthPlaneStrictProxy(t, proxy.URL, proxyID)
				}
				repo := &grokControlStrictRepo{account: snapshotRequestCredentialAccount(account)}
				ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer cancel()
				ok := executeGrokControlStrict(ctx, operation, account, repo, proxyRepo, grokControlStrictUpstream(&sends), destination.URL)
				if route == "proxy" || route == "unbound" {
					require.True(t, ok)
				} else if route != "authentication_required" || operation != "observed_models" {
					require.False(t, ok)
				}
				if route == "unbound" {
					require.Equal(t, int32(1), directConnections.Load())
				} else {
					require.Zero(t, directConnections.Load(), "a configured proxy failure cannot open a direct connection")
				}
				if route == "missing" || route == "lookup_failed" || route == "mismatched" || route == "invalid" {
					require.Zero(t, sends.Load(), "invalid captured proxy must stop before the sender")
				}
				if route == "proxy" || route == "timeout" || route == "authentication_required" {
					require.Positive(t, proxyCalls.Load())
				}
				if operation == "observed_models" {
					if route == "proxy" || route == "unbound" {
						require.Equal(t, 1, repo.writes)
					} else {
						require.Zero(t, repo.writes)
					}
				}
			})
		}
	}
}

func TestGrokIndependentControlRetainsCapturedRouteDuringProxyEdit(t *testing.T) {
	for _, operation := range []string{"observed_models", "compression_settings"} {
		t.Run(operation, func(t *testing.T) {
			var oldCalls, newCalls, sends atomic.Int32
			release, entered := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			oldProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				oldCalls.Add(1)
				close(entered)
				<-release
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"old-model"}],"accept_request_encodings":["zstd"]}`))
			}))
			t.Cleanup(oldProxy.Close)
			t.Cleanup(unblock)
			newProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				newCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"new-model"}],"accept_request_encodings":["zstd"]}`))
			}))
			t.Cleanup(newProxy.Close)
			proxy := openAIAuthPlaneStrictProxy(t, oldProxy.URL, 103)
			account := grokAuxiliaryAccount(AccountTypeOAuth, "http://grok-destination.invalid/v1", "captured-token", proxy)
			repo := &grokControlStrictRepo{account: snapshotRequestCredentialAccount(account)}
			bounded, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := WithOutboundIdentityScope(bounded, nil)
			recordRequestCredentialOwner(ctx, account, "captured-token")
			ctx = withRequestCredentialOwner(ctx, account, "captured-token")
			upstream := grokControlStrictUpstream(&sends)
			result := make(chan bool, 1)
			go func() {
				result <- executeGrokControlStrict(ctx, operation, account, repo, nil, upstream, "http://grok-destination.invalid")
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("captured control operation never reached its configured proxy")
			}
			// Edit the very same proxy row after the old physical send starts.
			replacement := openAIAuthPlaneStrictProxy(t, newProxy.URL, proxy.ID)
			*account.Proxy = *replacement
			account.Credentials["access_token"] = "replacement-token"
			repo.mu.Lock()
			repo.account = snapshotRequestCredentialAccount(account)
			repo.mu.Unlock()
			unblock()
			select {
			case accepted := <-result:
				require.Equal(t, operation != "observed_models", accepted, "a late model observation cannot certify the replacement owner")
			case <-ctx.Done():
				t.Fatal("captured control operation did not finish after its proxy replied")
			}
			require.Equal(t, int32(1), oldCalls.Load())
			require.Zero(t, newCalls.Load(), "an in-flight control request retains the captured proxy")
			require.Zero(t, repo.writes, "the late old-grant model result cannot mutate the new grant")
			require.True(t, executeGrokControlStrict(bounded, operation, account, repo, nil, upstream, "http://grok-destination.invalid"))
			require.Equal(t, int32(1), newCalls.Load(), "the next operation uses the committed replacement")
			if operation == "observed_models" {
				require.Equal(t, 1, repo.writes)
			}
		})
	}
}

func TestGrokCompressionWarmCacheCannotAuthorizeInvalidCapturedProxy(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)
	for _, malformed := range []string{"missing", "mismatched", "invalid"} {
		t.Run(malformed, func(t *testing.T) {
			account := grokCompressionTestAccount(104)
			id := int64(105)
			account.ProxyID = &id
			switch malformed {
			case "mismatched":
				account.Proxy = &Proxy{ID: 999, Protocol: "http", Host: "127.0.0.1", Port: 80}
			case "invalid":
				account.Proxy = &Proxy{ID: id, Protocol: "file", Host: "127.0.0.1", Port: 80}
			}
			key := grokRequestCompressionCacheKey{target: "https://cli-chat-proxy.grok.com", owner: account.ID, generation: credentialStateGeneration(account), proxy: grokRequestCompressionProxyKey(account)}
			grokRequestCompressionCacheStore(key, true, time.Now())
			var sends atomic.Int32
			svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: grokControlStrictUpstream(&sends)}
			require.False(t, svc.grokRequestCompressionCapabilityAdvertised(context.Background(), account))
			require.Zero(t, sends.Load())
		})
	}
}
