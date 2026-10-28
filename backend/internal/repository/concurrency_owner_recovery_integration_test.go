//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

var errOwnerReplyLost = errors.New("test: Redis owner committed but reply lost")
var errOwnerReleaseUnavailable = errors.New("test: first Redis owner release unavailable")

// Keep the production Redis implementation, including renewal and exact-owner
// deletion. Only the two ambiguous network outcomes are injected at its edge.
type ownerRecoveryRedisCache struct {
	*concurrencyCache
	mu               sync.Mutex
	loseAcquireScope string
	failReleaseScope string
	lostOwner        string
}

func (c *ownerRecoveryRedisCache) acquireReply(scope, owner string, acquired bool, err error) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && acquired && c.loseAcquireScope == scope {
		c.loseAcquireScope = ""
		c.lostOwner = owner
		return false, errOwnerReplyLost
	}
	return acquired, err
}

func (c *ownerRecoveryRedisCache) AcquireUserSlot(ctx context.Context, id int64, limit int, owner string) (bool, error) {
	acquired, err := c.concurrencyCache.AcquireUserSlot(ctx, id, limit, owner)
	return c.acquireReply("user", owner, acquired, err)
}

func (c *ownerRecoveryRedisCache) AcquireAccountSlot(ctx context.Context, id int64, limit int, owner string) (bool, error) {
	acquired, err := c.concurrencyCache.AcquireAccountSlot(ctx, id, limit, owner)
	return c.acquireReply("account", owner, acquired, err)
}

func (c *ownerRecoveryRedisCache) failRelease(scope string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failReleaseScope == scope {
		c.failReleaseScope = ""
		return true
	}
	return false
}

func (c *ownerRecoveryRedisCache) ReleaseUserSlot(ctx context.Context, id int64, owner string) error {
	if c.failRelease("user") {
		return errOwnerReleaseUnavailable
	}
	return c.concurrencyCache.ReleaseUserSlot(ctx, id, owner)
}

func (c *ownerRecoveryRedisCache) ReleaseAccountSlot(ctx context.Context, id int64, owner string) error {
	if c.failRelease("account") {
		return errOwnerReleaseUnavailable
	}
	return c.concurrencyCache.ReleaseAccountSlot(ctx, id, owner)
}

func acquireRedisRecoverySlot(ctx context.Context, svc *service.ConcurrencyService, scope string, id int64) (*service.AcquireResult, error) {
	if scope == "user" {
		return svc.AcquireUserSlot(ctx, id, 2)
	}
	return svc.AcquireAccountSlot(ctx, id, 2)
}

func (s *ConcurrencyCacheSuite) TestOwnerRecoveryLostAcquireReplyReclaimsOnlyExactRedisOwner() {
	for _, scope := range []string{"user", "account"} {
		s.Run(scope, func() {
			const id int64 = 9921
			key := userSlotKey(id)
			if scope == "account" {
				key = accountSlotKey(id)
			}
			now, err := s.rawCache.redisUnixSeconds(s.ctx)
			require.NoError(s.T(), err)
			// This live owner belongs to another instance and must survive every
			// compensation, including startup or account recovery.
			require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key, redis.Z{Score: float64(now), Member: "foreign-owner"}).Err())
			cache := &ownerRecoveryRedisCache{concurrencyCache: s.rawCache, loseAcquireScope: scope}
			svc := service.NewConcurrencyService(cache)
			result, err := acquireRedisRecoverySlot(s.ctx, svc, scope, id)
			require.ErrorIs(s.T(), err, errOwnerReplyLost)
			require.Nil(s.T(), result)
			require.NotEmpty(s.T(), cache.lostOwner, "the failure must follow a successful production Lua acquisition")
			members, err := s.rdb.ZRange(s.ctx, key, 0, -1).Result()
			require.NoError(s.T(), err)
			require.Equal(s.T(), []string{"foreign-owner"}, members, "uncertain admission must leave no ghost owner in the actual Redis ZSET")

			next, err := acquireRedisRecoverySlot(s.ctx, svc, scope, id)
			require.NoError(s.T(), err)
			require.True(s.T(), next.Acquired, "the next request must enter immediately without waiting for the Redis TTL")
			next.ReleaseFunc()
			members, err = s.rdb.ZRange(s.ctx, key, 0, -1).Result()
			require.NoError(s.T(), err)
			require.Equal(s.T(), []string{"foreign-owner"}, members)
		})
	}
}

func (s *ConcurrencyCacheSuite) TestOwnerRecoveryFailedRedisReleaseReadmitsImmediately() {
	for _, scope := range []string{"user", "account"} {
		for _, uncertainAcquire := range []bool{false, true} {
			s.Run(fmt.Sprintf("%s/uncertain-acquire=%t", scope, uncertainAcquire), func() {
				const id int64 = 9922
				key := userSlotKey(id)
				if scope == "account" {
					key = accountSlotKey(id)
				}
				now, err := s.rawCache.redisUnixSeconds(s.ctx)
				require.NoError(s.T(), err)
				require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key, redis.Z{Score: float64(now), Member: "foreign-owner"}).Err())
				cache := &ownerRecoveryRedisCache{concurrencyCache: s.rawCache, failReleaseScope: scope}
				if uncertainAcquire {
					cache.loseAcquireScope = scope
				}
				svc := service.NewConcurrencyService(cache)
				first, err := acquireRedisRecoverySlot(s.ctx, svc, scope, id)
				if uncertainAcquire {
					require.ErrorIs(s.T(), err, errOwnerReplyLost)
					require.Nil(s.T(), first)
				} else {
					require.NoError(s.T(), err)
					require.True(s.T(), first.Acquired)
					first.ReleaseFunc()
					first.ReleaseFunc()
				}
				members, err := s.rdb.ZRange(s.ctx, key, 0, -1).Result()
				require.NoError(s.T(), err)
				require.Len(s.T(), members, 2, "the failed first delete must genuinely retain the completed owner")
				require.Contains(s.T(), members, "foreign-owner")
				next, err := acquireRedisRecoverySlot(s.ctx, svc, scope, id)
				require.NoError(s.T(), err)
				require.True(s.T(), next.Acquired, "recovery must precede the next Redis limit decision")
				next.ReleaseFunc()
				members, err = s.rdb.ZRange(s.ctx, key, 0, -1).Result()
				require.NoError(s.T(), err)
				require.Equal(s.T(), []string{"foreign-owner"}, members, "only the completed and replacement request may be released")
			})
		}
	}
}
