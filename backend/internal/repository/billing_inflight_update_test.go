//go:build unit || !integration

package repository

import (
	"context"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestUpdateInflightBalanceExcludesOnlyItsLiveOwner(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 411
	zkey, hkey := billingInflightKeys(userID)
	allowed, _, err := cache.ReserveInflightBalance(ctx, userID, "selected", 3, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, _, err = cache.ReserveInflightBalance(ctx, userID, "other", 2, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	// Exact boundary: selected needs eight, while another request holds two.
	// Its previous three must not count a second time.
	allowed, pending, err := cache.UpdateInflightBalance(ctx, userID, "selected", 8, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, float64(2), pending)
	require.Equal(t, "8", cache.rdb.HGet(ctx, hkey, "selected").Val())
	require.Equal(t, "2", cache.rdb.HGet(ctx, hkey, "other").Val())
	require.EqualValues(t, 2, cache.rdb.ZCard(ctx, zkey).Val())

	previousExpiry := cache.rdb.ZScore(ctx, zkey, "selected").Val()
	allowed, pending, err = cache.UpdateInflightBalance(ctx, userID, "selected", 9, 10, 2*time.Minute)
	require.NoError(t, err)
	require.False(t, allowed, "an increase exceeding the remaining balance is rejected")
	require.Equal(t, float64(2), pending)
	require.Equal(t, "8", cache.rdb.HGet(ctx, hkey, "selected").Val(), "rejection preserves the previous reservation")
	require.Equal(t, previousExpiry, cache.rdb.ZScore(ctx, zkey, "selected").Val(), "rejection cannot extend the old owner")
	require.Equal(t, "2", cache.rdb.HGet(ctx, hkey, "other").Val())

	allowed, _, err = cache.UpdateInflightBalance(ctx, userID, "selected", 4, 0, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "8", cache.rdb.HGet(ctx, hkey, "selected").Val(), "a later cheaper attempt cannot reduce the accepted hold")
}

func TestUpdateInflightBalancePreservesLoneRequestGrace(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	allowed, _, err := cache.ReserveInflightBalance(ctx, 412, "only", 2, 0.01, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, pending, err := cache.UpdateInflightBalance(ctx, 412, "only", 20, 0.01, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed, "repricing the only request retains the existing first-request allowance")
	require.Zero(t, pending)
	_, hkey := billingInflightKeys(412)
	require.Equal(t, "20", cache.rdb.HGet(ctx, hkey, "only").Val())
}

func TestUpdateInflightBalanceCannotResurrectMissingOrExpiredOwner(t *testing.T) {
	for _, failure := range []string{"missing_owner", "missing_amount", "expired_owner"} {
		t.Run(failure, func(t *testing.T) {
			_, cache, _ := newInflightTestEnv(t, true, 60)
			ctx := context.Background()
			const userID int64 = 413
			zkey, hkey := billingInflightKeys(userID)
			allowed, _, err := cache.ReserveInflightBalance(ctx, userID, "selected", 1, 10, time.Minute)
			require.NoError(t, err)
			require.True(t, allowed)
			allowed, _, err = cache.ReserveInflightBalance(ctx, userID, "independent", 2, 10, time.Minute)
			require.NoError(t, err)
			require.True(t, allowed)
			switch failure {
			case "missing_owner":
				require.NoError(t, cache.ReleaseInflightBalance(ctx, userID, "selected"))
			case "missing_amount":
				require.NoError(t, cache.rdb.HDel(ctx, hkey, "selected").Err())
			case "expired_owner":
				require.NoError(t, cache.rdb.ZAdd(ctx, zkey, redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: "selected"}).Err())
			}
			allowed, _, err = cache.UpdateInflightBalance(ctx, userID, "selected", 4, 10, time.Minute)
			require.NoError(t, err)
			require.False(t, allowed)
			require.False(t, cache.rdb.HExists(ctx, hkey, "selected").Val(), "update cannot recreate an unowned amount")
			require.Equal(t, "2", cache.rdb.HGet(ctx, hkey, "independent").Val())
			require.NoError(t, cache.rdb.ZScore(ctx, zkey, "independent").Err())
			if failure != "missing_amount" {
				require.EqualValues(t, 1, cache.rdb.ZCard(ctx, zkey).Val())
			}
		})
	}
}

func TestUpdateInflightBalanceIgnoresExpiredCompetingReservation(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 414
	zkey, hkey := billingInflightKeys(userID)
	allowed, _, err := cache.ReserveInflightBalance(ctx, userID, "selected", 1, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, _, err = cache.ReserveInflightBalance(ctx, userID, "expired", 9, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, cache.rdb.ZAdd(ctx, zkey, redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: "expired"}).Err())
	allowed, pending, err := cache.UpdateInflightBalance(ctx, userID, "selected", 10, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Zero(t, pending)
	require.False(t, cache.rdb.HExists(ctx, hkey, "expired").Val())
	require.Equal(t, "10", cache.rdb.HGet(ctx, hkey, "selected").Val())
}

func TestUpdateInflightBalanceCompetingIncreasesAreAtomic(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 415
	for _, owner := range []string{"first", "second"} {
		allowed, _, err := cache.ReserveInflightBalance(ctx, userID, owner, 1, 10, time.Minute)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	type result struct {
		allowed bool
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, owner := range []string{"first", "second"} {
		workers.Add(1)
		go func(owner string) {
			defer workers.Done()
			<-start
			allowed, _, err := cache.UpdateInflightBalance(ctx, userID, owner, 6, 10, time.Minute)
			results <- result{allowed: allowed, err: err}
		}(owner)
	}
	close(start)
	workers.Wait()
	close(results)
	accepted := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.allowed {
			accepted++
		}
	}
	require.Equal(t, 1, accepted, "two six-dollar holds cannot both fit a ten-dollar balance")
	zkey, hkey := billingInflightKeys(userID)
	amounts, err := cache.rdb.HVals(ctx, hkey).Result()
	require.NoError(t, err)
	require.Len(t, amounts, 2, "the rejected request keeps its old one-dollar hold")
	total := 0.0
	for _, amount := range amounts {
		parsed, err := strconv.ParseFloat(amount, 64)
		require.NoError(t, err)
		total += parsed
	}
	require.Equal(t, float64(7), total)
	require.EqualValues(t, 2, cache.rdb.ZCard(ctx, zkey).Val())
}

func TestUpdateInflightBalanceSameOwnerConcurrentIncreasesNeverRegress(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 416
	allowed, _, err := cache.ReserveInflightBalance(ctx, userID, "selected", 1, 100, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	start := make(chan struct{})
	errors := make(chan error, 16)
	var workers sync.WaitGroup
	for amount := 2; amount <= 17; amount++ {
		workers.Add(1)
		go func(amount float64) {
			defer workers.Done()
			<-start
			allowed, _, err := cache.UpdateInflightBalance(ctx, userID, "selected", amount, 100, time.Minute)
			if !allowed && err == nil {
				t.Error("a lone live owner was unexpectedly rejected")
			}
			errors <- err
		}(float64(amount))
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	zkey, hkey := billingInflightKeys(userID)
	require.Equal(t, "17", cache.rdb.HGet(ctx, hkey, "selected").Val())
	require.EqualValues(t, 1, cache.rdb.ZCard(ctx, zkey).Val(), "all updates retain one owner")
}

func TestUpdateInflightBalanceCannotShortenOtherOwnersLifetime(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 417
	zkey, hkey := billingInflightKeys(userID)
	for _, owner := range []string{"selected", "other"} {
		allowed, _, err := cache.ReserveInflightBalance(ctx, userID, owner, 1, 100, 10*time.Minute)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	otherExpiry := cache.rdb.ZScore(ctx, zkey, "other").Val()
	selectedExpiry := cache.rdb.ZScore(ctx, zkey, "selected").Val()
	allowed, _, err := cache.UpdateInflightBalance(ctx, userID, "selected", 2, 100, time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, otherExpiry, cache.rdb.ZScore(ctx, zkey, "other").Val())
	require.Equal(t, selectedExpiry, cache.rdb.ZScore(ctx, zkey, "selected").Val(), "repricing must not shorten the selected owner's existing lease")
	require.Greater(t, cache.rdb.PTTL(ctx, zkey).Val(), 9*time.Minute)
	require.Greater(t, cache.rdb.PTTL(ctx, hkey).Val(), 9*time.Minute)

	allowed, _, err = cache.UpdateInflightBalance(ctx, userID+1, "selected", 3, 100, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed, "the same request ID under another user cannot acquire ownership")
	wrongZKey, wrongHKey := billingInflightKeys(userID + 1)
	require.Zero(t, cache.rdb.Exists(ctx, wrongZKey, wrongHKey).Val())
	require.Equal(t, "2", cache.rdb.HGet(ctx, hkey, "selected").Val())
}

func TestUpdateInflightBalanceErrorsNeverGrantAnUpdate(t *testing.T) {
	_, cache, _ := newInflightTestEnv(t, true, 60)
	ctx := context.Background()
	const userID int64 = 418
	zkey, _ := billingInflightKeys(userID)
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		allowed, _, err := cache.UpdateInflightBalance(ctx, userID, "selected", amount, 10, time.Minute)
		require.Error(t, err)
		require.False(t, allowed)
	}
	require.NoError(t, cache.rdb.Set(ctx, zkey, "wrong Redis type", time.Minute).Err())
	allowed, _, err := cache.UpdateInflightBalance(ctx, userID, "selected", 2, 10, time.Minute)
	require.Error(t, err)
	require.False(t, allowed)
}
