package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	asyncImageRateLimitWindow    = time.Minute
	asyncImageRateLimitKeyPrefix = "async-image:rate:user:"
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
	limiter *AsyncImageRateLimiter
	key     string
	members []string
}

func (r *AsyncImageRateLimitReservation) Release(ctx context.Context) error {
	if r == nil || r.limiter == nil || r.limiter.redis == nil || len(r.members) == 0 {
		return nil
	}
	members := make([]interface{}, len(r.members))
	for index, member := range r.members {
		members[index] = member
	}
	if err := r.limiter.redis.ZRem(ctx, r.key, members...).Err(); err != nil {
		return fmt.Errorf("release async image rate-limit reservation: %w", err)
	}
	r.members = nil
	return nil
}

// AsyncImageRateLimiter reserves generated-image units in a Redis ZSET.
// Redis TIME is used inside the Lua script so concurrent API instances share a
// single rolling 60-second window without depending on local clock skew.
type AsyncImageRateLimiter struct {
	redis    *redis.Client
	settings *SettingService
	reserve  *redis.Script
}

func NewAsyncImageRateLimiter(redisClient *redis.Client, settings *SettingService) *AsyncImageRateLimiter {
	return &AsyncImageRateLimiter{
		redis:    redisClient,
		settings: settings,
		reserve: redis.NewScript(`
local now = redis.call('TIME')
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)
local cutoff = now_ms - 60000
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
local used = redis.call('ZCARD', KEYS[1])
local requested = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
if used + requested > limit then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  local retry_ms = 1000
  if oldest[2] then
    retry_ms = math.max(1000, tonumber(oldest[2]) + 60000 - now_ms)
  end
  return {0, used, retry_ms}
end
for i = 0, requested - 1 do
  redis.call('ZADD', KEYS[1], now_ms, ARGV[3] .. ':' .. i)
end
redis.call('PEXPIRE', KEYS[1], 60000)
return {1, used + requested, 0}
`),
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
	if l.redis == nil || l.reserve == nil {
		return nil, ErrAsyncImageRateLimiterUnavailable
	}

	key := asyncImageRateLimitKeyPrefix + strconv.FormatInt(userID, 10)
	requestID := uuid.NewString()
	result, err := l.reserve.Run(ctx, l.redis, []string{key}, requested, limit, requestID).Int64Slice()
	if err != nil || len(result) < 3 {
		if err == nil {
			err = errors.New("unexpected Redis reservation result")
		}
		return nil, fmt.Errorf("%w: %v", ErrAsyncImageRateLimiterUnavailable, err)
	}
	if result[0] == 0 {
		retry := time.Duration(result[2]) * time.Millisecond
		if retry < time.Second {
			retry = time.Second
		}
		return nil, &AsyncImageRateLimitExceeded{Limit: limit, RetryAfter: retry}
	}

	members := make([]string, requested)
	for i := range members {
		members[i] = requestID + ":" + strconv.Itoa(i)
	}
	return &AsyncImageRateLimitReservation{limiter: l, key: key, members: members}, nil
}
