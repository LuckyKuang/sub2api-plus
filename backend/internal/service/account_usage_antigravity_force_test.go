//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/adminaccess"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

// A manual refresh (force=true) must not be answered from the antigravity usage cache: an
// account upgraded or reset upstream otherwise keeps showing old figures for the cache TTL.
func TestCachedAntigravityUsage_ForceSkipsFreshCache(t *testing.T) {
	s := &AccountUsageService{cache: NewUsageCache()}
	resetsAt := time.Now().Add(2 * time.Hour)
	owner := &Account{ID: 6, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
	stored := &UsageInfo{FiveHour: &UsageProgress{ResetsAt: &resetsAt}}
	s.cache.antigravityCache.Store(int64(6), &antigravityUsageCache{generation: credentialStateGeneration(owner), usageInfo: stored, timestamp: time.Now()})

	usage, ok := s.cachedAntigravityUsage(owner, false)
	if !ok || usage == stored || usage.FiveHour == stored.FiveHour {
		t.Fatalf("fresh cache should answer a normal query, got ok=%v", ok)
	}
	if usage.FiveHour.RemainingSeconds <= 0 {
		t.Fatalf("remaining seconds should be recalculated, got %d", usage.FiveHour.RemainingSeconds)
	}
	if usage, ok := s.cachedAntigravityUsage(owner, true); ok || usage != nil {
		t.Fatalf("a forced query must skip the cache")
	}
}

func TestCachedAntigravityUsage_ExpiredOrMissing(t *testing.T) {
	s := &AccountUsageService{cache: NewUsageCache()}
	if _, ok := s.cachedAntigravityUsage(&Account{ID: 7}, false); ok {
		t.Fatalf("missing entry must miss")
	}
	owner := &Account{ID: 7}
	s.cache.antigravityCache.Store(int64(7), &antigravityUsageCache{generation: credentialStateGeneration(owner), usageInfo: &UsageInfo{}, timestamp: time.Now().Add(-time.Hour)})
	if _, ok := s.cachedAntigravityUsage(owner, false); ok {
		t.Fatalf("expired entry must miss")
	}
}

// The owning outbound contract forbids using a replaced grant's quota or a
// missing configured proxy. Exercise the public usage boundary with real HTTP.
func TestAntigravityUsageRefreshRetainsOwnerAndStrictProxy(t *testing.T) {
	oldURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	t.Cleanup(func() {
		antigravity.BaseURLs = oldURLs
		antigravity.DefaultURLAvailability = oldAvailability
	})
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var oldCalls, newCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "fetchAvailableModels") {
			remaining := "0.9"
			if r.Header.Get("Authorization") == "Bearer old-owner" {
				if oldCalls.Add(1) == 1 {
					close(started)
					<-release
				}
				remaining = "0.2"
			} else {
				if r.Header.Get("Authorization") != "Bearer new-owner" {
					t.Errorf("unexpected owning authorization on quota request")
				}
				newCalls.Add(1)
			}
			_, _ = fmt.Fprintf(w, `{"models":{"gemini-4-argon":{"quotaInfo":{"remainingFraction":%s}}}}`, remaining)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	}()
	antigravity.BaseURLs = []string{server.URL}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)
	svc := &AccountUsageService{cache: NewUsageCache(), antigravityQuotaFetcher: NewAntigravityQuotaFetcher(&config.Config{})}
	owner := &Account{ID: 73, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "old-owner", "project_id": "old-project"}}
	oldDone := make(chan error, 1)
	go func() {
		usage, err := svc.GetUsageForAccount(t.Context(), owner, true)
		if err == nil && usage.AntigravityQuota["gemini-4-argon"].Utilization != 80 {
			err = fmt.Errorf("old request lost its captured grant")
		}
		oldDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("old owner's request did not reach the wire")
	}
	// This mutation happens after the old request has reached the server. All
	// remaining calls in that operation must retain its old token and project.
	owner.Credentials["access_token"] = "new-owner"
	owner.Credentials["project_id"] = "new-project"
	usage, err := svc.GetUsageForAccount(t.Context(), owner, true)
	require.NoError(t, err)
	require.InDelta(t, 10, usage.AntigravityQuota["gemini-4-argon"].Utilization, 1)
	require.EqualValues(t, 1, newCalls.Load(), "a different grant must not join the blocked old flight")
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-oldDone)
	usage, err = svc.GetUsageForAccount(t.Context(), owner)
	require.NoError(t, err)
	require.InDelta(t, 10, usage.AntigravityQuota["gemini-4-argon"].Utilization, 1, "late old quota must not appear for the new owner")
	before := newCalls.Load()
	_, err = svc.GetUsageForAccount(t.Context(), owner, true)
	require.NoError(t, err)
	require.Equal(t, before+1, newCalls.Load(), "force must reach upstream despite a fresh same-owner cache")
	broken := snapshotRequestCredentialAccount(owner)
	proxyID := int64(99)
	broken.ProxyID = &proxyID
	before = newCalls.Load()
	_, err = svc.GetUsageForAccount(t.Context(), broken, true)
	require.ErrorContains(t, err, "proxy")
	require.Equal(t, before, newCalls.Load(), "a missing bound proxy must never dial the direct endpoint")
}

