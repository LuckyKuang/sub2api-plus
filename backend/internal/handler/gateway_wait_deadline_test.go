//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/stretchr/testify/require"
)

// A successful Redis reply can arrive after queue admission has expired. The
// queue must return that exact owner instead of sending a new upstream request.
type lateQueueAcquisitionCache struct {
	service.ConcurrencyCache
	attempts atomic.Int32
	releases atomic.Int32
	started  chan struct{}
	resume   chan struct{}
}

func (c *lateQueueAcquisitionCache) waitForLateReply() bool {
	if c.attempts.Add(1) == 1 {
		return false
	}
	close(c.started)
	<-c.resume
	return true
}

func (c *lateQueueAcquisitionCache) AcquireAccountSlot(ctx context.Context, accountID int64, limit int, owner string) (bool, error) {
	if !c.waitForLateReply() {
		return false, nil
	}
	// Model a committed acquisition whose success reaches the gateway late.
	return c.ConcurrencyCache.AcquireAccountSlot(context.WithoutCancel(ctx), accountID, limit, owner)
}

func (c *lateQueueAcquisitionCache) AcquireUserSlot(ctx context.Context, userID int64, limit int, owner string) (bool, error) {
	if !c.waitForLateReply() {
		return false, nil
	}
	return c.ConcurrencyCache.AcquireUserSlot(context.WithoutCancel(ctx), userID, limit, owner)
}

func (c *lateQueueAcquisitionCache) ReleaseAccountSlot(ctx context.Context, accountID int64, owner string) error {
	c.releases.Add(1)
	return c.ConcurrencyCache.ReleaseAccountSlot(ctx, accountID, owner)
}

func (c *lateQueueAcquisitionCache) ReleaseUserSlot(ctx context.Context, userID int64, owner string) error {
	c.releases.Add(1)
	return c.ConcurrencyCache.ReleaseUserSlot(ctx, userID, owner)
}

func TestGatewayWaitDeadlineRejectsLateAcquisitionWithoutUpstream(t *testing.T) {
	for _, scope := range []string{"account_queue_deadline", "worker_request_deadline"} {
		t.Run(scope, func(t *testing.T) {
			raw, _ := concurrencycache.New(t)
			// Another accepted execution must survive cleanup of this late
			// acquisition. A zero-only assertion would miss deleting every owner.
			const independentOwner = "accepted-independent-execution"
			acquired, err := raw.AcquireAccountSlot(context.Background(), 85101, 2, independentOwner)
			require.NoError(t, err)
			require.True(t, acquired)
			acquired, err = raw.AcquireUserSlot(context.Background(), 85102, 2, independentOwner)
			require.NoError(t, err)
			require.True(t, acquired)
			cache := &lateQueueAcquisitionCache{ConcurrencyCache: raw, started: make(chan struct{}), resume: make(chan struct{})}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			c, _ := newHelperTestContext(http.MethodPost, "/v1/images/generations")
			deadline := time.Now().Add(time.Second)
			if scope == "worker_request_deadline" {
				ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			var upstreamCalls, upstreamAttempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			type result struct {
				err error
			}
			done := make(chan result, 1)
			go func() {
				var release func()
				var err error
				if scope == "account_queue_deadline" {
					release, err = helper.AcquireAccountSlotForWaitPlan(c, 85101, &service.AccountWaitPlan{MaxConcurrency: 2, MaxWaiting: 2, Timeout: time.Until(deadline)}, false, new(bool))
				} else {
					release, err = helper.acquireWorkerUserSlotWithWait(c, 85102, 2)
				}
				if err == nil {
					upstreamAttempts.Add(1)
					request, requestErr := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, nil)
					if requestErr == nil {
						response, _ := upstream.Client().Do(request)
						if response != nil {
							_ = response.Body.Close()
						}
					}
				}
				if release != nil {
					release()
					release()
				}
				done <- result{err: err}
			}()
			select {
			case <-cache.started:
			case <-time.After(2 * time.Second):
				t.Fatal("queue did not attempt acquisition")
			}
			// The independently declared acceptance deadline expires before the
			// transport publishes its successful result; no implementation value
			// is used to compute the expected rejection.
			timer := time.NewTimer(time.Until(deadline.Add(40 * time.Millisecond)))
			<-timer.C
			close(cache.resume)
			var got result
			select {
			case got = <-done:
			case <-time.After(time.Second):
				t.Fatal("late acquisition cleanup did not finish")
			}
			require.Error(t, got.err, "expired queue admission cannot authorize execution")
			if scope == "account_queue_deadline" {
				var queueErr *ConcurrencyError
				require.ErrorAs(t, got.err, &queueErr)
				require.True(t, queueErr.IsTimeout)
			} else {
				require.ErrorIs(t, got.err, context.DeadlineExceeded)
			}
			require.Zero(t, upstreamAttempts.Load(), "a late owner cannot start forwarding")
			require.Zero(t, upstreamCalls.Load())
			require.Equal(t, int32(1), cache.releases.Load(), "return exactly the newly acquired owner")
			accounts, err := raw.GetAccountConcurrency(context.Background(), 85101)
			require.NoError(t, err)
			require.Equal(t, 1, accounts, "retain the independent account execution")
			waiting, err := raw.GetAccountWaitingCount(context.Background(), 85101)
			require.NoError(t, err)
			require.Zero(t, waiting)
			users, err := raw.GetUsersLoadBatch(context.Background(), []service.UserWithConcurrency{{ID: 85102, MaxConcurrency: 2}})
			require.NoError(t, err)
			require.Equal(t, 1, users[85102].CurrentConcurrency, "retain the independent user execution")
			require.Zero(t, users[85102].WaitingCount)
			renewer := raw.(service.ConcurrencyLeaseCache)
			retained, err := renewer.RefreshAccountSlot(context.Background(), 85101, independentOwner)
			require.NoError(t, err)
			require.True(t, retained, "cleanup must preserve the exact independent account owner")
			retained, err = renewer.RefreshUserSlot(context.Background(), 85102, independentOwner)
			require.NoError(t, err)
			require.True(t, retained, "cleanup must preserve the exact independent user owner")
			require.NoError(t, raw.ReleaseAccountSlot(context.Background(), 85101, independentOwner))
			require.NoError(t, raw.ReleaseUserSlot(context.Background(), 85102, independentOwner))
			accounts, err = raw.GetAccountConcurrency(context.Background(), 85101)
			require.NoError(t, err)
			require.Zero(t, accounts)
			usersAfterRelease, err := raw.GetUserConcurrency(context.Background(), 85102)
			require.NoError(t, err)
			require.Zero(t, usersAfterRelease)
		})
	}
}

