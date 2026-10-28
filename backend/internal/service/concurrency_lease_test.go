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

type renewableConcurrencyCacheForTest struct {
	stubConcurrencyCacheForTest
	mu                sync.Mutex
	ttl               time.Duration
	refreshes         int
	releaseUser       int
	releaseAccount    int
	lose              bool
	refreshErr        error
	limits            []int
	userAcquireOwners []string
	userReleaseOwners []string
	userReleaseIDs    []int64
}

func (c *renewableConcurrencyCacheForTest) ConcurrencySlotTTL() time.Duration { return c.ttl }
func (c *renewableConcurrencyCacheForTest) RefreshUserSlot(context.Context, int64, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshes++
	return !c.lose, c.refreshErr
}
func (c *renewableConcurrencyCacheForTest) RefreshAccountSlot(ctx context.Context, id int64, request string) (bool, error) {
	return c.RefreshUserSlot(ctx, id, request)
}
func (c *renewableConcurrencyCacheForTest) AcquireUserSlot(_ context.Context, _ int64, limit int, owner string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limits = append(c.limits, limit)
	c.userAcquireOwners = append(c.userAcquireOwners, owner)
	return c.acquireResult, c.acquireErr
}
func (c *renewableConcurrencyCacheForTest) ReleaseUserSlot(_ context.Context, userID int64, owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseUser++
	c.userReleaseOwners = append(c.userReleaseOwners, owner)
	c.userReleaseIDs = append(c.userReleaseIDs, userID)
	return nil
}
func (c *renewableConcurrencyCacheForTest) ReleaseAccountSlot(context.Context, int64, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseAccount++
	return nil
}

type currentConcurrencyUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *currentConcurrencyUserRepo) GetByID(context.Context, int64) (*User, error) {
	return r.user, r.err
}

func TestUserConcurrencyLeaseRenewsAndReleaseIsIdempotent(t *testing.T) {
	// An executing parent/sub-agent request retains its one global slot even
	// when its lifetime exceeds the original TTL. Releasing twice counts once.
	cache := &renewableConcurrencyCacheForTest{ttl: 90 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	svc := NewConcurrencyService(cache)
	ctx, cancel := WithConcurrencyLeaseContext(context.Background())
	defer cancel(nil)
	acquired, err := svc.AcquireUserSlot(ctx, 42, 3)
	require.NoError(t, err)
	require.True(t, acquired.Acquired)
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshes >= 4 }, time.Second, 10*time.Millisecond)
	require.NoError(t, acquired.Context.Err())
	acquired.ReleaseFunc()
	acquired.ReleaseFunc()
	cache.mu.Lock()
	released, refreshes := cache.releaseUser, cache.refreshes
	cache.mu.Unlock()
	require.Equal(t, 1, released)
	require.GreaterOrEqual(t, refreshes, 4)
	require.NoError(t, ctx.Err(), "normal slot release must not cancel a failover's shared execution context")
}

func TestUserAndAccountConcurrencyLeaseLossCancelsExecution(t *testing.T) {
	for _, failure := range []string{"missing-user", "redis-error", "missing-account"} {
		t.Run(failure, func(t *testing.T) {
			cache := &renewableConcurrencyCacheForTest{ttl: 60 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
			svc := NewConcurrencyService(cache)
			ctx, cancel := WithConcurrencyLeaseContext(context.Background())
			defer cancel(nil)
			var acquired *AcquireResult
			var err error
			if failure == "missing-account" {
				acquired, err = svc.AcquireAccountSlot(ctx, 99, 2)
			} else {
				acquired, err = svc.AcquireUserSlot(ctx, 42, 3)
			}
			require.NoError(t, err)
			require.True(t, acquired.Acquired)
			defer acquired.ReleaseFunc()
			cache.mu.Lock()
			cache.lose = true
			if failure == "redis-error" {
				cache.refreshErr = errors.New("redis unavailable")
			}
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("lost capacity ownership must stop execution before another request can reuse it")
			}
			require.ErrorIs(t, context.Cause(ctx), ErrConcurrencyLeaseLost)
		})
	}
}

func TestUserConcurrencyAdmissionUsesCurrentServerLimit(t *testing.T) {
	cache := &renewableConcurrencyCacheForTest{ttl: time.Minute, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	repo := &currentConcurrencyUserRepo{user: &User{ID: 42, Concurrency: 3}}
	svc := NewConcurrencyService(cache)
	svc.SetUserRepository(repo)
	first, err := svc.AcquireUserSlot(context.Background(), 42, 99)
	require.NoError(t, err)
	first.ReleaseFunc()
	repo.user.Concurrency = 1
	next, err := svc.AcquireUserSlot(context.Background(), 42, 99)
	require.NoError(t, err)
	next.ReleaseFunc()
	cache.mu.Lock()
	limits := append([]int(nil), cache.limits...)
	cache.mu.Unlock()
	require.Equal(t, []int{3, 1}, limits, "old auth or websocket values cannot enlarge the user's current cap")
	repo.err = errors.New("user limit storage unavailable")
	_, err = svc.AcquireUserSlot(context.Background(), 42, 0)
	require.Error(t, err, "a stale unlimited credential cannot bypass a failed current-limit read")
	repo.err = nil
	repo.user.Concurrency = 0
	unlimited, err := svc.AcquireUserSlot(context.Background(), 42, 99)
	require.NoError(t, err)
	unlimited.ReleaseFunc()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, []int{3, 1, 0}, cache.limits, "unlimited execution remains counted so a later lowered cap sees it")
}

