//go:build integration

package repository

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type AsyncImageRateLimitStoreSuite struct {
	IntegrationRedisSuite
	store service.AsyncImageRateLimitStore
}

func (s *AsyncImageRateLimitStoreSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.store = NewAsyncImageRateLimitStore(s.rdb)
}

func (s *AsyncImageRateLimitStoreSuite) TestCountsUnitsIsolatesUsersAndSetsTTL() {
	result, err := s.store.Reserve(s.ctx, 7, 3, 4, "generation")
	require.NoError(s.T(), err)
	require.True(s.T(), result.Allowed)

	result, err = s.store.Reserve(s.ctx, 7, 1, 4, "edit")
	require.NoError(s.T(), err)
	require.True(s.T(), result.Allowed)

	result, err = s.store.Reserve(s.ctx, 7, 1, 4, "excess")
	require.NoError(s.T(), err)
	require.False(s.T(), result.Allowed)
	require.GreaterOrEqual(s.T(), result.RetryAfter, time.Second)
	require.LessOrEqual(s.T(), result.RetryAfter, time.Minute)

	key := asyncImageRateLimitKeyPrefix + "7"
	require.Equal(s.T(), int64(4), s.rdb.ZCard(s.ctx, key).Val())
	ttl, err := s.rdb.PTTL(s.ctx, key).Result()
	require.NoError(s.T(), err)
	s.AssertTTLWithin(ttl, time.Second, time.Minute)

	otherUser, err := s.store.Reserve(s.ctx, 8, 4, 4, "other-user")
	require.NoError(s.T(), err)
	require.True(s.T(), otherUser.Allowed)
	require.Equal(s.T(), int64(4), s.rdb.ZCard(s.ctx, asyncImageRateLimitKeyPrefix+"8").Val())
}

func (s *AsyncImageRateLimitStoreSuite) TestConcurrentReservationsAreAtomic() {
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
			result, err := s.store.Reserve(s.ctx, userID, 1, limit, "concurrent-"+strconv.Itoa(index))
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
		require.NoError(s.T(), err)
	}

	require.Equal(s.T(), int32(limit), accepted.Load())
	require.Equal(s.T(), int64(limit), s.rdb.ZCard(s.ctx, asyncImageRateLimitKeyPrefix+"99").Val())
}

func (s *AsyncImageRateLimitStoreSuite) TestReleaseTargetsReservationAndIsIdempotent() {
	const userID int64 = 18
	first, err := s.store.Reserve(s.ctx, userID, 2, 4, "reservation-a")
	require.NoError(s.T(), err)
	require.True(s.T(), first.Allowed)
	second, err := s.store.Reserve(s.ctx, userID, 2, 4, "reservation-b")
	require.NoError(s.T(), err)
	require.True(s.T(), second.Allowed)

	require.NoError(s.T(), s.store.Release(s.ctx, userID, "reservation-a", 2))
	key := asyncImageRateLimitKeyPrefix + "18"
	require.Equal(s.T(), int64(2), s.rdb.ZCard(s.ctx, key).Val())
	require.ErrorIs(s.T(), s.rdb.ZScore(s.ctx, key, "reservation-a:0").Err(), redis.Nil)
	require.NoError(s.T(), s.rdb.ZScore(s.ctx, key, "reservation-b:0").Err())

	require.NoError(s.T(), s.store.Release(s.ctx, userID, "reservation-a", 2))
	require.Equal(s.T(), int64(2), s.rdb.ZCard(s.ctx, key).Val())
}

func (s *AsyncImageRateLimitStoreSuite) TestRemovesExpiredEntriesBeforeReserving() {
	key := asyncImageRateLimitKeyPrefix + "23"
	oldScore := float64(time.Now().Add(-61 * time.Second).UnixMilli())
	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key,
		redis.Z{Score: oldScore, Member: "expired:0"},
		redis.Z{Score: oldScore, Member: "expired:1"},
		redis.Z{Score: oldScore, Member: "expired:2"},
		redis.Z{Score: oldScore, Member: "expired:3"},
	).Err())
	require.NoError(s.T(), s.rdb.Expire(s.ctx, key, 2*time.Minute).Err())

	result, err := s.store.Reserve(s.ctx, 23, 4, 4, "new-window")
	require.NoError(s.T(), err)
	require.True(s.T(), result.Allowed)
	require.Equal(s.T(), int64(4), s.rdb.ZCard(s.ctx, key).Val())
	require.ErrorIs(s.T(), s.rdb.ZScore(s.ctx, key, "expired:0").Err(), redis.Nil)
}

func TestAsyncImageRateLimitStoreSuite(t *testing.T) {
	suite.Run(t, new(AsyncImageRateLimitStoreSuite))
}