func TestGatewayWaitDeadlineKeepsAcceptedExecutionRenewable(t *testing.T) {
	for _, scope := range []string{"account", "worker"} {
		t.Run(scope, func(t *testing.T) {
			cache := &renewableHelperConcurrencyCache{
				ttl: 60 * time.Millisecond,
				helperConcurrencyCacheStub: helperConcurrencyCacheStub{
					userSeq: []bool{false, true}, accountSeq: []bool{false, true}, waitAllowed: true,
				},
			}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			c, _ := newHelperTestContext(http.MethodPost, "/v1/images/generations")
			var release func()
			var err error
			if scope == "account" {
				release, err = helper.AcquireAccountSlotForWaitPlan(c, 85101, &service.AccountWaitPlan{MaxConcurrency: 1, MaxWaiting: 2, Timeout: time.Second}, false, new(bool))
			} else {
				release, err = helper.acquireWorkerUserSlotWithWait(c, 85102, 1)
			}
			require.NoError(t, err)
			require.NotNil(t, release)
			defer release()
			_, cancel := service.WithConcurrencyLeaseContext(c.Request.Context())
			defer cancel(nil)
			// The queue helper has already returned and cancelled its wait scope.
			// Sixty renewals also exceed the account's original one-second deadline.
			require.Eventually(t, func() bool {
				cache.mu.Lock()
				defer cache.mu.Unlock()
				return cache.refreshCalls >= 60
			}, 3*time.Second, 5*time.Millisecond)
			require.NoError(t, c.Request.Context().Err(), "accepted execution must outlive queue cancellation")
			release()
			release()
			cache.mu.Lock()
			defer cache.mu.Unlock()
			if scope == "account" {
				require.Equal(t, 1, cache.accountReleaseCalls)
				require.Equal(t, 1, cache.accountWaitDecrementCalls)
			} else {
				require.Equal(t, 1, cache.userReleaseCalls)
				require.Equal(t, 1, cache.waitDecrementCalls)
			}
		})
	}
}