func TestDetachedWorkerUsesIndependentConcurrencyLeaseContext(t *testing.T) {
	// Durable work inherits metadata but owns capacity after its ingress ends.
	type metadataKey struct{}
	ingress, stopIngress := WithConcurrencyLeaseContext(context.WithValue(context.Background(), metadataKey{}, "accepted-price"))
	worker, stopWorker := WithConcurrencyLeaseContext(WithoutConcurrencyLeaseContext(ingress))
	defer stopWorker(nil)
	stopIngress(errors.New("request completed"))
	require.NoError(t, worker.Err())
	require.Equal(t, "accepted-price", worker.Value(metadataKey{}))
	stopWorker(ErrConcurrencyLeaseLost)
	require.ErrorIs(t, context.Cause(worker), ErrConcurrencyLeaseLost)
	require.EqualError(t, context.Cause(ingress), "request completed")
}

func TestWorkerUserLeaseDelegationIsScopedAndCannotOutliveItsOwner(t *testing.T) {
	cache := &renewableConcurrencyCacheForTest{ttl: time.Minute, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	svc := NewConcurrencyService(cache)
	worker, cancel := WithConcurrencyLeaseContext(context.Background())
	defer cancel(nil)
	outer, err := svc.AcquireWorkerUserSlot(worker, 42, 3)
	require.NoError(t, err)
	inner, err := svc.AcquireUserSlot(outer.Context, 42, 3)
	require.NoError(t, err)
	inner.ReleaseFunc()
	cache.mu.Lock()
	require.Equal(t, []int{3}, cache.limits, "a worker's synchronous gateway call consumes one user slot total")
	require.Zero(t, cache.releaseUser, "returning from the gateway must retain worker capacity for storage")
	cache.mu.Unlock()

	// Independent agent requests and separately detached workers cannot inherit
	// that admission. Another user cannot borrow the worker owner's slot either.
	for _, call := range []struct {
		ctx    context.Context
		userID int64
	}{
		{context.Background(), 42},
		{WithoutConcurrencyLeaseContext(outer.Context), 42},
		{outer.Context, 43},
	} {
		lease, err := svc.AcquireUserSlot(call.ctx, call.userID, 3)
		require.NoError(t, err)
		lease.ReleaseFunc()
	}
	cache.mu.Lock()
	require.Equal(t, []int{3, 3, 3, 3}, cache.limits)
	require.Equal(t, 3, cache.releaseUser)
	cache.mu.Unlock()
	outer.ReleaseFunc()
	outer.ReleaseFunc()
	_, err = svc.AcquireUserSlot(outer.Context, 42, 3)
	require.ErrorIs(t, err, ErrConcurrencyLeaseLost, "a saved worker context cannot bypass admission after completion")
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, 4, cache.releaseUser)
	require.Equal(t, []int{3, 3, 3, 3}, cache.limits)
}

func TestAccountLeaseReleaseKeepsUserLeaseExecuting(t *testing.T) {
	// A failing account can be replaced while the same user request stays charged
	// exactly one global slot; releasing the account must keep the domain alive.
	cache := &renewableConcurrencyCacheForTest{ttl: time.Minute, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	svc := NewConcurrencyService(cache)
	ctx, cancel := WithConcurrencyLeaseContext(context.Background())
	defer cancel(nil)
	user, err := svc.AcquireUserSlot(ctx, 42, 1)
	require.NoError(t, err)
	defer user.ReleaseFunc()
	first, err := svc.AcquireAccountSlot(ctx, 10, 1)
	require.NoError(t, err)
	first.ReleaseFunc()
	require.NoError(t, ctx.Err())
	second, err := svc.AcquireAccountSlot(ctx, 11, 1)
	require.NoError(t, err)
	defer second.ReleaseFunc()
	require.NoError(t, user.Context.Err())
	require.NoError(t, second.Context.Err())
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, []int{1}, cache.limits)
}

func TestCapacityLeaseSurvivesClientDisconnectUntilUpstreamDrainCompletes(t *testing.T) {
	// Disconnecting a billable request cannot free its global capacity while the
	// existing provider call drains usage. Losing Redis ownership still stops it.
	type priceMetadata struct{}
	parent, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	ctx, cancel := WithConcurrencyLeaseContext(parent)
	defer cancel(nil)
	cache := &renewableConcurrencyCacheForTest{ttl: 60 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	lease, err := NewConcurrencyService(cache).AcquireUserSlot(ctx, 42, 1)
	require.NoError(t, err)
	defer lease.ReleaseFunc()
	ctx = context.WithValue(ctx, priceMetadata{}, "accepted-price")
	detached, releaseDetached := detachStreamUpstreamContext(ctx, true)
	defer releaseDetached()
	disconnect()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NoError(t, detached.Err())
	require.Equal(t, "accepted-price", detached.Value(priceMetadata{}))
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshes >= 4 }, time.Second, 5*time.Millisecond)
	require.NoError(t, detached.Err(), "still-executing upstream drain retains its renewable slot")
	cache.mu.Lock()
	cache.lose = true
	cache.mu.Unlock()
	select {
	case <-detached.Done():
	case <-time.After(time.Second):
		t.Fatal("lost capacity did not terminate detached upstream work")
	}
	require.ErrorIs(t, context.Cause(detached), ErrConcurrencyLeaseLost)
	lease.ReleaseFunc()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Equal(t, 1, cache.releaseUser)
}

