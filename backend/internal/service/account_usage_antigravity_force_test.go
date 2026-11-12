//go:build unit

package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
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
