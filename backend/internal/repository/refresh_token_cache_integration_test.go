//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// AUTHENTICATION.md requires exactly one rotation winner and preservation of
// the credential owner's family and binding metadata in the consumed value.
func TestRefreshTokenCacheConsumeRealRedis(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewRefreshTokenCache(rdb)
	data := &service.RefreshTokenData{UserID: 7, FamilyID: "family-fixture", TokenVersion: 3, BindingHash: "binding-fixture", ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}
	require.NoError(t, cache.StoreRefreshToken(ctx, "consume-fixture", data, time.Minute))
	const workers = 24
	start := make(chan struct{})
	type result struct {
		data *service.RefreshTokenData
		err  error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			value, err := cache.ConsumeRefreshToken(ctx, "consume-fixture")
			results <- result{value, err}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	winners, missing := 0, 0
	for observed := range results {
		if observed.err == nil {
			winners++
			require.Equal(t, data, observed.data)
		} else {
			require.ErrorIs(t, observed.err, service.ErrRefreshTokenNotFound)
			require.Nil(t, observed.data)
			missing++
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 23, missing)
	_, err := cache.ConsumeRefreshToken(ctx, "consume-fixture")
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	_, err = cache.GetRefreshToken(ctx, "consume-fixture")
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	require.NoError(t, rdb.Close())
	value, err := cache.ConsumeRefreshToken(ctx, "closed-redis")
	require.Error(t, err)
	require.False(t, errors.Is(err, service.ErrRefreshTokenNotFound), "dependency failure must not be classified as a replay")
	require.Nil(t, value)
}
