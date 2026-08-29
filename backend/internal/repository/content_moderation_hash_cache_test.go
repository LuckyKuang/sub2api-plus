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
