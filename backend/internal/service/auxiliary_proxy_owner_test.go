//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pool "github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// The account's explicitly bound route is an egress boundary. A missing
// hydrated proxy is not an operator request to disconnect that route. These
// regressions assert transport behavior, not merely a helper's return value.
type auxiliaryOwnerRepo struct {
	AccountRepository
	mu      sync.Mutex
	account *Account
	writes  []map[string]any
}

func (r *auxiliaryOwnerRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotRequestCredentialAccount(r.account), nil
}

func (r *auxiliaryOwnerRepo) replace(account *Account) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account = snapshotRequestCredentialAccount(account)
}

func (*auxiliaryOwnerRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }

func (r *auxiliaryOwnerRepo) UpdateOpenAICodexUsageIfUnchanged(_ context.Context, targetID int64, owner *Account, updates map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner == nil || targetID != owner.ID || !sameRequestCredentialGrant(r.account, owner) {
		return false, nil
	}
	r.writes = append(r.writes, cloneRequestCredentialMap(updates))
	return true, nil
}

func (r *auxiliaryOwnerRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, owner *Account, updates map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !auxiliaryProbeOwnersMatch(r.account, owner) {
		return false, nil
	}
	if updates[owner.Platform+"_balance_low"] != nil && (r.account.Schedulable != owner.Schedulable ||
		!reflect.DeepEqual(r.account.TempUnschedulableUntil, owner.TempUnschedulableUntil) || r.account.TempUnschedulableReason != owner.TempUnschedulableReason) {
		return false, nil
	}
	r.writes = append(r.writes, cloneRequestCredentialMap(updates))
	return true, nil
}

func auxiliaryProbeOwnersMatch(current, owner *Account) bool {
	if current == nil || owner == nil || current.ID != owner.ID || current.Platform != owner.Platform || current.Type != owner.Type || current.Status != owner.Status || !reflect.DeepEqual(current.ProxyID, owner.ProxyID) {
		return false
	}
	currentCredentials, currentErr := json.Marshal(current.Credentials)
	ownerCredentials, ownerErr := json.Marshal(owner.Credentials)
	return currentErr == nil && ownerErr == nil && string(currentCredentials) == string(ownerCredentials) && (current.ProxyID == nil || reflect.DeepEqual(current.Proxy, owner.Proxy))
}

func (r *auxiliaryOwnerRepo) ApplyGrokRequestOutcomeIfUnchanged(_ context.Context, owner *Account, outcome GrokRequestOutcome) (GrokRequestOutcomeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !auxiliaryProbeOwnersMatch(r.account, owner) || r.account.Schedulable != owner.Schedulable ||
		!reflect.DeepEqual(r.account.TempUnschedulableUntil, owner.TempUnschedulableUntil) || r.account.TempUnschedulableReason != owner.TempUnschedulableReason ||
		!reflect.DeepEqual(r.account.RateLimitResetAt, owner.RateLimitResetAt) || !reflect.DeepEqual(r.account.OverloadUntil, owner.OverloadUntil) {
		return GrokRequestOutcomeResult{}, nil
	}
	r.writes = append(r.writes, cloneRequestCredentialMap(outcome.Extra))
	return GrokRequestOutcomeResult{Applied: true, RecoveryGeneration: runtimeRecoveryGeneration(owner)}, nil
}

const auxiliaryOwnerUA = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
const auxiliaryReplacementUA = "codex-tui/0.165.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.165.0)"

func auxiliaryCodexAccount(token, ua string, proxyID int64) *Account {
	a := &Account{ID: 883001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": token, "chatgpt_account_id": "owner-" + token,
			"user_agent": ua, "expires_at": "2099-01-01T00:00:00Z"}}
	if proxyID > 0 {
		a.ProxyID = &proxyID
		a.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy-" + token + ".invalid", Port: 8080, Status: StatusActive}
	}
	return a
}

type auxiliaryRoundTripper func(*http.Request) (*http.Response, error)