func TestCapacityLeaseCancellationReachesActualDetachedHTTPCall(t *testing.T) {
	// All common detach builders must preserve lease cancellation on the actual
	// HTTP request while retaining client-disconnect usage-draining behavior.
	for _, mode := range []string{"upstream", "stream", "non-stream"} {
		t.Run(mode, func(t *testing.T) {
			parent, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			ctx, cancel := WithConcurrencyLeaseContext(parent)
			defer cancel(nil)
			cache := &renewableConcurrencyCacheForTest{ttl: 60 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
			lease, err := NewConcurrencyService(cache).AcquireUserSlot(ctx, 42, 1)
			require.NoError(t, err)
			defer lease.ReleaseFunc()
			var detached context.Context
			var finish context.CancelFunc
			if mode == "upstream" {
				detached, finish = detachUpstreamContext(ctx)
			} else {
				detached, finish = detachStreamUpstreamContext(ctx, mode == "stream")
			}
			defer finish()
			started := make(chan struct{})
			stopped := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) }))
			defer upstream.Close()
			defer cancel(nil)
			req, err := http.NewRequestWithContext(detached, http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				response, callErr := upstream.Client().Do(req)
				if response != nil {
					_ = response.Body.Close()
				}
				done <- callErr
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("HTTP request did not start")
			}
			disconnect()
			require.NoError(t, detached.Err())
			require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshes >= 4 }, time.Second, 5*time.Millisecond)
			cache.mu.Lock()
			cache.lose = true
			cache.mu.Unlock()
			select {
			case callErr := <-done:
				require.Error(t, callErr)
			case <-time.After(time.Second):
				t.Fatal("detached real HTTP request survived lost capacity")
			}
			require.ErrorIs(t, context.Cause(detached), ErrConcurrencyLeaseLost)
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("HTTP peer did not observe capacity cancellation")
			}
		})
	}
}

func TestGeminiCachedContentCapacityLossStopsActualMutationRequest(t *testing.T) {
	// Cache mutations deliberately survive a client disconnect to preserve local
	// ownership/billing. They still must stop if global execution capacity is lost.
	parent, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	ctx, cancel := WithConcurrencyLeaseContext(parent)
	defer cancel(nil)
	cache := &renewableConcurrencyCacheForTest{ttl: 60 * time.Millisecond, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	lease, err := NewConcurrencyService(cache).AcquireUserSlot(ctx, 42, 1)
	require.NoError(t, err)
	defer lease.ReleaseFunc()
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	defer cancel(nil)
	service := NewGeminiCachedContentService(newGeminiCachedContentRepoStub(), &GeminiMessagesCompatService{httpUpstream: &firstByteHTTPTestUpstream{client: upstream.Client()}}, nil)
	account := &Account{ID: 8, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-only"}}
	done := make(chan error, 1)
	go func() {
		_, callErr := service.doUpstream(ctx, account, http.MethodPost, upstream.URL, []byte(`{"model":"gemini-test"}`), time.Second)
		done <- callErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cache mutation HTTP did not start")
	}
	disconnect()
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshes >= 4 }, time.Second, 5*time.Millisecond)
	cache.mu.Lock()
	cache.lose = true
	cache.mu.Unlock()
	select {
	case callErr := <-done:
		require.Error(t, callErr)
	case <-time.After(time.Second):
		t.Fatal("cache mutation survived lost capacity")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cache provider did not observe cancellation")
	}
}

func TestInvalidCapacityTTLRejectsAndReleasesNewAdmission(t *testing.T) {
	for _, scope := range []string{"user", "account"} {
		t.Run(scope, func(t *testing.T) {
			cache := &renewableConcurrencyCacheForTest{ttl: 0, stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
			service := NewConcurrencyService(cache)
			var acquired *AcquireResult
			var err error
			if scope == "user" {
				acquired, err = service.AcquireUserSlot(context.Background(), 42, 1)
			} else {
				acquired, err = service.AcquireAccountSlot(context.Background(), 8, 1)
			}
			require.ErrorIs(t, err, ErrConcurrencyLeaseLost)
			require.Nil(t, acquired)
			cache.mu.Lock()
			defer cache.mu.Unlock()
			if scope == "user" {
				require.Equal(t, 1, cache.releaseUser)
			} else {
				require.Equal(t, 1, cache.releaseAccount)
			}
		})
	}
}
