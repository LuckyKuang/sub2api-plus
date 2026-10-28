//go:build integration

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func (s *ConcurrencyCacheSuite) TestGlobalUserPoolCountsParentAndSubagentsAcrossInstances() {
	// Business cap is three requests, irrespective of the API key, platform,
	// parent-thread or instance that issued them. Real Redis is authoritative.
	owner := int64(9911)
	peers := []service.ConcurrencyCache{s.cache, NewConcurrencyCache(s.rdb, 15, 900)}
	requests := []string{"instance-a/key-a/openai/main", "instance-b/key-b/grok/sub-1", "instance-a/key-c/stepfun/sub-2"}
	for i, id := range requests {
		ok, err := peers[i%2].AcquireUserSlot(s.ctx, owner, 3, id)
		require.NoError(s.T(), err)
		require.True(s.T(), ok)
	}
	ok, err := peers[1].AcquireUserSlot(s.ctx, owner, 3, "instance-b/key-d/gemini/sub-3")
	require.NoError(s.T(), err)
	require.False(s.T(), ok)
	count, err := s.cache.GetUserConcurrency(s.ctx, owner)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 3, count)
	require.NoError(s.T(), peers[1].CleanupStaleProcessSlots(s.ctx, "brand-new-instance-"))
	count, err = s.cache.GetUserConcurrency(s.ctx, owner)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 3, count, "rolling startup must not erase other owners")
	ok, err = peers[1].AcquireUserSlot(s.ctx, owner, 1, "lowered-limit")
	require.NoError(s.T(), err)
	require.False(s.T(), ok, "lowering a cap blocks additions while existing work drains")
	for i, id := range requests {
		require.NoError(s.T(), peers[i%2].ReleaseUserSlot(s.ctx, owner, id))
	}
	ok, err = peers[1].AcquireUserSlot(s.ctx, owner, 1, "after-drain")
	require.NoError(s.T(), err)
	require.True(s.T(), ok)
}

func (s *ConcurrencyCacheSuite) TestRegularLeaseRefreshDoesNotResurrectOrReapOtherOwners() {
	owner := int64(9912)
	require.True(s.T(), s.rawCache.slotTTLSeconds > 0)
	ok, err := s.rawCache.AcquireUserSlot(s.ctx, owner, 2, "live-other-instance")
	require.NoError(s.T(), err)
	require.True(s.T(), ok)
	now, err := s.rawCache.redisUnixSeconds(s.ctx)
	require.NoError(s.T(), err)
	key := userSlotKey(owner)
	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key, redis.Z{Score: float64(now - int64(s.rawCache.slotTTLSeconds) - 1), Member: "expired-instance"}).Err())
	renewed, err := s.rawCache.RefreshUserSlot(s.ctx, owner, "expired-instance")
	require.NoError(s.T(), err)
	require.False(s.T(), renewed, "an expired owner cannot silently rejoin execution")
	renewed, err = s.rawCache.RefreshUserSlot(s.ctx, owner, "live-other-instance")
	require.NoError(s.T(), err)
	require.True(s.T(), renewed)
	require.NoError(s.T(), s.rawCache.CleanupStaleProcessSlots(s.ctx, "new-instance-"))
	members, err := s.rdb.ZRange(s.ctx, key, 0, -1).Result()
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"live-other-instance"}, members)
}