func (f auxiliaryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodexUsageProbeRejectsMissingConfiguredProxyBeforeTransport(t *testing.T) {
	a := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	a.Proxy = nil // Real hydration has this shape after the proxy is soft-deleted.
	client, err := pool.GetClient(pool.Options{Timeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second})
	require.NoError(t, err)
	prior := client.Transport
	sends := 0
	client.Transport = auxiliaryRoundTripper(func(r *http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	t.Cleanup(func() { client.Transport = prior })
	_, err = (&AccountUsageService{}).probeOpenAICodexSnapshot(context.Background(), a)
	require.Error(t, err)
	require.Zero(t, sends, "configured proxy loss must not reach the direct HTTP transport")
}

func TestWHAMRejectsMissingConfiguredProxyBeforeCreatingClient(t *testing.T) {
	a := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	a.Proxy = nil
	repo := &auxiliaryOwnerRepo{account: a}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(a): "fake-old"}}
	clients := 0
	s := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), func(string) (*req.Client, error) {
		clients++
		return nil, fmt.Errorf("unexpected direct-egress client")
	})
	_, err := s.QueryUsage(context.Background(), a.ID)
	require.Error(t, err)
	require.Zero(t, clients, "missing bound proxy must fail before client construction")
}

type auxiliaryQuotaUpstream struct {
	recordingHTTPUpstream
	proxy string
}

func (u *auxiliaryQuotaUpstream) Do(_ *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.proxy = proxy
	return nil, fmt.Errorf("mock-only transport stop")
}

func TestNativeAPIKeyQuotaRejectsMissingConfiguredProxyBeforeTransport(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		t.Run(platform, func(t *testing.T) {
			a := codingAccount(platform)
			id := int64(7)
			a.ProxyID = &id
			repo := &fakeCNProbeAccountRepo{account: a}
			upstream := &auxiliaryQuotaUpstream{}
			_, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsageForAccount(context.Background(), a)
			require.Error(t, err)
			require.Zero(t, upstream.calls, "a configured but unavailable proxy must never become direct egress")
		})
	}
}

func TestWHAMUsesOneOwnerAcrossQuotaAndCreditRequests(t *testing.T) {
	old := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	next := auxiliaryCodexAccount("fake-new", auxiliaryReplacementUA, 8)
	repo := &auxiliaryOwnerRepo{account: old}
	type wireRequest struct{ path, authorization, accountID, ua string }
	var seen []wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, wireRequest{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id"), r.Header.Get("User-Agent")})
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/rate-limit-reset-credits") {
			fmt.Fprint(w, `{"available_count":0,"credits":[]}`)
		} else {
			fmt.Fprint(w, `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":12,"limit_window_seconds":18000}}}`)
		}
	}))
	defer srv.Close()
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(old): "fake-old"}}
	var routes []string
	factory := func(proxy string) (*req.Client, error) {
		routes = append(routes, proxy)
		// The prior grant was acquired. Replacement takes effect for the next
		// operation; the quota/credit pair must retain the old grant and route.
		repo.replace(next)
		cache.tokens[OpenAITokenCacheKey(next)] = "fake-new"
		return newQuotaRedirectingFactory(srv)(proxy)
	}
	s := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), factory)
	usage, err := s.QueryUsage(context.Background(), old.ID)
	require.NoError(t, err)
	require.Empty(t, repo.writes, "late old-owner quota must not persist onto the replacement row")
	require.Error(t, s.CacheCreditsSnapshot(context.Background(), old.ID, usage), "an old usage payload cannot update the replacement cache")
	require.Error(t, s.CacheResetCreditsSnapshot(context.Background(), old.ID, usage.RateLimitResetCredits), "old reset credits cannot update the replacement cache")
	_, err = s.QueryUsage(context.Background(), next.ID)
	require.NoError(t, err)
	require.Len(t, repo.writes, 1, "the next operation can persist its new owner's quota")
	require.Equal(t, []string{old.Proxy.URL(), next.Proxy.URL()}, routes)
	require.Equal(t, []wireRequest{
		{"/backend-api/wham/usage", "Bearer fake-old", "owner-fake-old", auxiliaryOwnerUA},
		{"/backend-api/wham/rate-limit-reset-credits", "Bearer fake-old", "owner-fake-old", auxiliaryOwnerUA},
		{"/backend-api/wham/usage", "Bearer fake-new", "owner-fake-new", auxiliaryReplacementUA},
		{"/backend-api/wham/rate-limit-reset-credits", "Bearer fake-new", "owner-fake-new", auxiliaryReplacementUA},
	}, seen)
}

