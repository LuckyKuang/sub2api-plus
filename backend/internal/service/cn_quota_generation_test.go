//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func bindCNQuotaTestSnapshot(account *Account) {
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[cnExtraKey(account.Platform, cnQuotaGenerationSuffix)] = cnQuotaGeneration(account)
}

type cnQuotaGenerationRepo struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]*Account
	writes   int
}

func (r *cnQuotaGenerationRepo) put(account *Account) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.ID] = account
}

func (r *cnQuotaGenerationRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotCNQuotaAccount(r.accounts[id])
}

func (r *cnQuotaGenerationRepo) UpdateCNProviderQuotaIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.accounts[expected.ID]
	if current == nil {
		return false, nil
	}
	oldJSON, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	currentJSON, err := json.Marshal(current.Credentials)
	if err != nil {
		return false, err
	}
	if current.Platform != expected.Platform || current.Type != expected.Type || current.Status != expected.Status ||
		string(oldJSON) != string(currentJSON) || !cnOAuthSameProxyID(current.ProxyID, expected.ProxyID) || current.proxyURLOrEmpty() != expected.proxyURLOrEmpty() {
		return false, nil
	}
	if current.Extra == nil {
		current.Extra = map[string]any{}
	}
	for key, value := range updates {
		current.Extra[key] = value
	}
	r.writes++
	return true, nil
}

// Use the real HTTP client/body and the full query/persistence path while
// retaining the official destination in every request presented by the service.
func cnQuotaGenerationHTTPUpstream(t *testing.T, handler http.Handler) *cnRecoveryUpstream {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	return &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.kimi.com" || req.URL.Path != "/coding/v1/usages" {
			return nil, fmt.Errorf("unexpected quota destination")
		}
		forwarded := req.Clone(req.Context())
		target := *req.URL
		target.Scheme, target.Host = endpoint.Scheme, endpoint.Host
		forwarded.URL = &target
		return server.Client().Do(forwarded)
	}}
}

func TestCNQuotaProbeReauthorizationSeparatesFlightsAndRejectsLateExhaustion(t *testing.T) {
	old := codingAccount(PlatformKimi)
	old.ID, old.Status = 81, StatusActive
	old.Credentials["api_key"] = "old-grant"
	old.Extra = map[string]any{"kimi_weekly_used_percent": 100.0, "kimi_weekly_reset_at": time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)}
	bindCNQuotaTestSnapshot(old)
	repo := &cnQuotaGenerationRepo{accounts: map[int64]*Account{old.ID: old}}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	upstream := cnQuotaGenerationHTTPUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ratio := "0.1"
		if req.Header.Get("Authorization") == "Bearer old-grant" {
			close(started)
			<-release
			ratio = "1"
		}
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":%s,"reset_time":%q}}}`, ratio, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	}))
	svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com"))
	type outcome struct {
		result *CNProviderQuotaProbeResult
		err    error
	}
	oldDone := make(chan outcome, 1)
	go func() {
		result, err := svc.QueryUsageForAccount(context.Background(), old)
		oldDone <- outcome{result, err}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("old probe did not start")
	}
	current, err := snapshotCNQuotaAccount(old)
	require.NoError(t, err)
	current.Credentials["api_key"] = "new-grant"
	repo.put(current)
	require.False(t, EvaluateAccountSchedulingThreshold(current, map[string]int{PlatformKimi: 80}, time.Now()).ShouldPause, "reauthorization cannot inherit the old grant's exhausted windows")
	require.Nil(t, cnProviderQuotaSnapshotReset(current, time.Now()))
	newDone := make(chan outcome, 1)
	go func() {
		result, err := svc.QueryUsageForAccount(context.Background(), current)
		newDone <- outcome{result, err}
	}()
	select {
	case fresh := <-newDone:
		require.NoError(t, fresh.err)
		require.True(t, fresh.result.Success)
		require.True(t, fresh.result.Persisted)
		require.Equal(t, 10.0, fresh.result.Tiers[0].UsedPercent)
	case <-time.After(3 * time.Second):
		t.Fatal("new grant must not join the old grant's blocked flight")
	}
	releaseOnce.Do(func() { close(release) })
	late := <-oldDone
	require.NoError(t, late.err)
	require.False(t, late.result.Success, "a stale quota owner cannot publish a successful current observation")
	require.False(t, late.result.Persisted)
	require.Equal(t, 100.0, late.result.Tiers[0].UsedPercent)
	saved, err := repo.GetByID(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, "new-grant", saved.GetCredential("api_key"))
	require.Equal(t, 10.0, saved.Extra["kimi_5h_used_percent"])
	require.Nil(t, saved.Extra["kimi_weekly_used_percent"])
	require.Nil(t, saved.Extra["kimi_weekly_reset_at"])
	require.False(t, EvaluateAccountSchedulingThreshold(saved, map[string]int{PlatformKimi: 80}, time.Now()).ShouldPause)
	require.Equal(t, 1, repo.writes)
}

