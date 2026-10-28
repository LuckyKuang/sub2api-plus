//go:build unit || !integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestProvidedConcurrencyQueueTTLIncludesEveryWaiterAndCleanup(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		sticky, fallback time.Duration
		wantTTLSeconds   int
		beforeWaitEnds   time.Duration
	}{
		{name: "production_defaults", sticky: 120 * time.Second, fallback: 30 * time.Second, wantTTLSeconds: 130, beforeWaitEnds: 119 * time.Second},
		{name: "fractional_wait_below_worker_limit", sticky: 1500 * time.Millisecond, fallback: 1500 * time.Millisecond, wantTTLSeconds: 40, beforeWaitEnds: 29 * time.Second},
		{name: "worker_minimum", wantTTLSeconds: 40, beforeWaitEnds: 29 * time.Second},
		{name: "fractional_sticky_rounds_up", sticky: 120500 * time.Millisecond, fallback: 30 * time.Second, wantTTLSeconds: 131, beforeWaitEnds: 120 * time.Second},
		{name: "fractional_fallback_rounds_up", sticky: 30 * time.Second, fallback: 120500 * time.Millisecond, wantTTLSeconds: 131, beforeWaitEnds: 120 * time.Second},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			firstClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
			secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = firstClient.Close(); _ = secondClient.Close() })
			cfg := &config.Config{}
			cfg.Gateway.ConcurrencySlotTTLMinutes = 15
			cfg.Gateway.Scheduling.StickySessionWaitTimeout = scenario.sticky
			cfg.Gateway.Scheduling.FallbackWaitTimeout = scenario.fallback
			first, ok := ProvideConcurrencyCache(firstClient, cfg).(*concurrencyCache)
			require.True(t, ok)
			second, ok := ProvideConcurrencyCache(secondClient, cfg).(*concurrencyCache)
			require.True(t, ok)
			require.Equal(t, scenario.wantTTLSeconds, first.waitQueueTTLSeconds)
			require.Equal(t, scenario.wantTTLSeconds, second.waitQueueTTLSeconds)
			ctx := context.Background()
			const accountID, userID = int64(99601), int64(99602)
			for _, cache := range []*concurrencyCache{first, second} {
				accepted, err := cache.IncrementAccountWaitCount(ctx, accountID, 2)
				require.NoError(t, err)
				require.True(t, accepted)
				accepted, err = cache.IncrementWaitCount(ctx, userID, 2)
				require.NoError(t, err)
				require.True(t, accepted)
			}
			server.FastForward(scenario.beforeWaitEnds)
			require.True(t, server.Exists(accountWaitKey(accountID)), "account queue ownership cannot expire before the final waiter")
			require.True(t, server.Exists(waitQueueKey(userID)), "the private worker queue must retain its thirty-second admission bound")
			waiting, err := second.GetAccountWaitingCount(ctx, accountID)
			require.NoError(t, err)
			require.Equal(t, 2, waiting)
			accepted, err := second.IncrementAccountWaitCount(ctx, accountID, 2)
			require.NoError(t, err)
			require.False(t, accepted, "another instance must still observe the full account queue")
			accepted, err = second.IncrementWaitCount(ctx, userID, 2)
			require.NoError(t, err)
			require.False(t, accepted, "another worker must still observe the full private user queue")
			for _, cache := range []*concurrencyCache{first, second} {
				require.NoError(t, cache.DecrementAccountWaitCount(ctx, accountID))
				require.NoError(t, cache.DecrementWaitCount(ctx, userID))
			}
			waiting, err = second.GetAccountWaitingCount(ctx, accountID)
			require.NoError(t, err)
			require.Zero(t, waiting)
			userWaiting, err := secondClient.Get(ctx, waitQueueKey(userID)).Int()
			require.NoError(t, err)
			require.Zero(t, userWaiting)
			accepted, err = second.IncrementAccountWaitCount(ctx, accountID, 2)
			require.NoError(t, err)
			require.True(t, accepted, "released queue capacity must be reusable across instances")
			// A vanished process still has bounded orphan cleanup.
			server.FastForward(time.Duration(scenario.wantTTLSeconds) * time.Second)
			waiting, err = first.GetAccountWaitingCount(ctx, accountID)
			require.NoError(t, err)
			require.Zero(t, waiting)
		})
	}
}
