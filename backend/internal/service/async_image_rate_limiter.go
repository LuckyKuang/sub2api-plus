package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	asyncImageRateLimitWindow = time.Minute
)

var (
	// ErrAsyncImageRateLimiterUnavailable is returned only when the feature is
	// configured but Redis cannot atomically reserve the requested image units.
	ErrAsyncImageRateLimiterUnavailable = errors.New("async image rate limiter is unavailable")
)

// AsyncImageRateLimitExceeded carries the response metadata for a rejected
// reservation. It intentionally counts image units rather than requests.
type AsyncImageRateLimitExceeded struct {
	Limit      int
	RetryAfter time.Duration
}

func (e *AsyncImageRateLimitExceeded) Error() string {
	if e == nil {
		return "async image generation limit exceeded"
	}
	return fmt.Sprintf("async image generation limit exceeded: %d images per 60 seconds", e.Limit)
}

// AsyncImageRateLimitReservation is released only if submission fails before a
// task is persisted. Once a task exists, upstream failures still consume the
// reserved image units by design.
type AsyncImageRateLimitReservation struct {
	limiter   *AsyncImageRateLimiter
	userID    int64
	requestID string
	requested int
}

func (r *AsyncImageRateLimitReservation) Release(ctx context.Context) error {
	if r == nil || r.limiter == nil || r.limiter.store == nil || r.requested <= 0 {
		return nil
	}
	if err := r.limiter.store.Release(ctx, r.userID, r.requestID, r.requested); err != nil {
		return fmt.Errorf("release async image rate-limit reservation: %w", err)
	}
	r.requested = 0
	return nil
}

// AsyncImageRateLimitStore atomically reserves image units in a rolling window.
// Repository implementations provide the shared-storage semantics.
type AsyncImageRateLimitStore interface {
	Reserve(ctx context.Context, userID int64, requested, limit int, reservationID string) (AsyncImageRateLimitStoreResult, error)
	Release(ctx context.Context, userID int64, reservationID string, requested int) error
}

type AsyncImageRateLimitStoreResult struct {
	Allowed    bool
	RetryAfter time.Duration
}

// AsyncImageRateLimiter reserves generated-image units through the configured
// shared store. Its repository implementation uses Redis TIME inside a Lua
// script so API instances share one rolling 60-second window.
type AsyncImageRateLimiter struct {
	store    AsyncImageRateLimitStore
	settings *SettingService
}

func NewAsyncImageRateLimiter(store AsyncImageRateLimitStore, settings *SettingService) *AsyncImageRateLimiter {
	return &AsyncImageRateLimiter{
		store:    store,
		settings: settings,
	}
}

// Reserve uses the current global setting for every request, so an administrator
// change takes effect on the next submission without restarting the process.
// A configured limiter fails closed because accepting work during a Redis outage
// would silently bypass the user-level protection.
func (l *AsyncImageRateLimiter) Reserve(ctx context.Context, userID int64, requested int) (*AsyncImageRateLimitReservation, error) {
	if l == nil || l.settings == nil {
		return nil, nil
	}
	limit := l.settings.GetAsyncImageUserImagesPerMinute(ctx)
	if limit <= 0 {
		return nil, nil
	}
	if requested <= 0 {
		requested = 1
	}
	if l.store == nil {
		return nil, ErrAsyncImageRateLimiterUnavailable
	}

	requestID := uuid.NewString()
	result, err := l.store.Reserve(ctx, userID, requested, limit, requestID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAsyncImageRateLimiterUnavailable, err)
	}
	if !result.Allowed {
		retry := result.RetryAfter
		if retry < time.Second {
			retry = time.Second
		}
		return nil, &AsyncImageRateLimitExceeded{Limit: limit, RetryAfter: retry}
	}

	return &AsyncImageRateLimitReservation{limiter: l, userID: userID, requestID: requestID, requested: requested}, nil
}
