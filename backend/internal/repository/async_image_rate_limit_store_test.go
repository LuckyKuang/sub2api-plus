//go:build unit

package repository

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestNewAsyncImageRateLimitStoreReturnsNilWithoutRedis(t *testing.T) {
	require.Nil(t, NewAsyncImageRateLimitStore(nil))
}

func TestAsyncImageRateLimitStoreCountsUnitsIsolatesUsersAndSetsTTL(t *testing.T) {
	store, redisServer, rdb := newAsyncImageRateLimitStoreTest(t)
	ctx := context.Background()

	result, err := store.Reserve(ctx, 7, 3, 4, "generation")
	require.NoError(t, err)
	require.True(t, result.Allowed)

	result, err = store.Reserve(ctx, 7, 1, 4, "edit")
	require.NoError(t, err)
	require.True(t, result.Allowed)

	result, err = store.Reserve(ctx, 7, 1, 4, "excess")
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.GreaterOrEqual(t, result.RetryAfter, time.Second)
	require.LessOrEqual(t, result.RetryAfter, time.Minute)

	key := asyncImageRateLimitKeyPrefix + "7"
	require.Equal(t, int64(4), rdb.ZCard(ctx, key).Val())
	require.Greater(t, redisServer.TTL(key), time.Duration(0))
	require.LessOrEqual(t, redisServer.TTL(key), time.Minute)

	otherUser, err := store.Reserve(ctx, 8, 4, 4, "other-user")
	require.NoError(t, err)
	require.True(t, otherUser.Allowed)
	require.Equal(t, int64(4), rdb.ZCard(ctx, asyncImageRateLimitKeyPrefix+"8").Val())
}

func TestAsyncImageRateLimitStoreReleaseTargetsReservationAndIsIdempotent(t *testing.T) {
	store, _, rdb := newAsyncImageRateLimitStoreTest(t)
	ctx := context.Background()
	const userID int64 = 18

	first, err := store.Reserve(ctx, userID, 2, 4, "reservation-a")
	require.NoError(t, err)
	require.True(t, first.Allowed)
	second, err := store.Reserve(ctx, userID, 2, 4, "reservation-b")
	require.NoError(t, err)
	require.True(t, second.Allowed)

	require.NoError(t, store.Release(ctx, userID, "reservation-a", 2))
	key := asyncImageRateLimitKeyPrefix + "18"
	require.Equal(t, int64(2), rdb.ZCard(ctx, key).Val())
	require.ErrorIs(t, rdb.ZScore(ctx, key, "reservation-a:0").Err(), redis.Nil)
	require.NoError(t, rdb.ZScore(ctx, key, "reservation-b:0").Err())

	require.NoError(t, store.Release(ctx, userID, "reservation-a", 2))
	require.Equal(t, int64(2), rdb.ZCard(ctx, key).Val())
}

func TestAsyncImageRateLimitStoreExpiresRollingWindow(t *testing.T) {
	store, redisServer, _ := newAsyncImageRateLimitStoreTest(t)
	ctx := context.Background()

	result, err := store.Reserve(ctx, 23, 4, 4, "old-window")
	require.NoError(t, err)
	require.True(t, result.Allowed)

	redisServer.FastForward(time.Minute)

	result, err = store.Reserve(ctx, 23, 4, 4, "new-window")
	require.NoError(t, err)
	require.True(t, result.Allowed)
}

func TestAsyncImageRateLimitStoreConcurrentReservationsAreAtomic(t *testing.T) {
	store, _, rdb := newAsyncImageRateLimitStoreTest(t)
	ctx := context.Background()
	const (
		userID = int64(99)
		limit  = 4
	)

	var accepted atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := store.Reserve(ctx, userID, 1, limit, "concurrent-"+strconv.Itoa(index))
			if err != nil {
				errs <- err
				return
			}
			if result.Allowed {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int32(limit), accepted.Load())
	require.Equal(t, int64(limit), rdb.ZCard(ctx, asyncImageRateLimitKeyPrefix+"99").Val())
}

func TestAsyncImageRateLimitStoreReturnsErrorsWhenRedisIsUnavailable(t *testing.T) {
	store, redisServer, _ := newAsyncImageRateLimitStoreTest(t)
	redisServer.Close()

	_, err := store.Reserve(context.Background(), 30, 1, 4, "unavailable")
	require.Error(t, err)
	require.Error(t, store.Release(context.Background(), 30, "unavailable", 1))
}

func newAsyncImageRateLimitStoreTest(
	t *testing.T,
) (service.AsyncImageRateLimitStore, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	return NewAsyncImageRateLimitStore(rdb), redisServer, rdb
}
