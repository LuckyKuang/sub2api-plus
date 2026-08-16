package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheCodexTurnStateOriginIsSharedHashedAndExpiring(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}

	const (
		seed      = "42\x00client-session-secret"
		accountID = int64(73)
		ttl       = 5 * time.Minute
	)
	require.NoError(t, cache.SetOpenAICodexTurnStateOrigin(context.Background(), seed, accountID, ttl))

	storedAccountID, err := cache.GetOpenAICodexTurnStateOrigin(context.Background(), seed)
	require.NoError(t, err)
	require.Equal(t, accountID, storedAccountID)
	require.True(t, server.Exists(openAICodexTurnStateOriginKey(seed)))
	require.False(t, server.Exists(openAICodexTurnStateOriginPrefix+seed), "raw session seed must not enter Redis keys")

	server.FastForward(ttl)
	storedAccountID, err = cache.GetOpenAICodexTurnStateOrigin(context.Background(), seed)
	require.NoError(t, err)
	require.Zero(t, storedAccountID)
}