func (s *ConcurrencyCacheSuite) TestRegularServiceLeaseLivesPastItsOriginalTTL() {
	// A short actual Redis TTL makes the original defect observable without
	// waiting the production TTL. The service must renew while executing.
	cache := NewConcurrencyCache(s.rdb, 1, 60).(*concurrencyCache)
	cache.slotTTLSeconds = 2
	svc := service.NewConcurrencyService(cache)
	ctx, cancel := service.WithConcurrencyLeaseContext(context.Background())
	defer cancel(nil)
	first, err := svc.AcquireUserSlot(ctx, 9913, 1)
	require.NoError(s.T(), err)
	require.True(s.T(), first.Acquired)
	defer first.ReleaseFunc()
	releaseKey := svc.TrackAPIKeySlot(ctx, 9913)
	defer releaseKey()
	started := time.Now()
	for time.Since(started) < 2500*time.Millisecond {
		require.NoError(s.T(), ctx.Err())
		time.Sleep(25 * time.Millisecond)
	}
	contender, err := cache.AcquireUserSlot(s.ctx, 9913, 1, fmt.Sprintf("after-old-ttl-%d", time.Now().UnixNano()))
	require.NoError(s.T(), err)
	require.False(s.T(), contender)
	keyCounts, err := cache.GetAPIKeyConcurrencyBatch(s.ctx, []int64{9913})
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, keyCounts[9913], "an executing request must remain visible for its API key beyond the original TTL")
	releaseKey()
	releaseKey()
	keyCounts, err = cache.GetAPIKeyConcurrencyBatch(s.ctx, []int64{9913})
	require.NoError(s.T(), err)
	require.Zero(s.T(), keyCounts[9913])
	first.ReleaseFunc()
	count, err := cache.GetUserConcurrency(s.ctx, 9913)
	require.NoError(s.T(), err)
	require.Zero(s.T(), count)
}

func (s *ConcurrencyCacheSuite) TestAPIKeyStatsRefreshDoesNotResurrectMissingOrExpiredOwners() {
	keyID := int64(9915)
	require.NoError(s.T(), s.rawCache.TrackAPIKeySlot(s.ctx, keyID, "active-owner"))
	now, err := s.rawCache.redisUnixSeconds(s.ctx)
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, apiKeySlotKey(keyID), redis.Z{Score: float64(now - int64(s.rawCache.slotTTLSeconds) - 1), Member: "expired-owner"}).Err())
	for _, owner := range []string{"expired-owner", "missing-owner"} {
		refreshed, err := s.rawCache.RefreshAPIKeySlot(s.ctx, keyID, owner)
		require.NoError(s.T(), err)
		require.False(s.T(), refreshed)
	}
	refreshed, err := s.rawCache.RefreshAPIKeySlot(s.ctx, keyID, "active-owner")
	require.NoError(s.T(), err)
	require.True(s.T(), refreshed)
	members, err := s.rdb.ZRange(s.ctx, apiKeySlotKey(keyID), 0, -1).Result()
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"active-owner"}, members)
}

func (s *ConcurrencyCacheSuite) TestUnlimitedUserRequestsRemainVisibleWhenLimitIsLowered() {
	// Unlimited bypasses the cap, not accounting. Admin lowering 0->1 must see
	// active execution from every key/provider rather than opening extra capacity.
	owner := int64(9914)
	for _, id := range []string{"key-a/openai/parent", "key-b/grok/child"} {
		ok, err := s.cache.AcquireUserSlot(s.ctx, owner, 0, id)
		require.NoError(s.T(), err)
		require.True(s.T(), ok)
	}
	count, err := s.cache.GetUserConcurrency(s.ctx, owner)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 2, count)
	ok, err := s.cache.AcquireUserSlot(s.ctx, owner, 1, "after-admin-lowered")
	require.NoError(s.T(), err)
	require.False(s.T(), ok)
	require.NoError(s.T(), s.cache.ReleaseUserSlot(s.ctx, owner, "key-a/openai/parent"))
	ok, err = s.cache.AcquireUserSlot(s.ctx, owner, 1, "while-child-executes")
	require.NoError(s.T(), err)
	require.False(s.T(), ok)
	require.NoError(s.T(), s.cache.ReleaseUserSlot(s.ctx, owner, "key-b/grok/child"))
	ok, err = s.cache.AcquireUserSlot(s.ctx, owner, 1, "after-drain")
	require.NoError(s.T(), err)
	require.True(s.T(), ok)
}
