package service

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
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

type asyncImageRateLimitStoreStub struct {
	mu           sync.Mutex
	now          time.Time
	reservations map[int64]map[string]time.Time
}

func newAsyncImageRateLimitStoreStub() *asyncImageRateLimitStoreStub {
	return &asyncImageRateLimitStoreStub{
		now:          time.Now().UTC(),
		reservations: make(map[int64]map[string]time.Time),
	}
}

func (s *asyncImageRateLimitStoreStub) Reserve(_ context.Context, userID int64, requested, limit int, reservationID string) (AsyncImageRateLimitStoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.reservations[userID]
	if entries == nil {
		entries = make(map[string]time.Time)
		s.reservations[userID] = entries
	}
	cutoff := s.now.Add(-asyncImageRateLimitWindow)
	oldest := time.Time{}
	for member, createdAt := range entries {
		if !createdAt.After(cutoff) {
			delete(entries, member)
			continue
		}
		if oldest.IsZero() || createdAt.Before(oldest) {
			oldest = createdAt
		}
	}
	if len(entries)+requested > limit {
		retryAfter := time.Second
		if !oldest.IsZero() {
			retryAfter = oldest.Add(asyncImageRateLimitWindow).Sub(s.now)
			if retryAfter < time.Second {
				retryAfter = time.Second
			}
		}
		return AsyncImageRateLimitStoreResult{RetryAfter: retryAfter}, nil
	}
	for index := 0; index < requested; index++ {
		entries[reservationID+":"+strconv.Itoa(index)] = s.now
	}
	return AsyncImageRateLimitStoreResult{Allowed: true}, nil
}

func (s *asyncImageRateLimitStoreStub) Release(_ context.Context, userID int64, reservationID string, requested int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.reservations[userID]
	for index := 0; index < requested; index++ {
		delete(entries, reservationID+":"+strconv.Itoa(index))
	}
	return nil
}

func (s *asyncImageRateLimitStoreStub) Advance(duration time.Duration) {
	s.mu.Lock()
	s.now = s.now.Add(duration)
	s.mu.Unlock()
}

func newAsyncImageRateLimiterForTest(t *testing.T, limit string) (*AsyncImageRateLimiter, *asyncImageRateLimitStoreStub) {
	t.Helper()
	store := newAsyncImageRateLimitStoreStub()
	settings := NewSettingService(&asyncImageRateLimitSettingRepo{values: map[string]string{
		SettingKeyAsyncImageUserImagesPerMinute: limit,
	}}, &config.Config{})
	return NewAsyncImageRateLimiter(store, settings), store
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
	limiter, store := newAsyncImageRateLimiterForTest(t, "4")
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		reservation, err := limiter.Reserve(ctx, 7, 1)
		require.NoError(t, err)
		require.NotNil(t, reservation)
	}
	_, err := limiter.Reserve(ctx, 7, 1)
	require.ErrorAs(t, err, new(*AsyncImageRateLimitExceeded))

	store.Advance(asyncImageRateLimitWindow)
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
