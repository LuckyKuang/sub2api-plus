//go:build unit || !integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestMediaBillingClaimExpiryCannotDeleteSuccessorOrCompletion(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	a, b := NewGatewayCache(client), NewGatewayCache(client)
	ctx := context.Background()
	key := "10:20:completed-video"
	acquired, err := a.ClaimGrokVideoBilled(ctx, key, "owner-a", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = b.ClaimGrokVideoBilled(ctx, key, "owner-b", 30*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)
	server.FastForward(time.Minute)
	acquired, err = b.ClaimGrokVideoBilled(ctx, key, "owner-b", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired, "an abandoned worker has only a short processing lease")
	// A's delayed error or success cannot alter B's current processing lease.
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, key, "owner-a"))
	completed, err := a.CompleteGrokVideoBilled(ctx, key, "owner-a", 48*time.Hour)
	require.NoError(t, err)
	require.False(t, completed)
	value, err := client.Get(ctx, grokVideoBilledPrefix+key).Result()
	require.NoError(t, err)
	require.Equal(t, "owner-b", value)
	completed, err = b.CompleteGrokVideoBilled(ctx, key, "owner-b", 48*time.Hour)
	require.NoError(t, err)
	require.True(t, completed)
	// Neither a stale worker nor the successful worker can undo completion.
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, key, "owner-a"))
	require.NoError(t, b.ReleaseGrokVideoBilled(ctx, key, "owner-b"))
	server.FastForward(24 * time.Hour)
	acquired, err = a.ClaimGrokVideoBilled(ctx, key, "owner-c", 30*time.Second)
	require.NoError(t, err)
	require.False(t, acquired, "durable success retains a distinct 48-hour completion marker")
}
