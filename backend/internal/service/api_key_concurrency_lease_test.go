//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type renewableAPIKeyStatsCache struct {
	*renewableConcurrencyCacheForTest
	statsMu        sync.Mutex
	owners         map[string]time.Time
	statsRefreshes int
	statsTracks    int
	statsReleases  int
	loseStats      bool
	statsError     error
}

func (c *renewableAPIKeyStatsCache) TrackAPIKeySlot(_ context.Context, _ int64, requestID string) error {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	c.statsTracks++
	c.owners[requestID] = time.Now().Add(c.ttl)
	return nil
}

func (c *renewableAPIKeyStatsCache) RefreshAPIKeySlot(_ context.Context, _ int64, requestID string) (bool, error) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	c.statsRefreshes++
	if c.statsError != nil {
		return false, c.statsError
	}
	if c.loseStats || !c.owners[requestID].After(time.Now()) {
		delete(c.owners, requestID)
		return false, nil
	}
	c.owners[requestID] = time.Now().Add(c.ttl)
	return true, nil
}

func (c *renewableAPIKeyStatsCache) ReleaseAPIKeySlot(_ context.Context, _ int64, requestID string) error {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	c.statsReleases++
	delete(c.owners, requestID)
	return nil
}

func (c *renewableAPIKeyStatsCache) GetAPIKeyConcurrencyBatch(_ context.Context, apiKeyIDs []int64) (map[int64]int, error) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	for requestID, expires := range c.owners {
		if !expires.After(time.Now()) {
			delete(c.owners, requestID)
		}
	}
	counts := make(map[int64]int, len(apiKeyIDs))
	for _, id := range apiKeyIDs {
		counts[id] = len(c.owners)
	}
	return counts, nil
}

func TestAPIKeyConcurrencyStatsFollowLongRunningHTTPWithoutControllingAdmission(t *testing.T) {
	for _, failure := range []string{"healthy", "missing-owner", "stats-error"} {
		t.Run(failure, func(t *testing.T) {
			cache := &renewableAPIKeyStatsCache{renewableConcurrencyCacheForTest: &renewableConcurrencyCacheForTest{
				ttl: 90 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}, owners: make(map[string]time.Time)}
			svc := NewConcurrencyService(cache)
			parent, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			ctx, cancel := WithConcurrencyLeaseContext(parent)
			defer cancel(nil)
			lease, err := svc.AcquireUserSlot(ctx, 42, 3)
			require.NoError(t, err)
			defer lease.ReleaseFunc()
			releaseStats := svc.TrackAPIKeySlot(ctx, 88)
			defer releaseStats()
			detached, finishDetached := detachUpstreamContext(ctx)
			defer finishDetached()
			started, finish := make(chan struct{}), make(chan struct{})
			var finishOnce sync.Once
			finishProvider := func() { finishOnce.Do(func() { close(finish) }) }
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-finish:
					_, _ = io.WriteString(w, "completed")
				case <-r.Context().Done():
				}
			}))
			defer provider.Close()
			defer finishProvider()
			req, err := http.NewRequestWithContext(detached, http.MethodGet, provider.URL, nil)
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				response, callErr := provider.Client().Do(req)
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				result <- callErr
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("provider request did not start")
			}
			cache.statsMu.Lock()
			cache.loseStats = failure == "missing-owner"
			if failure == "stats-error" {
				cache.statsError = errors.New("statistics unavailable")
			}
			cache.statsMu.Unlock()
			disconnect()
			// The provider is still executing after the original statistics TTL,
			// including when the downstream client has already disconnected.
			require.Eventually(t, func() bool {
				cache.mu.Lock()
				defer cache.mu.Unlock()
				return cache.refreshes >= 4
			}, time.Second, 5*time.Millisecond)
			require.NoError(t, detached.Err(), "statistics loss must not revoke admitted capacity")
			select {
			case err := <-result:
				t.Fatalf("statistics failure stopped the real provider request: %v", err)
			default:
			}
			counts, err := svc.GetAPIKeyConcurrencyBatch(context.Background(), []int64{88})
			require.NoError(t, err)
			want := 0
			if failure == "healthy" {
				want = 1
			}
			require.Equal(t, want, counts[88])
			finishProvider()
			select {
			case err := <-result:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("provider request did not finish")
			}
			releaseStats()
			releaseStats()
			counts, err = svc.GetAPIKeyConcurrencyBatch(context.Background(), []int64{88})
			require.NoError(t, err)
			require.Zero(t, counts[88])
			cache.statsMu.Lock()
			require.Equal(t, 1, cache.statsTracks, "missing statistics must not be recreated with a new owner")
			require.Equal(t, 1, cache.statsReleases, "release must be idempotent")
			require.Greater(t, cache.statsRefreshes, 0)
			cache.statsMu.Unlock()
		})
	}
}

var _ APIKeyConcurrencyLeaseCache = (*renewableAPIKeyStatsCache)(nil)
