//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The storage stub deliberately records exactly the requested amount. The
// service must serialize concurrent estimates and never submit a smaller hold.
type inflightIncreaseLifecycleCache struct {
	*memInflightCache
	updateCalls int
	deny        bool
	updateErr   error
	balanceErr  error
	entered     chan struct{}
	resume      chan struct{}
}

func (c *inflightIncreaseLifecycleCache) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	c.mu.Lock()
	err := c.balanceErr
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return c.memInflightCache.GetUserBalance(ctx, userID)
}

func (c *inflightIncreaseLifecycleCache) UpdateInflightBalance(ctx context.Context, _ int64, owner string, amount, _ float64, _ time.Duration) (bool, float64, error) {
	c.mu.Lock()
	c.updateCalls++
	entered, resume := c.entered, c.resume
	c.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
		select {
		case <-resume:
		case <-ctx.Done():
			return false, 0, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.updateErr != nil {
		return false, 0, c.updateErr
	}
	if _, alive := c.res[owner]; !alive || c.deny {
		return false, 0, nil
	}
	c.res[owner] = amount
	return true, 0, nil
}

func TestIncreaseInflightReservationMonotonicAndCapped(t *testing.T) {
	cache := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
	svc := newInflightSvc(t, cache, 60)
	svc.cfg.Billing.InflightReservation.MaxReservationUSD = 5
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 91}, nil, nil, 1)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	defer reservation.Release()

	require.NoError(t, svc.IncreaseInflightReservation(context.Background(), reservation, 4))
	require.Equal(t, float64(4), reservation.Amount())
	require.NoError(t, svc.IncreaseInflightReservation(context.Background(), reservation, 2))
	require.Equal(t, float64(4), reservation.Amount(), "a cheaper failover cannot reopen spendable capacity")
	require.NoError(t, svc.IncreaseInflightReservation(context.Background(), reservation, 9))
	require.Equal(t, float64(5), reservation.Amount(), "the configured reservation cap applies to the total, not the increment")
	cache.mu.Lock()
	held, updateCalls := cache.res[reservation.requestID], cache.updateCalls
	cache.mu.Unlock()
	require.Equal(t, float64(5), held)
	require.Equal(t, 3, updateCalls)
}

func TestIncreaseInflightReservationConcurrentAmountAndUpdates(t *testing.T) {
	cache := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
	svc := newInflightSvc(t, cache, 60)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 92}, nil, nil, 1)
	require.NoError(t, err)
	defer reservation.Release()
	start, stop := make(chan struct{}), make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		<-start
		previous := float64(1)
		for {
			current := reservation.Amount()
			if current < previous {
				t.Errorf("concurrent readers observed a reservation decrease: %v -> %v", previous, current)
				return
			}
			previous = current
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	var writers sync.WaitGroup
	for amount := 2; amount <= 17; amount++ {
		writers.Add(1)
		go func(amount float64) {
			defer writers.Done()
			<-start
			if err := svc.IncreaseInflightReservation(context.Background(), reservation, amount); err != nil {
				t.Errorf("increase failed: %v", err)
			}
		}(float64(amount))
	}
	close(start)
	writers.Wait()
	close(stop)
	<-readerDone
	require.Equal(t, float64(17), reservation.Amount())
	cache.mu.Lock()
	held := cache.res[reservation.requestID]
	cache.mu.Unlock()
	require.Equal(t, float64(17), held, "out-of-order smaller estimates cannot overwrite the largest accepted hold")
}

func TestIncreaseInflightReservationReleaseSerializesWithUpdate(t *testing.T) {
	cache := &inflightIncreaseLifecycleCache{
		memInflightCache: newMemInflightCache(100),
		entered:          make(chan struct{}, 1), resume: make(chan struct{}),
	}
	svc := newInflightSvc(t, cache, 60)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 93}, nil, nil, 1)
	require.NoError(t, err)
	defer reservation.Release()
	defer func() {
		select {
		case <-cache.resume:
		default:
			close(cache.resume)
		}
	}()
	updated := make(chan error, 1)
	go func() { updated <- svc.IncreaseInflightReservation(context.Background(), reservation, 4) }()
	select {
	case <-cache.entered:
	case <-time.After(time.Second):
		t.Fatal("increase did not reach storage")
	}
	releaseStarted, released := make(chan struct{}), make(chan struct{})
	go func() {
		close(releaseStarted)
		reservation.Release()
		close(released)
	}()
	<-releaseStarted
	select {
	case <-released:
		t.Fatal("release removed an owner while its atomic increase was still pending")
	case <-time.After(20 * time.Millisecond):
	}
	close(cache.resume)
	require.NoError(t, <-updated)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("release did not finish after the update")
	}
	require.Zero(t, cache.count())
	require.Equal(t, int32(1), cache.releaseCt.Load())
	require.ErrorIs(t, svc.IncreaseInflightReservation(context.Background(), reservation, 5), ErrInsufficientBalance)
	reservation.Acquire()()
	reservation.HandlerDone()
	require.Zero(t, cache.count(), "late increases and billing references must not resurrect a released owner")
	cache.mu.Lock()
	updateCalls := cache.updateCalls
	cache.mu.Unlock()
	require.Equal(t, 1, updateCalls)
}