func TestWHAMProxyFailuresNeverReachDirectEndpoint(t *testing.T) {
	for _, failure := range []string{"timeout", "refused", "authentication_required"} {
		t.Run(failure, func(t *testing.T) {
			var directHits, proxyHits atomic.Int32
			direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				directHits.Add(1)
				fmt.Fprint(w, `{"rate_limit":{"allowed":true}}`)
			}))
			defer direct.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyHits.Add(1)
				if failure == "timeout" {
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
					return
				}
				w.WriteHeader(http.StatusProxyAuthRequired)
			}))
			defer proxy.Close()
			proxyURL := proxy.URL
			if failure == "refused" {
				proxy.Close()
			}
			parsed, _ := url.Parse(proxyURL)
			_, portString, _ := net.SplitHostPort(parsed.Host)
			var port int
			count, err := fmt.Sscanf(portString, "%d", &port)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			a := auxiliaryCodexAccount("fake-proxy", auxiliaryOwnerUA, 7)
			a.Proxy = &Proxy{ID: 7, Protocol: "http", Host: parsed.Hostname(), Port: port, Status: StatusActive}
			repo := &auxiliaryOwnerRepo{account: a}
			cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(a): "fake-proxy"}}
			factory := func(route string) (*req.Client, error) {
				require.Equal(t, proxyURL, route)
				client, _ := newQuotaRedirectingFactory(direct)(route)
				return client.SetProxyURL(route), nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			_, err = NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), factory).QueryUsage(ctx, a.ID)
			require.Error(t, err)
			require.Zero(t, directHits.Load(), "proxy errors must never retry through the host's direct network")
			if failure != "refused" {
				require.EqualValues(t, 1, proxyHits.Load())
			}
			require.Empty(t, repo.writes, "failed proxy operations cannot persist quota observations")
		})
	}
}

func TestBalanceAndGrokQuotaRejectMissingConfiguredProxy(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi} {
		t.Run(platform, func(t *testing.T) {
			a := newDeepSeekBalanceProbeAccount()
			a.Platform = platform
			a.ProxyID = new(int64)
			*a.ProxyID = 7
			repo := &cnBalanceProbeRepo{account: a}
			upstream := &auxiliaryQuotaUpstream{}
			_, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), a.ID)
			require.Error(t, err)
			require.Zero(t, upstream.calls)
		})
	}
	t.Run("grok", func(t *testing.T) {
		a := &Account{ID: 883002, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive,
			Credentials: map[string]any{"access_token": "fake-grok", "refresh_token": "fake-refresh", "expires_at": "2099-01-01T00:00:00Z"}}
		a.ProxyID = new(int64)
		*a.ProxyID = 7
		repo := &auxiliaryOwnerRepo{account: a}
		upstream := &auxiliaryQuotaUpstream{}
		_, err := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream, nil).ProbeUsage(context.Background(), a.ID)
		require.Error(t, err)
		require.Zero(t, upstream.calls)
	})
}

func TestPreparedProviderTransportRequiresConfiguredRouteWithoutPriorBinding(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, transport := range []string{"gateway", "account_test", "account_test_tls"} {
			t.Run(platform+"/"+transport, func(t *testing.T) {
				a := &Account{ID: 883003, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1,
					Credentials: map[string]any{"api_key": "fake-only"}}
				a.ProxyID = new(int64)
				*a.ProxyID = 7
				request, err := http.NewRequest(http.MethodGet, "https://provider.invalid/models", nil)
				require.NoError(t, err)
				upstream := &auxiliaryQuotaUpstream{}
				if transport == "gateway" {
					_, err = (&OpenAIGatewayService{httpUpstream: upstream}).doOpenAIUpstreamPrepared(request, "http://foreign-proxy.invalid:8080", a)
				} else {
					_, err = (&AccountTestService{httpUpstream: upstream}).doOpenAIAccountTestUpstreamPrepared(request, "http://foreign-proxy.invalid:8080", a, transport == "account_test_tls")
				}
				require.Error(t, err)
				require.Zero(t, upstream.calls, "missing bound proxy must fail before transport even without prior credential context")
			})
		}
	}
}