func TestCNQuotaIndependentAccountsKeepExhaustionWithCredentialOwner(t *testing.T) {
	a, b := codingAccount(PlatformKimi), codingAccount(PlatformKimi)
	a.ID, b.ID = 91, 92
	a.Credentials["api_key"], b.Credentials["api_key"] = "exhausted-A", "healthy-B"
	repo := &cnQuotaGenerationRepo{accounts: map[int64]*Account{a.ID: a, b.ID: b}}
	upstream := cnQuotaGenerationHTTPUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ratio := "0.1"
		if req.Header.Get("Authorization") == "Bearer exhausted-A" {
			ratio = "1"
		}
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":%s,"reset_time":%q}}}`, ratio, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	}))
	svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com"))
	for _, owner := range []*Account{a, b} {
		result, err := svc.QueryUsageForAccount(context.Background(), owner)
		require.NoError(t, err)
		require.True(t, result.Persisted)
	}
	first, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	second, err := repo.GetByID(context.Background(), b.ID)
	require.NoError(t, err)
	require.True(t, EvaluateAccountSchedulingThreshold(first, map[string]int{PlatformKimi: 80}, time.Now()).ShouldPause)
	require.False(t, EvaluateAccountSchedulingThreshold(second, map[string]int{PlatformKimi: 80}, time.Now()).ShouldPause)
	second.Extra = first.Extra
	require.False(t, EvaluateAccountSchedulingThreshold(second, map[string]int{PlatformKimi: 80}, time.Now()).ShouldPause, "A's snapshot cannot block B even if copied")
}

func TestCNQuotaGenerationRejectsChangedOwnerAndLegacySnapshot(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax} {
		for _, change := range []string{"legacy", "id", "platform", "type", "credentials", "nested credentials", "proxy", "proxy endpoint"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				a := codingAccount(platform)
				a.ProxyID = new(int64)
				*a.ProxyID = 5
				a.Proxy = &Proxy{ID: 5, Protocol: "http", Host: "old", Port: 8111, Status: StatusActive}
				a.Credentials["model_mapping"] = map[string]any{"a": "b"}
				a.Extra = map[string]any{cnExtraKey(platform, "5h_used_percent"): 100.0, cnExtraKey(platform, "5h_reset_at"): time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
				bindCNQuotaTestSnapshot(a)
				require.True(t, EvaluateAccountSchedulingThreshold(a, map[string]int{platform: 80}, time.Now()).ShouldPause)
				switch change {
				case "legacy":
					delete(a.Extra, cnExtraKey(platform, cnQuotaGenerationSuffix))
				case "id":
					a.ID++
				case "platform":
					a.Platform = PlatformDeepseek
				case "type":
					a.Type = AccountTypeOAuth
				case "credentials":
					a.Credentials["api_key"] = "replacement"
				case "nested credentials":
					a.Credentials["model_mapping"].(map[string]any)["a"] = "changed"
				case "proxy":
					*a.ProxyID = 6
				case "proxy endpoint":
					a.Proxy.Host = "new"
				}
				require.False(t, EvaluateAccountSchedulingThreshold(a, map[string]int{platform: 80}, time.Now()).ShouldPause)
				require.Nil(t, cnProviderQuotaSnapshotReset(a, time.Now()))
			})
		}
	}
}

func TestCNQuotaMissingCASDoesNotWriteUnconditionally(t *testing.T) {
	a := codingAccount(PlatformKimi)
	repo := &fakeCNProbeAccountRepo{account: a}
	upstream := &cnRecoveryUpstream{send: func(*http.Request) (*http.Response, error) {
		return cnRecoveryResponse(200, `{"usages":{"limit_5h":{"used_ratio":0.1}}}`), nil
	}}
	result, err := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com")).QueryUsageForAccount(context.Background(), a)
	require.NoError(t, err)
	require.False(t, result.Success, "an unavailable CAS cannot produce a successful monitor observation")
	require.False(t, result.Persisted)
	require.Empty(t, a.Extra)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), a.GetCredential("api_key")))
}

func TestCNQuotaMonitorSeparatesConcurrentGrantsAndCachesTheHealthyOwner(t *testing.T) {
	old := codingAccount(PlatformKimi)
	old.Credentials["api_key"] = "old-monitor-grant"
	repo := &cnQuotaGenerationRepo{accounts: map[int64]*Account{old.ID: old}}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var newQueries atomic.Int32
	upstream := cnQuotaGenerationHTTPUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ratio := "0.1"
		if req.Header.Get("Authorization") == "Bearer old-monitor-grant" {
			close(started)
			<-release
			ratio = "1"
		} else {
			newQueries.Add(1)
		}
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":%s,"reset_time":%q}}}`, ratio, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	}))
	svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com"))
	fetcher := &ChannelMonitorQuotaFetcher{cnQuota: svc, accounts: repo}
	oldDone := make(chan struct{})
	go func() { fetcher.Fetch(context.Background(), old.ID); close(oldDone) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("old monitor query did not start")
	}
	current, err := snapshotCNQuotaAccount(old)
	require.NoError(t, err)
	current.Credentials["api_key"] = "new-monitor-grant"
	repo.put(current)
	newDone := make(chan float64, 1)
	go func() {
		result := fetcher.Fetch(context.Background(), current.ID)
		if !result.Success || len(result.Tiers) != 1 {
			newDone <- -1
			return
		}
		newDone <- result.Tiers[0].UsedPercent
	}()
	select {
	case used := <-newDone:
		require.Equal(t, 10.0, used)
	case <-time.After(3 * time.Second):
		t.Fatal("new monitor grant joined the previous grant's flight")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-oldDone:
	case <-time.After(3 * time.Second):
		t.Fatal("old monitor flight did not finish")
	}
	healthy := fetcher.Fetch(context.Background(), current.ID)
	require.True(t, healthy.Success)
	require.Equal(t, 10.0, healthy.Tiers[0].UsedPercent)
	require.Equal(t, int32(1), newQueries.Load(), "late old completion cannot evict the new generation's healthy cache")
}

func (r *cnQuotaGenerationRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	return r.UpdateCNProviderQuotaIfUnchanged(ctx, expected, updates)
}
