package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
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
	mu            sync.Mutex
	now           time.Time
	reservations  map[int64]map[string]time.Time
	reserveErr    error
	releaseErr    error
	reserveResult *AsyncImageRateLimitStoreResult
	reserveCalls  int
	releaseCalls  int
	lastUserID    int64
	lastRequested int
	lastLimit     int
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
	s.reserveCalls++
	s.lastUserID = userID
	s.lastRequested = requested
	s.lastLimit = limit
	if s.reserveErr != nil {
		return AsyncImageRateLimitStoreResult{}, s.reserveErr
	}
	if s.reserveResult != nil {
		return *s.reserveResult, nil
	}

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
	s.releaseCalls++
	if s.releaseErr != nil {
		return s.releaseErr
	}
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
	return newAsyncImageRateLimiterWithStoreForTest(t, limit, store), store
}

func newAsyncImageRateLimiterWithStoreForTest(
	t *testing.T,
	limit string,
	store AsyncImageRateLimitStore,
) *AsyncImageRateLimiter {
	t.Helper()
	settings := NewSettingService(&asyncImageRateLimitSettingRepo{values: map[string]string{
		SettingKeyAsyncImageUserImagesPerMinute: limit,
	}}, &config.Config{})
	return NewAsyncImageRateLimiter(store, settings)
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

func TestAsyncImageRateLimiterDisabledOrInvalidSettingBypassesStore(t *testing.T) {
	tests := []struct {
		name    string
		setting string
	}{
		{name: "disabled", setting: "0"},
		{name: "missing", setting: ""},
		{name: "negative", setting: "-1"},
		{name: "malformed", setting: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newAsyncImageRateLimitStoreStub()
			limiter := newAsyncImageRateLimiterWithStoreForTest(t, tt.setting, store)

			reservation, err := limiter.Reserve(context.Background(), 42, 3)

			require.NoError(t, err)
			require.Nil(t, reservation)
			require.Zero(t, store.reserveCalls)
		})
	}

	t.Run("setting service unavailable", func(t *testing.T) {
		store := newAsyncImageRateLimitStoreStub()
		limiter := NewAsyncImageRateLimiter(store, nil)

		reservation, err := limiter.Reserve(context.Background(), 42, 3)

		require.NoError(t, err)
		require.Nil(t, reservation)
		require.Zero(t, store.reserveCalls)
	})
}

func TestAsyncImageRateLimiterFailsClosedWhenStoreIsUnavailable(t *testing.T) {
	limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", nil)

	reservation, err := limiter.Reserve(context.Background(), 42, 1)

	require.Nil(t, reservation)
	require.ErrorIs(t, err, ErrAsyncImageRateLimiterUnavailable)
}

func TestAsyncImageRateLimiterWrapsStoreErrorsAsUnavailable(t *testing.T) {
	store := newAsyncImageRateLimitStoreStub()
	store.reserveErr = errors.New("redis unavailable")
	limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", store)

	reservation, err := limiter.Reserve(context.Background(), 42, 1)

	require.Nil(t, reservation)
	require.ErrorIs(t, err, ErrAsyncImageRateLimiterUnavailable)
	require.ErrorContains(t, err, "redis unavailable")
	var exceeded *AsyncImageRateLimitExceeded
	require.False(t, errors.As(err, &exceeded))
}

func TestAsyncImageRateLimiterNormalizesRequestedUnitsAndRetryAfter(t *testing.T) {
	t.Run("non-positive requested units count as one", func(t *testing.T) {
		for _, requested := range []int{0, -3} {
			store := newAsyncImageRateLimitStoreStub()
			allowed := AsyncImageRateLimitStoreResult{Allowed: true}
			store.reserveResult = &allowed
			limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", store)

			reservation, err := limiter.Reserve(context.Background(), 42, requested)

			require.NoError(t, err)
			require.NotNil(t, reservation)
			require.Equal(t, 1, store.reserveCalls)
			require.Equal(t, int64(42), store.lastUserID)
			require.Equal(t, 1, store.lastRequested)
			require.Equal(t, 4, store.lastLimit)
		}
	})

	t.Run("retry after is floored at one second", func(t *testing.T) {
		store := newAsyncImageRateLimitStoreStub()
		rejected := AsyncImageRateLimitStoreResult{RetryAfter: time.Millisecond}
		store.reserveResult = &rejected
		limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", store)

		reservation, err := limiter.Reserve(context.Background(), 42, 1)

		require.Nil(t, reservation)
		var exceeded *AsyncImageRateLimitExceeded
		require.ErrorAs(t, err, &exceeded)
		require.Equal(t, time.Second, exceeded.RetryAfter)
	})
}

func TestAsyncImageRateLimitReservationReleaseIsIdempotentAfterSuccess(t *testing.T) {
	store := newAsyncImageRateLimitStoreStub()
	limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", store)
	reservation, err := limiter.Reserve(context.Background(), 42, 2)
	require.NoError(t, err)

	require.NoError(t, reservation.Release(context.Background()))
	require.NoError(t, reservation.Release(context.Background()))
	require.Equal(t, 1, store.releaseCalls)
}

func TestAsyncImageRateLimitReservationReleaseRetriesAfterError(t *testing.T) {
	store := newAsyncImageRateLimitStoreStub()
	limiter := newAsyncImageRateLimiterWithStoreForTest(t, "4", store)
	reservation, err := limiter.Reserve(context.Background(), 42, 2)
	require.NoError(t, err)
	store.releaseErr = errors.New("redis unavailable")

	err = reservation.Release(context.Background())

	require.ErrorContains(t, err, "release async image rate-limit reservation")
	require.ErrorContains(t, err, "redis unavailable")
	require.Equal(t, 1, store.releaseCalls)

	store.releaseErr = nil
	require.NoError(t, reservation.Release(context.Background()))
	require.Equal(t, 2, store.releaseCalls)
}