func TestCodexProbeThrottleTracksCredentialAndProxyGeneration(t *testing.T) {
	now := time.Now()
	old := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	repo := &auxiliaryOwnerRepo{account: old}
	svc := &AccountUsageService{accountRepo: repo, cache: &UsageCache{}}
	require.True(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), old, now))
	require.False(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), old, now.Add(time.Second)))
	changed := snapshotRequestCredentialAccount(old)
	changed.Credentials["access_token"] = "fake-new"
	require.True(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), changed, now.Add(time.Second)), "new credentials must not inherit the prior throttle")
	changed.Proxy.Port++
	require.True(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), changed, now.Add(time.Second)), "proxy edits must permit a fresh probe immediately")
	shadow := &Account{ID: 883005, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &old.ID, QuotaDimension: QuotaDimensionSpark}
	require.True(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), shadow, now))
	require.False(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), shadow, now.Add(time.Second)))
	repo.replace(changed)
	require.True(t, svc.shouldProbeOpenAICodexSnapshot(context.Background(), shadow, now.Add(time.Second)), "shadow throttle must follow the parent's new route")
}

func TestCodexProbeLateObservationDoesNotMergeOrPersistAfterProxyEdit(t *testing.T) {
	old := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	replacement := snapshotRequestCredentialAccount(old)
	replacement.Proxy.Port++
	repo := &auxiliaryOwnerRepo{account: replacement}
	header := http.Header{"X-Codex-Primary-Used-Percent": []string{"100"}, "X-Codex-Primary-Window-Minutes": []string{"300"}}
	updates, err := (&AccountUsageService{accountRepo: repo}).recordOpenAICodexProbeResponse(context.Background(), old, &http.Response{StatusCode: http.StatusTooManyRequests, Header: header}, time.Now())
	require.NoError(t, err)
	require.Empty(t, updates, "a stale response cannot merge into the account card")
	require.Empty(t, repo.writes, "a stale response cannot update the current account row")
}

func TestWHAMAutoResetOwnerChangeFailsBeforeNetwork(t *testing.T) {
	old := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	changed := auxiliaryCodexAccount("fake-new", auxiliaryReplacementUA, 8)
	repo := &auxiliaryOwnerRepo{account: changed}
	factories := 0
	svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, nil, nil), func(string) (*req.Client, error) { factories++; return nil, fmt.Errorf("unexpected network") })
	ctx := withCodexObservationOwner(context.Background(), old)
	_, err := svc.QueryUsage(ctx, old.ID)
	require.Error(t, err)
	_, err = svc.ResetCreditTargeted(ctx, old.ID, "old-credit", "fixed-redeem")
	require.Error(t, err)
	require.Zero(t, factories, "an old auto-reset evaluation must never use a replacement grant")
}

type auxiliaryReferralClient struct {
	calls            []OpenAIReferralCall
	afterEligibility func()
}

func (c *auxiliaryReferralClient) QueryEligibility(_ context.Context, call OpenAIReferralCall) (*OpenAIReferralEligibility, error) {
	c.calls = append(c.calls, call)
	if c.afterEligibility != nil {
		c.afterEligibility()
	}
	remaining := 2
	return &OpenAIReferralEligibility{ShouldShow: true, RemainingSendCapacity: &remaining}, nil
}
func (c *auxiliaryReferralClient) SendInvite(_ context.Context, call OpenAIReferralCall, _ string) error {
	c.calls = append(c.calls, call)
	return nil
}

func TestReferralEligibilityAndSendRetainOneOwnerAndProxy(t *testing.T) {
	old := auxiliaryCodexAccount("fake-old", auxiliaryOwnerUA, 7)
	next := auxiliaryCodexAccount("fake-new", auxiliaryReplacementUA, 8)
	next.Credentials["account_type"] = "workspace"
	repo := &auxiliaryOwnerRepo{account: old}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(old): "fake-old"}}
	client := &auxiliaryReferralClient{afterEligibility: func() { repo.replace(next) }}
	svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), func(string) (*req.Client, error) { return nil, fmt.Errorf("referral adapter owns transport") })
	svc.referralClient = client
	result, err := svc.SendReferralInvite(context.Background(), old.ID, OpenAIReferralSendRequest{Email: "test@example.test", ProgramID: openAIReferralConsumer, Confirmed: true})
	require.NoError(t, err)
	require.True(t, result.Sent)
	require.Len(t, client.calls, 2)
	for _, call := range client.calls {
		require.Equal(t, old.Proxy.URL(), call.ProxyURL)
		require.Equal(t, openAIReferralConsumer, call.ProgramID)
		headers := http.Header{}
		for key, value := range call.Headers {
			headers.Set(key, value)
		}
		require.Equal(t, "Bearer fake-old", headers.Get("Authorization"))
		require.Equal(t, "owner-fake-old", headers.Get("ChatGPT-Account-Id"))
		require.Equal(t, auxiliaryOwnerUA, headers.Get("User-Agent"))
	}
	// The old row's eligibility cannot publish onto a newly authorized account.
	oldEligibility := &OpenAIReferralEligibility{credentialOwner: old}
	require.Error(t, svc.CacheReferralSnapshot(context.Background(), old.ID, oldEligibility))
	require.Empty(t, repo.writes)
}

