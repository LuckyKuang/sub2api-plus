// Package liverequestcache stores bounded, best-effort gateway observations.
package liverequestcache

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client
}

func New(client *redis.Client) *Store { return &Store{client: client} }

var errUnavailable = errors.New("live request cache unavailable")

var storeLiveRequest = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if old and cjson.decode(old).version >= tonumber(ARGV[2]) then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'EX', 360)
for i = 2, 3 do
  redis.call('ZADD', KEYS[i], ARGV[3], ARGV[4])
  if i == 2 then
    local expired = redis.call('ZRANGEBYSCORE', KEYS[i], '-inf', tonumber(ARGV[3]) - 300000)
    for _, id in ipairs(expired) do redis.call('DEL', 'live:req:' .. id) end
    local evicted = redis.call('ZRANGE', KEYS[i], 0, -2001)
    for _, id in ipairs(evicted) do redis.call('DEL', 'live:req:' .. id) end
  end
  redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', tonumber(ARGV[3]) - 300000)
  local cap = i == 2 and 2000 or 200
  redis.call('ZREMRANGEBYRANK', KEYS[i], 0, -cap-1)
  redis.call('EXPIRE', KEYS[i], 360)
end
return 1
`)

func (s *Store) Store(ctx context.Context, id string, userID, version int64, updatedAt time.Time, data []byte) error {
	if s == nil || s.client == nil {
		return errUnavailable
	}
	return storeLiveRequest.Run(ctx, s.client, []string{
		"live:req:" + id, "live:index:all", "live:index:user:" + strconv.FormatInt(userID, 10),
	}, string(data), version, updatedAt.UnixMilli(), id).Err()
}

func (s *Store) List(ctx context.Context, userID int64, since time.Time) ([]string, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, errUnavailable
	}
	index := "live:index:all"
	if userID > 0 {
		index = "live:index:user:" + strconv.FormatInt(userID, 10)
	}
	ids, err := s.client.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key: index, Start: "+inf", Stop: strconv.FormatInt(since.UnixMilli(), 10),
		ByScore: true, Rev: true, Offset: 0, Count: 201,
	}).Result()
	if err != nil {
		return nil, false, err
	}
	truncated := len(ids) >= 200
	if len(ids) > 200 {
		ids = ids[:200]
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = "live:req:" + id
	}
	values, err := s.read(ctx, keys)
	return values, truncated, err
}

var bindLiveUsage = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if old and old ~= ARGV[1] then
  redis.call('SET', KEYS[1], 'ambiguous', 'EX', 360)
else
  redis.call('SET', KEYS[1], ARGV[1], 'EX', 360)
end
return 1
`)

func (s *Store) BindUsage(ctx context.Context, id string, data []byte) error {
	if s == nil || s.client == nil {
		return errUnavailable
	}
	return bindLiveUsage.Run(ctx, s.client, []string{"live:usage:" + id}, string(data)).Err()
}

func (s *Store) UsageBindings(ctx context.Context, ids []string) ([]string, error) {
	if s == nil || s.client == nil {
		return nil, errUnavailable
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = "live:usage:" + id
	}
	return s.read(ctx, keys)
}

func (s *Store) read(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]string, len(values))
	for i, value := range values {
		result[i], _ = value.(string)
	}
	return result, nil
}
