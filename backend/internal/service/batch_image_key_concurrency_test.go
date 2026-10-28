//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchImageWorkerTracksAPIKeyThroughActualProviderExecution(t *testing.T) {
	for _, outcome := range []string{"success", "provider-error", "capacity-loss"} {
		t.Run(outcome, func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			var once sync.Once
			finishProvider := func() { once.Do(func() { close(finish) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-finish:
					if outcome == "provider-error" {
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					w.WriteHeader(http.StatusOK)
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			defer finishProvider()
			cache := &renewableAPIKeyStatsCache{renewableConcurrencyCacheForTest: &renewableConcurrencyCacheForTest{
				ttl: 90 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}, owners: make(map[string]time.Time)}
			concurrency := NewConcurrencyService(cache)
			repo := newFakeBatchImageRepository()
			accountID, keyID := int64(20), int64(88)
			repo.jobs["key-owned"] = &BatchImageJob{BatchID: "key-owned", UserID: 42, APIKeyID: &keyID, Provider: "fake", Status: BatchImageJobStatusSubmitted, AccountID: &accountID, ProviderJobName: batchImageStringPtr("provider/job")}
			provider := &fakeProcessorProvider{getFunc: func(ctx context.Context, _ *BatchImageJob, _ *Account) (*BatchProviderStatus, error) {
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
				if err != nil {
					return nil, err
				}
				response, err := server.Client().Do(request)
				if err != nil {
					return nil, err
				}
				defer response.Body.Close()
				_, _ = io.Copy(io.Discard, response.Body)
				if response.StatusCode != http.StatusOK {
					return nil, ErrBatchImageProviderSubmitFailed
				}
				return &BatchProviderStatus{InternalState: BatchProviderStateRunning, RawState: "RUNNING"}, nil
			}}
			processor := newTestBatchImageProcessor(repo, provider)
			processor.Concurrency = concurrency
			done := make(chan error, 1)
			go func() { _, err := processor.Process(context.Background(), "key-owned"); done <- err }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("worker never contacted the provider")
			}
			require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshes >= 4 }, time.Second, 5*time.Millisecond)
			counts, err := concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{keyID})
			require.NoError(t, err)
			require.Equal(t, 1, counts[keyID], "a batch worker is one executing request for its submitting key")
			if outcome == "capacity-loss" {
				cache.mu.Lock()
				cache.lose = true
				cache.mu.Unlock()
			} else {
				finishProvider()
			}
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("worker did not stop after the provider outcome")
			}
			counts, err = concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{keyID})
			require.NoError(t, err)
			require.Zero(t, counts[keyID])
			cache.mu.Lock()
			require.Equal(t, 1, cache.releaseUser)
			cache.mu.Unlock()
			cache.statsMu.Lock()
			require.Equal(t, 1, cache.statsTracks)
			require.Equal(t, 1, cache.statsReleases)
			cache.statsMu.Unlock()
		})
	}
}

func TestWorkerAPIKeyStatsDelegationCannotBeBorrowedByIndependentExecutions(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{acquireResult: true}
	svc := NewConcurrencyService(cache)
	worker, err := svc.AcquireWorkerUserSlot(context.Background(), 42, 3)
	require.NoError(t, err)
	defer worker.ReleaseFunc()
	releaseOuter := svc.TrackAPIKeySlot(worker.Context, 88)
	defer releaseOuter()
	releaseNested := svc.TrackAPIKeySlot(worker.Context, 88)
	releaseNested()
	require.Equal(t, []int64{88}, cache.trackedAPIKeyIDs)
	require.Empty(t, cache.releasedAPIKeyIDs, "nested gateway does not own the worker's statistics release")
	for _, release := range []func(){
		svc.TrackAPIKeySlot(WithoutConcurrencyLeaseContext(worker.Context), 88),
		svc.TrackAPIKeySlot(worker.Context, 89),
		NewConcurrencyService(cache).TrackAPIKeySlot(worker.Context, 88),
	} {
		release()
	}
	require.Equal(t, []int64{88, 88, 89, 88}, cache.trackedAPIKeyIDs)
	releaseOuter()
	require.Len(t, cache.releasedAPIKeyIDs, 4)
}