func TestCodexUsageProbeUsesActualRefreshedCredentialOwner(t *testing.T) {
	old := auxiliaryCodexAccount("fake-probe-old", auxiliaryOwnerUA, 71)
	next := auxiliaryCodexAccount("fake-probe-new", auxiliaryReplacementUA, 71)
	next.Proxy = snapshotRequestCredentialAccount(old).Proxy
	repo := &auxiliaryOwnerRepo{account: next}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(old): "fake-probe-new"}}
	client, err := pool.GetClient(pool.Options{ProxyURL: old.Proxy.URL(), Timeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second})
	require.NoError(t, err)
	prior := client.Transport
	t.Cleanup(func() { client.Transport = prior })
	var sent http.Header
	client.Transport = auxiliaryRoundTripper(func(request *http.Request) (*http.Response, error) {
		sent = request.Header.Clone()
		header := http.Header{}
		header.Set("x-codex-primary-used-percent", "12")
		header.Set("x-codex-primary-window-minutes", "300")
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})
	svc := &AccountUsageService{accountRepo: repo, openAIQuotaService: &OpenAIQuotaService{tokenProvider: NewOpenAITokenProvider(repo, cache, nil)}}
	updates, err := svc.probeOpenAICodexSnapshot(context.Background(), old)
	require.NoError(t, err)
	require.Equal(t, "Bearer fake-probe-new", sent.Get("Authorization"))
	require.Equal(t, "owner-fake-probe-new", sent.Get("ChatGPT-Account-Id"))
	require.Equal(t, auxiliaryReplacementUA, sent.Get("User-Agent"))
	require.Equal(t, 12.0, updates["codex_5h_used_percent"])
	require.Len(t, repo.writes, 1)
}

func TestModelsPublicRegistryAndUnsupportedMiniMaxOAuthBoundaries(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		t.Run(platform+"-unbound-missing-proxy", func(t *testing.T) {
			account := &Account{ID: 883010, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fake-only"}}
			account.ProxyID = new(int64)
			*account.ProxyID = 7
			upstream := &auxiliaryQuotaUpstream{}
			request, err := http.NewRequest(http.MethodGet, "https://provider.invalid/v1/models", nil)
			require.NoError(t, err)
			_, err = (&AccountTestService{httpUpstream: upstream}).doUpstreamModelsRequestPrepared(request, account)
			require.Error(t, err)
			require.Zero(t, upstream.calls)
		})
	}
	t.Run("public-registry-has-no-provider-credentials", func(t *testing.T) {
		account := auxiliaryCodexAccount("fake-never-send", auxiliaryOwnerUA, 7)
		account.Proxy = nil
		upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
			require.Equal(t, "", proxy)
			require.Empty(t, request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("x-api-key"))
			require.Equal(t, modelsDevRegistryURL, request.URL.String())
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}}
		request, err := http.NewRequest(http.MethodGet, modelsDevRegistryURL, nil)
		require.NoError(t, err)
		response, err := (&AccountTestService{httpUpstream: upstream}).doUpstreamModelsRequest(request, account)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	})
	t.Run("MiniMax-oauth-live-model-sync-unsupported", func(t *testing.T) {
		upstream := &auxiliaryQuotaUpstream{}
		account := &Account{ID: 883011, Platform: PlatformMiniMax, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"oauth_provider": PlatformMiniMax, "oauth_region": "cn", "access_token": "fake-model-token", "refresh_token": "fake-refresh", "expires_at": "2099-01-01T00:00:00Z"}}
		_, err := (&AccountTestService{httpUpstream: upstream}).FetchUpstreamSupportedModels(context.Background(), account)
		require.Error(t, err)
		require.Zero(t, upstream.calls)
	})
}
