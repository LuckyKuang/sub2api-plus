//go:build integration

package repository

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newInflightSelectedOwnerRealRedisEnv(t *testing.T) (*redis.Client, [2]*billingCache, [2]*service.BillingCacheService, *service.User) {
	t.Helper()
	rdb := testRedis(t)
	caches := [2]*billingCache{{rdb: rdb}, {rdb: rdb}}
	var peers [2]*service.BillingCacheService
	for i := range peers {
		cfg := &config.Config{}
		cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
		peers[i] = service.NewBillingCacheService(caches[i], nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(peers[i].Stop)
	}
	user := &service.User{ID: 731, Balance: 10}
	require.NoError(t, caches[0].SetUserBalance(context.Background(), user.ID, 10))
	return rdb, caches, peers, user
}

func inflightSelectedOwnerRealRedisAmounts(t *testing.T, rdb *redis.Client, userID int64) map[string]string {
	t.Helper()
	_, key := billingInflightKeys(userID)
	amounts, err := rdb.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	return amounts
}

func TestInflightSelectedOwnerRealRedisExactBalanceBoundaryAndRejectedHoldPreservation(t *testing.T) {
	rdb, _, peers, user := newInflightSelectedOwnerRealRedisEnv(t)
	ctx := context.Background()
	selected, err := peers[0].ReserveInflight(ctx, user, nil, nil, 3)
	require.NoError(t, err)
	require.NotNil(t, selected)
	defer selected.HandlerDone()
	other, err := peers[1].ReserveInflight(ctx, user, nil, nil, 2)
	require.NoError(t, err)
	require.NotNil(t, other)
	defer other.HandlerDone()

	// Each service represents an independent gateway instance. Repricing the
	// selected request to eight must exclude its previous three from the sum.
	require.NoError(t, peers[0].IncreaseInflightReservation(ctx, selected, 8))
	require.Equal(t, float64(8), selected.Amount())
	require.Equal(t, float64(2), other.Amount())
	before := inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID)
	require.Len(t, before, 2)
	amounts := make([]string, 0, 2)
	for _, amount := range before {
		amounts = append(amounts, amount)
	}
	require.ElementsMatch(t, []string{"8", "2"}, amounts)
	zkey, _ := billingInflightKeys(user.ID)
	ownersBefore, err := rdb.ZRange(ctx, zkey, 0, -1).Result()
	require.NoError(t, err)

	require.ErrorIs(t, peers[0].IncreaseInflightReservation(ctx, selected, 9), service.ErrInsufficientBalance)
	require.Equal(t, float64(8), selected.Amount())
	require.Equal(t, before, inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID), "a rejected increase must leave every old hold intact")
	third, err := peers[1].ReserveInflight(ctx, user, nil, nil, 1)
	require.ErrorIs(t, err, service.ErrInsufficientBalance, "the other instance must see the selected request's raised amount")
	require.Nil(t, third)
	ownersAfter, err := rdb.ZRange(ctx, zkey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, ownersBefore, ownersAfter, "neither rejection may replace or drop an existing owner")
	selected.HandlerDone()
	remaining := inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID)
	require.Len(t, remaining, 1)
	for _, amount := range remaining {
		require.Equal(t, "2", amount, "releasing one instance must preserve the other instance's hold")
	}
	other.HandlerDone()
	require.Empty(t, inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID))
}

func TestInflightSelectedOwnerRealRedisCompetingIncreasesAreAtomicAcrossInstances(t *testing.T) {
	rdb, _, peers, user := newInflightSelectedOwnerRealRedisEnv(t)
	ctx := context.Background()
	var reservations [2]*service.InflightReservation
	for i := range peers {
		reservation, err := peers[i].ReserveInflight(ctx, user, nil, nil, 1)
		require.NoError(t, err)
		require.NotNil(t, reservation)
		reservations[i] = reservation
		defer reservation.HandlerDone()
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := range peers {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			results <- peers[i].IncreaseInflightReservation(ctx, reservations[i], 6)
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.True(t, errors.Is(err, service.ErrInsufficientBalance), "unexpected rejection: %v", err)
			rejected++
		}
	}
	require.Equal(t, 1, accepted, "two six-dollar holds cannot both fit a ten-dollar balance")
	require.Equal(t, 1, rejected)
	stored := inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID)
	require.Len(t, stored, 2, "the rejected request keeps its initial one-dollar hold")
	total := 0.0
	for _, amount := range stored {
		value, err := strconv.ParseFloat(amount, 64)
		require.NoError(t, err)
		total += value
	}
	require.Equal(t, float64(7), total)
	require.LessOrEqual(t, total, float64(10))
	require.ElementsMatch(t, []float64{1, 6}, []float64{reservations[0].Amount(), reservations[1].Amount()})
	zkey, _ := billingInflightKeys(user.ID)
	count, err := rdb.ZCard(ctx, zkey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

func TestInflightSelectedOwnerRealRedisDeletedOrExpiredOwnerCannotReappear(t *testing.T) {
	for _, ownerState := range []string{"deleted", "expired"} {
		t.Run(ownerState, func(t *testing.T) {
			rdb, caches, peers, user := newInflightSelectedOwnerRealRedisEnv(t)
			ctx := context.Background()
			selected, err := peers[0].ReserveInflight(ctx, user, nil, nil, 1)
			require.NoError(t, err)
			require.NotNil(t, selected)
			defer selected.HandlerDone()
			selectedOwners := inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID)
			require.Len(t, selectedOwners, 1)
			var selectedOwner string
			for owner := range selectedOwners {
				selectedOwner = owner
			}
			other, err := peers[1].ReserveInflight(ctx, user, nil, nil, 2)
			require.NoError(t, err)
			require.NotNil(t, other)
			defer other.HandlerDone()
			zkey, hkey := billingInflightKeys(user.ID)
			if ownerState == "deleted" {
				require.NoError(t, caches[1].ReleaseInflightBalance(ctx, user.ID, selectedOwner))
			} else {
				// A past lease deadline exercises real Redis's lazy-expiry path
				// without waiting for the production one-minute reservation TTL.
				require.NoError(t, rdb.ZAdd(ctx, zkey, redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: selectedOwner}).Err())
			}
			for _, estimate := range []float64{1, 4} {
				require.ErrorIs(t, peers[0].IncreaseInflightReservation(ctx, selected, estimate), service.ErrInsufficientBalance, "both unchanged and raised estimates require a live owner")
			}
			require.Equal(t, float64(1), selected.Amount(), "rejection does not change the handle's last accepted amount")
			present, err := rdb.HExists(ctx, hkey, selectedOwner).Result()
			require.NoError(t, err)
			require.False(t, present, "a lost selected owner must never be recreated")
			require.ErrorIs(t, rdb.ZScore(ctx, zkey, selectedOwner).Err(), redis.Nil)
			remaining := inflightSelectedOwnerRealRedisAmounts(t, rdb, user.ID)
			require.Len(t, remaining, 1)
			for _, amount := range remaining {
				require.Equal(t, "2", amount, "the independent live owner's hold survives lost-owner cleanup")
			}
			require.NoError(t, peers[1].IncreaseInflightReservation(ctx, other, 3))
			require.Equal(t, float64(3), other.Amount())
			count, err := rdb.ZCard(ctx, zkey).Result()
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