func TestIncreaseInflightReservationSurvivesHandlerUntilBillingReferenceReturns(t *testing.T) {
	cache := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
	svc := newInflightSvc(t, cache, 60)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 94}, nil, nil, 1)
	require.NoError(t, err)
	defer reservation.Release()
	billingDone := reservation.Acquire()
	reservation.HandlerDone()
	require.NoError(t, svc.IncreaseInflightReservation(context.Background(), reservation, 5))
	require.Equal(t, float64(5), reservation.Amount())
	require.Equal(t, 1, cache.count())
	billingDone()
	billingDone()
	require.Zero(t, cache.count())
	require.Equal(t, int32(1), cache.releaseCt.Load())
	require.ErrorIs(t, svc.IncreaseInflightReservation(context.Background(), reservation, 6), ErrInsufficientBalance)
}

func TestIncreaseInflightReservationExplicitRejectionKeepsOldHold(t *testing.T) {
	for _, reason := range []string{"insufficient_balance", "missing_owner_same_estimate"} {
		t.Run(reason, func(t *testing.T) {
			cache := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
			svc := newInflightSvc(t, cache, 60)
			reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 95}, nil, nil, 1)
			require.NoError(t, err)
			defer reservation.Release()
			estimate := float64(4)
			cache.mu.Lock()
			if reason == "insufficient_balance" {
				cache.deny = true
			} else {
				delete(cache.res, reservation.requestID)
				estimate = 1
			}
			cache.mu.Unlock()
			require.ErrorIs(t, svc.IncreaseInflightReservation(context.Background(), reservation, estimate), ErrInsufficientBalance)
			require.Equal(t, float64(1), reservation.Amount())
			require.Zero(t, cache.releaseCt.Load(), "a rejected update never releases the old owner")
			cache.mu.Lock()
			updateCalls := cache.updateCalls
			held, present := cache.res[reservation.requestID]
			cache.mu.Unlock()
			require.Equal(t, 1, updateCalls, "unchanged estimates must still detect missing storage ownership")
			if reason == "insufficient_balance" {
				require.True(t, present)
				require.Equal(t, float64(1), held)
			} else {
				require.False(t, present)
			}
		})
	}
}

type inflightIncreaseUnavailableUserRepo struct{ UserRepository }

func (inflightIncreaseUnavailableUserRepo) GetByID(context.Context, int64) (*User, error) {
	return nil, errors.New("balance database unavailable")
}

func TestIncreaseInflightReservationDependencyFailuresKeepExistingHold(t *testing.T) {
	for _, failure := range []string{"redis_update", "balance_read", "updater_unsupported"} {
		t.Run(failure, func(t *testing.T) {
			storage := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
			var cache BillingCache = storage
			if failure == "updater_unsupported" {
				cache = storage.memInflightCache
			}
			svc := newInflightSvc(t, cache, 60)
			svc.userRepo = inflightIncreaseUnavailableUserRepo{}
			reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 96}, nil, nil, 1)
			require.NoError(t, err)
			defer reservation.Release()
			storage.mu.Lock()
			if failure == "redis_update" {
				storage.updateErr = errors.New("Redis command failed")
			}
			if failure == "balance_read" {
				storage.balanceErr = errors.New("balance cache unavailable")
			}
			storage.mu.Unlock()
			require.NoError(t, svc.IncreaseInflightReservation(context.Background(), reservation, 4), "dependency failure retains the existing fail-open contract")
			require.Equal(t, float64(1), reservation.Amount())
			storage.mu.Lock()
			held := storage.res[reservation.requestID]
			storage.mu.Unlock()
			require.Equal(t, float64(1), held)
			require.Zero(t, storage.releaseCt.Load())
		})
	}
}

func TestIncreaseInflightReservationRejectsInvalidEstimateWithoutMutation(t *testing.T) {
	cache := &inflightIncreaseLifecycleCache{memInflightCache: newMemInflightCache(100)}
	svc := newInflightSvc(t, cache, 60)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 97}, nil, nil, 1)
	require.NoError(t, err)
	defer reservation.Release()
	for _, estimate := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.ErrorIs(t, svc.IncreaseInflightReservation(context.Background(), reservation, estimate), ErrBillingRateUnavailable)
	}
	require.Equal(t, float64(1), reservation.Amount())
	cache.mu.Lock()
	updateCalls := cache.updateCalls
	cache.mu.Unlock()
	require.Zero(t, updateCalls)
	require.NoError(t, svc.IncreaseInflightReservation(context.Background(), nil, 10))
}
