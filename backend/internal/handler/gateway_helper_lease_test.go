//go:build unit || !integration

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type renewableHelperConcurrencyCache struct {
	helperConcurrencyCacheStub
	ttl          time.Duration
	leaseLost    bool
	refreshCalls int
}

func (c *renewableHelperConcurrencyCache) ConcurrencySlotTTL() time.Duration { return c.ttl }
func (c *renewableHelperConcurrencyCache) RefreshUserSlot(context.Context, int64, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshCalls++
	return !c.leaseLost, nil
}
func (c *renewableHelperConcurrencyCache) RefreshAccountSlot(ctx context.Context, id int64, request string) (bool, error) {
	return c.RefreshUserSlot(ctx, id, request)
}
func (c *renewableHelperConcurrencyCache) RefreshAPIKeySlot(context.Context, int64, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.apiKeyTrackCalls > c.apiKeyReleaseCalls, nil
}

func TestConcurrencyHelper_AcquiredExecutionOutlivesQueueAndStopsOnLeaseLoss(t *testing.T) {
	// Request lifetime, not the queue helper's timeout scope, owns regular slots.
	// Every Gin acquisition must install that same cancellable context before the
	// handler forwards upstream. Check an actual HTTP call is stopped on lease loss.
	for _, path := range []string{"user-immediate", "worker-queued", "account-immediate", "account-queued"} {
		t.Run(path, func(t *testing.T) {
			sequence := []bool{true}
			if path == "worker-queued" || path == "account-queued" {
				sequence = []bool{false, true}
			}
			cache := &renewableHelperConcurrencyCache{ttl: 90 * time.Millisecond, helperConcurrencyCacheStub: helperConcurrencyCacheStub{userSeq: append([]bool(nil), sequence...), accountSeq: append([]bool(nil), sequence...), waitAllowed: true}}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			c, _ := newHelperTestContext(http.MethodPost, "/v1/chat/completions")
			streaming := false
			var release func()
			var err error
			switch path {
			case "user-immediate":
				release, err = helper.AcquireUserSlot(c, 42, 2)
			case "worker-queued":
				release, err = helper.acquireWorkerUserSlotWithWait(c, 42, 2)
			default:
				release, err = helper.AcquireAccountSlotForWaitPlan(c, 99, &service.AccountWaitPlan{MaxConcurrency: 2, MaxWaiting: 2, Timeout: time.Second}, false, &streaming)
			}
			require.NoError(t, err)
			require.NotNil(t, release)
			defer release()
			executionCtx := c.Request.Context()
			_, cancel := service.WithConcurrencyLeaseContext(executionCtx)
			defer cancel(nil)
			require.NoError(t, executionCtx.Err(), "returning from the queue helper must leave execution alive")
			require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshCalls >= 4 }, time.Second, 5*time.Millisecond)
			require.NoError(t, executionCtx.Err(), "the same owner must renew beyond the initial TTL")

			upstreamStarted := make(chan struct{})
			upstreamStopped := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(upstreamStarted)
				<-r.Context().Done()
				close(upstreamStopped)
			}))
			defer upstream.Close()
			// Keep cancellation before server teardown even when an assertion fails.
			defer cancel(nil)
			request, err := http.NewRequestWithContext(executionCtx, http.MethodPost, upstream.URL, nil)
			require.NoError(t, err)
			clientDone := make(chan error, 1)
			go func() {
				response, callErr := upstream.Client().Do(request)
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				clientDone <- callErr
			}()
			select {
			case <-upstreamStarted:
			case <-time.After(time.Second):
				t.Fatal("upstream call did not start")
			}
			cache.mu.Lock()
			cache.leaseLost = true
			cache.mu.Unlock()
			select {
			case <-executionCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("lease loss did not cancel the installed execution context")
			}
			require.ErrorIs(t, context.Cause(executionCtx), service.ErrConcurrencyLeaseLost)
			select {
			case callErr := <-clientDone:
				require.Error(t, callErr)
			case <-time.After(time.Second):
				t.Fatal("actual upstream HTTP request survived lost capacity")
			}
			select {
			case <-upstreamStopped:
			case <-time.After(time.Second):
				t.Fatal("upstream server did not observe cancellation")
			}
			release()
			release()
			cache.mu.Lock()
			defer cache.mu.Unlock()
			if path == "user-immediate" || path == "worker-queued" {
				require.Equal(t, 1, cache.userReleaseCalls)
			} else {
				require.Equal(t, 1, cache.accountReleaseCalls)
			}
		})
	}
}

