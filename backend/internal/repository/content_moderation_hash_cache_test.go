package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationEndpointCircuitUsesPassiveHalfOpenLock(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &contentModerationHashCache{rdb: client}
	ctx := context.Background()

	claimed, halfOpen, err := cache.ClaimEndpoint(ctx, "endpoint-a", 5*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	require.False(t, halfOpen)

	require.NoError(t, cache.OpenEndpoint(ctx, "endpoint-a", time.Minute))
	claimed, halfOpen, err = cache.ClaimEndpoint(ctx, "endpoint-a", 5*time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
	require.False(t, halfOpen)

	server.FastForward(time.Minute)
	claimed, halfOpen, err = cache.ClaimEndpoint(ctx, "endpoint-a", 5*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	require.True(t, halfOpen)

	claimed, halfOpen, err = cache.ClaimEndpoint(ctx, "endpoint-a", 5*time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
	require.False(t, halfOpen)

	require.NoError(t, cache.CloseEndpoint(ctx, "endpoint-a"))
	claimed, halfOpen, err = cache.ClaimEndpoint(ctx, "endpoint-a", 5*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	require.False(t, halfOpen)
}

func TestContentModerationHashCache_RecordBlockedSessionDoesNotRenewTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &contentModerationHashCache{rdb: client}
	ctx := context.Background()
	blockKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	require.NoError(t, cache.RecordBlockedSession(ctx, blockKey, time.Minute))
	ttl := server.TTL(contentModerationSessionBlockRedisKey(blockKey))
	require.Greater(t, ttl, 50*time.Second)

	server.FastForward(30 * time.Second)
	require.NoError(t, cache.RecordBlockedSession(ctx, blockKey, time.Hour))
	remaining := server.TTL(contentModerationSessionBlockRedisKey(blockKey))
	require.Greater(t, remaining, 20*time.Second)
	require.Less(t, remaining, 40*time.Second)
}
