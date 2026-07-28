package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const asyncImageRateLimitKeyPrefix = "async-image:rate:user:"

type asyncImageRateLimitStore struct {
	rdb     *redis.Client
	reserve *redis.Script
}

func NewAsyncImageRateLimitStore(rdb *redis.Client) service.AsyncImageRateLimitStore {
	if rdb == nil {
		return nil
	}
	return &asyncImageRateLimitStore{
		rdb: rdb,
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

func (s *asyncImageRateLimitStore) Reserve(
	ctx context.Context,
	userID int64,
	requested, limit int,
	reservationID string,
) (service.AsyncImageRateLimitStoreResult, error) {
	if s == nil || s.rdb == nil || s.reserve == nil {
		return service.AsyncImageRateLimitStoreResult{}, errors.New("async image rate-limit store is unavailable")
	}
	key := asyncImageRateLimitKeyPrefix + strconv.FormatInt(userID, 10)
	result, err := s.reserve.Run(ctx, s.rdb, []string{key}, requested, limit, reservationID).Int64Slice()
	if err != nil {
		return service.AsyncImageRateLimitStoreResult{}, err
	}
	if len(result) < 3 {
		return service.AsyncImageRateLimitStoreResult{}, fmt.Errorf("unexpected Redis reservation result")
	}
	return service.AsyncImageRateLimitStoreResult{
		Allowed:    result[0] == 1,
		RetryAfter: time.Duration(result[2]) * time.Millisecond,
	}, nil
}

func (s *asyncImageRateLimitStore) Release(ctx context.Context, userID int64, reservationID string, requested int) error {
	if s == nil || s.rdb == nil {
		return errors.New("async image rate-limit store is unavailable")
	}
	members := make([]any, requested)
	for index := range members {
		members[index] = reservationID + ":" + strconv.Itoa(index)
	}
	key := asyncImageRateLimitKeyPrefix + strconv.FormatInt(userID, 10)
	return s.rdb.ZRem(ctx, key, members...).Err()
}