func TestConcurrencyHelper_APIKeyAndUserReleaseAreIdempotent(t *testing.T) {
	cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	ctx, cancel := service.WithConcurrencyLeaseContext(context.Background())
	defer cancel(nil)
	release, acquired, err := helper.TryAcquireUserSlotForAPIKey(ctx, 42, 2, 17)
	require.NoError(t, err)
	require.True(t, acquired)
	release()
	release()
	require.Equal(t, 1, cache.userReleaseCalls)
	require.Equal(t, 1, cache.apiKeyReleaseCalls)
	require.NoError(t, ctx.Err(), "normal release does not cancel the shared failover context")
}

func TestConcurrencyHelper_ClientDisconnectKeepsDrainingCapacityUntilForwardReturns(t *testing.T) {
	// The handler's automatic release must follow capacity cancellation, not the
	// client context; services intentionally drain accepted upstream usage.
	cache := &renewableHelperConcurrencyCache{ttl: 60 * time.Millisecond, helperConcurrencyCacheStub: helperConcurrencyCacheStub{userSeq: []bool{true}}}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/images/generations")
	parent, disconnect := context.WithCancel(c.Request.Context())
	defer disconnect()
	c.Request = c.Request.WithContext(parent)
	acquired, err := helper.AcquireUserSlot(c, 42, 1)
	require.NoError(t, err)
	release := wrapReleaseOnDone(c.Request.Context(), acquired)
	defer release()
	disconnect()
	require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshCalls >= 4 }, time.Second, 5*time.Millisecond)
	cache.mu.Lock()
	released := cache.userReleaseCalls
	cache.mu.Unlock()
	require.Zero(t, released, "client disconnect must not give another request the draining provider's slot")
	release()
	release()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, 1, cache.userReleaseCalls)
}

func TestConcurrencyHelper_WorkerWaitsForSubmittingRequestThenKeepsExecutionAlive(t *testing.T) {
	// With C=1 the accepted worker may start before the submitting HTTP handler
	// returns. It must queue, then own capacity beyond that queue's lifetime.
	cache := &renewableHelperConcurrencyCache{ttl: 60 * time.Millisecond, helperConcurrencyCacheStub: helperConcurrencyCacheStub{userSeq: []bool{false, true}, waitAllowed: true}}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/images/generations")
	release, err := helper.acquireWorkerUserSlotWithWait(c, 42, 1)
	require.NoError(t, err)
	defer release()
	require.NoError(t, c.Request.Context().Err())
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshCalls >= 4 }, time.Second, 5*time.Millisecond)
	inner, acquired, err := helper.TryAcquireUserSlot(c.Request.Context(), 42, 1)
	require.NoError(t, err)
	require.True(t, acquired)
	inner()
	cache.mu.Lock()
	require.Equal(t, 2, cache.userAcquireCalls, "one refused attempt and one acquired worker; gateway borrows that owner")
	require.Equal(t, 1, cache.waitIncrementCalls)
	require.Equal(t, 20, cache.waitMaxWait)
	require.Equal(t, 1, cache.waitDecrementCalls)
	require.Zero(t, cache.userReleaseCalls)
	cache.mu.Unlock()
	release()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, 1, cache.userReleaseCalls)
}
