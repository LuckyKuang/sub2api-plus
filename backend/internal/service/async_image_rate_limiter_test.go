package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type asyncImageRateLimitSettingRepo struct {
	values map[string]string
}

func (r *asyncImageRateLimitSettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (r *asyncImageRateLimitSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (r *asyncImageRateLimitSettingRepo) Set(context.Context, string, string) error { return nil }
func (r *asyncImageRateLimitSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r *asyncImageRateLimitSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *asyncImageRateLimitSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (r *asyncImageRateLimitSettingRepo) Delete(context.Context, string) error { return nil }

func newAsyncImageRateLimiterForTest(t *testing.T, limit string) (*AsyncImageRateLimiter, *miniredis.Miniredis) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	settings := NewSettingService(&asyncImageRateLimitSettingRepo{values: map[string]string{
		SettingKeyAsyncImageUserImagesPerMinute: limit,
	}}, &config.Config{})
	return NewAsyncImageRateLimiter(client, settings), redisServer
}

func TestAsyncImageRateLimiterCountsGenerationAndEditOutputUnitsAcrossAPIKeys(t *testing.T) {
	limiter, _ := newAsyncImageRateLimiterForTest(t, "4")
	ctx := context.Background()

	// Task type and API key identity are intentionally absent from the limiter
	// key: a generation reserving three outputs leaves only one output for an
	// edit submitted by the same user through any API key.
	reservation, err := limiter.Reserve(ctx, 42, 3)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	editReservation, err := limiter.Reserve(ctx, 42, 1)
	require.NoError(t, err)
	require.NotNil(t, editReservation)
	_, err = limiter.Reserve(ctx, 42, 1)
	var exceeded *AsyncImageRateLimitExceeded
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, 4, exceeded.Limit)
	require.GreaterOrEqual(t, exceeded.RetryAfter, time.Second)

	// A different user has an independent rolling window.
	other, err := limiter.Reserve(ctx, 43, 4)
	require.NoError(t, err)
	require.NotNil(t, other)
}

func TestAsyncImageRateLimiterAllowsFourSinglesThenRejectsAndExpires(t *testing.T) {
	limiter, redisServer := newAsyncImageRateLimiterForTest(t, "4")
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		reservation, err := limiter.Reserve(ctx, 7, 1)
		require.NoError(t, err)
		require.NotNil(t, reservation)
	}
	_, err := limiter.Reserve(ctx, 7, 1)
	require.ErrorAs(t, err, new(*AsyncImageRateLimitExceeded))

	redisServer.FastForward(asyncImageRateLimitWindow)
	reservation, err := limiter.Reserve(ctx, 7, 1)
	require.NoError(t, err)
	require.NotNil(t, reservation)
}

func TestAsyncImageRateLimiterReleaseAndConcurrentReservations(t *testing.T) {
	limiter, _ := newAsyncImageRateLimiterForTest(t, "4")
	ctx := context.Background()
	reservation, err := limiter.Reserve(ctx, 18, 4)
	require.NoError(t, err)
	require.NoError(t, reservation.Release(ctx), "failed task persistence must return the reservation")
	_, err = limiter.Reserve(ctx, 18, 4)
	require.NoError(t, err)

	limiter, _ = newAsyncImageRateLimiterForTest(t, "4")
	var accepted int
	var lock sync.Mutex
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, reserveErr := limiter.Reserve(ctx, 99, 1); reserveErr == nil {
				lock.Lock()
				accepted++
				lock.Unlock()
			}
		}()
	}
	wait.Wait()
	require.Equal(t, 4, accepted)
}