// Recovery must commit against the same saved credential/proxy and error state.
// This fixture also detects the former unconditional ClearError outside a transaction.
type antigravityUsageRecoveryRepo struct {
	AccountRepository
	mu                                   sync.Mutex
	current                              *Account
	writes, unfencedWrites, transactions int
	commitErr                            error
}

func (r *antigravityUsageRecoveryRepo) ClearCredentialErrorIfUnchanged(_ context.Context, owner *Account) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transactions++
	if r.commitErr != nil {
		return false, r.commitErr
	}
	if credentialStateGeneration(r.current) != credentialStateGeneration(owner) || r.current.Status != owner.Status || r.current.ErrorMessage != owner.ErrorMessage {
		return false, nil
	}
	r.current.Status, r.current.ErrorMessage = StatusActive, ""
	r.writes++
	return true, nil
}
func (r *antigravityUsageRecoveryRepo) ClearError(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unfencedWrites++
	r.current.Status, r.current.ErrorMessage = StatusActive, ""
	return nil
}

func TestAntigravityUsageRecoveryRequiresFreshSuccessfulCurrentOwner(t *testing.T) {
	oldURLs, oldAvailability := append([]string(nil), antigravity.BaseURLs...), antigravity.DefaultURLAvailability
	t.Cleanup(func() { antigravity.BaseURLs, antigravity.DefaultURLAvailability = oldURLs, oldAvailability })
	for _, scenario := range []string{"current", "new_token", "new_proxy", "new_error", "already_active", "commit_failure", "degraded", "forbidden", "cached", "scoped", "nonrecoverable", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			owner := &Account{ID: 81, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
				Status: StatusError, ErrorMessage: "unauthenticated old grant", Credentials: map[string]any{"access_token": "old-owner", "project_id": "project"}}
			if scenario == "nonrecoverable" {
				owner.ErrorMessage = "manual quarantine"
			}
			repo := &antigravityUsageRecoveryRepo{current: snapshotRequestCredentialAccount(owner)}
			if scenario == "commit_failure" {
				repo.commitErr = fmt.Errorf("synthetic transaction failure")
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(req.URL.Path, "fetchAvailableModels") {
					calls.Add(1)
					repo.mu.Lock()
					switch scenario {
					case "new_token":
						repo.current.Credentials["access_token"] = "new-owner"
					case "new_proxy":
						id := int64(99)
						repo.current.ProxyID = &id
					case "new_error":
						repo.current.ErrorMessage = "unauthenticated new error"
					case "already_active":
						repo.current.Status, repo.current.ErrorMessage = StatusActive, ""
					}
					repo.mu.Unlock()
					if scenario == "degraded" {
						_, _ = w.Write([]byte(`{`))
						return
					}
					if scenario == "forbidden" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"error":"forbidden"}`))
						return
					}
					_, _ = w.Write([]byte(`{"models":{"gemini-4-argon":{"quotaInfo":{"remainingFraction":0.9}}}}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			antigravity.BaseURLs = []string{server.URL}
			antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)
			accountRepo := AccountRepository(repo)
			if scenario == "unsupported" {
				accountRepo = struct{ AccountRepository }{repo}
			}
			svc := &AccountUsageService{accountRepo: accountRepo, cache: NewUsageCache(), antigravityQuotaFetcher: NewAntigravityQuotaFetcher(&config.Config{})}
			if scenario == "cached" {
				svc.cache.antigravityCache.Store(owner.ID, &antigravityUsageCache{generation: credentialStateGeneration(owner), usageInfo: &UsageInfo{}, timestamp: time.Now()})
			}
			ctx := t.Context()
			if scenario == "scoped" {
				owner.GroupIDs = []int64{1}
				ctx = scopedStatisticsServiceContext("accounts.usage.read")
				adminaccess.FromContext(ctx).Policy.Rules[0].Platforms = []string{PlatformAntigravity}
			}
			_, err := svc.GetUsageForAccount(ctx, owner)
			require.NoError(t, err)
			require.Zero(t, repo.unfencedWrites, "quota recovery cannot write without a saved-owner transaction")
			if scenario == "current" {
				require.Equal(t, 1, repo.writes)
				require.Equal(t, StatusActive, repo.current.Status)
			} else {
				require.Zero(t, repo.writes, "a stale, cached or failed result cannot recover an account")
				if scenario != "already_active" {
					require.Equal(t, StatusError, repo.current.Status)
				}
			}
			if scenario == "cached" {
				require.Zero(t, calls.Load())
				require.Zero(t, repo.transactions)
			}
			if scenario == "forbidden" || scenario == "degraded" || scenario == "scoped" || scenario == "nonrecoverable" || scenario == "unsupported" {
				require.Zero(t, repo.transactions)
			}
			require.Equal(t, StatusError, owner.Status, "caller-owned account must not be mutated by usage recovery")
		})
	}
}
