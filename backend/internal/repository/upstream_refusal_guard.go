package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

const refusalPrefix = "refusal_guard:"

var refusalRecordScript = redis.NewScript(`
if ARGV[1] ~= '' then redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[2],'NX') end
if ARGV[3] == '0' then return 0 end
local n=redis.call('INCR',KEYS[2])
redis.call('EXPIRE',KEYS[2],ARGV[4])
if n >= tonumber(ARGV[5]) then
 redis.call('SET',KEYS[3],ARGV[6], 'EX',ARGV[7])
 redis.call('DEL',KEYS[2])
end
return n
`)

type upstreamRefusalRedisStore struct{ rdb *redis.Client }

func NewUpstreamRefusalStore(rdb *redis.Client) service.UpstreamRefusalStore {
	return &upstreamRefusalRedisStore{rdb}
}
func (s *upstreamRefusalRedisStore) HasSession(ctx context.Context, key string) (bool, error) {
	n, err := s.rdb.Exists(ctx, refusalPrefix+"session:"+key).Result()
	return n > 0, err
}
func (s *upstreamRefusalRedisStore) CooldownRemaining(ctx context.Context, id int64) (time.Duration, error) {
	ttl, err := s.rdb.PTTL(ctx, fmt.Sprintf("%scooldown:user:%d", refusalPrefix, id)).Result()
	if ttl < 0 {
		return 0, err
	}
	return ttl, err
}
func (s *upstreamRefusalRedisStore) Record(ctx context.Context, v service.RefusalSession, cfg service.UpstreamRefusalGuardConfig) (int64, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	id := strconv.FormatInt(v.UserID, 10)
	cooldown := service.RefusalCooldown{UserID: v.UserID, Until: time.Now().Add(time.Duration(cfg.UserCooldownSeconds) * time.Second).UTC(), TriggerRequestID: v.RequestID, Count: int64(cfg.UserThreshold)}
	cd, _ := json.Marshal(cooldown)
	sessionValue := string(raw)
	if v.BlockKey == "" {
		sessionValue = ""
	}
	userFlag := "1"
	if v.UserID <= 0 {
		userFlag = "0"
	}
	return refusalRecordScript.Run(ctx, s.rdb, []string{refusalPrefix + "session:" + v.BlockKey, refusalPrefix + "count:user:" + id, refusalPrefix + "cooldown:user:" + id}, sessionValue, cfg.SessionBlockTTLSeconds, userFlag, cfg.CountWindowSeconds, cfg.UserThreshold, string(cd), cfg.UserCooldownSeconds).Int64()
}
func (s *upstreamRefusalRedisStore) scan(ctx context.Context, pattern string) ([]string, error) {
	keys := make([]string, 0)
	var cursor uint64
	for {
		part, next, err := s.rdb.Scan(ctx, cursor, pattern, 256).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, part...)
		if len(keys) >= 1000 {
			keys = keys[:1000]
			break
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Strings(keys)
	return keys, nil
}
func (s *upstreamRefusalRedisStore) ListSessions(ctx context.Context) ([]service.RefusalSession, error) {
	keys, err := s.scan(ctx, refusalPrefix+"session:*")
	if err != nil {
		return nil, err
	}
	result := make([]service.RefusalSession, 0, len(keys))
	for _, key := range keys {
		raw, err := s.rdb.Get(ctx, key).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		var item service.RefusalSession
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		ttl, err := s.rdb.TTL(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		item.RemainingSeconds = int64(ttl.Seconds())
		result = append(result, item)
	}
	return result, nil
}
func (s *upstreamRefusalRedisStore) ListUsers(ctx context.Context) ([]service.RefusalUser, error) {
	counts, err := s.scan(ctx, refusalPrefix+"count:user:*")
	if err != nil {
		return nil, err
	}
	cooldowns, err := s.scan(ctx, refusalPrefix+"cooldown:user:*")
	if err != nil {
		return nil, err
	}
	users := map[int64]*service.RefusalUser{}
	for _, key := range counts {
		id, _ := strconv.ParseInt(strings.TrimPrefix(key, refusalPrefix+"count:user:"), 10, 64)
		n, err := s.rdb.Get(ctx, key).Int64()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		ttl, err := s.rdb.TTL(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		users[id] = &service.RefusalUser{UserID: id, Count: n, CountTTLSeconds: int64(ttl.Seconds())}
	}
	for _, key := range cooldowns {
		id, _ := strconv.ParseInt(strings.TrimPrefix(key, refusalPrefix+"cooldown:user:"), 10, 64)
		raw, err := s.rdb.Get(ctx, key).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		var cd service.RefusalCooldown
		if json.Unmarshal(raw, &cd) != nil {
			continue
		}
		ttl, err := s.rdb.TTL(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		u := users[id]
		if u == nil {
			u = &service.RefusalUser{UserID: id}
			users[id] = u
		}
		u.CooldownUntil = cd.Until
		u.CooldownRemainingSeconds = int64(ttl.Seconds())
	}
	result := make([]service.RefusalUser, 0, len(users))
	for _, u := range users {
		result = append(result, *u)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
	return result, nil
}
func (s *upstreamRefusalRedisStore) ClearUser(ctx context.Context, id int64) (int64, error) {
	return s.rdb.Del(ctx, fmt.Sprintf("%scount:user:%d", refusalPrefix, id), fmt.Sprintf("%scooldown:user:%d", refusalPrefix, id)).Result()
}
func (s *upstreamRefusalRedisStore) ClearSession(ctx context.Context, key string) (int64, error) {
	if len(key) != 64 {
		return 0, fmt.Errorf("invalid block key")
	}
	for _, ch := range key {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return 0, fmt.Errorf("invalid block key")
		}
	}
	return s.rdb.Del(ctx, refusalPrefix+"session:"+key).Result()
}
func (s *upstreamRefusalRedisStore) ClearAll(ctx context.Context) (int64, error) {
	var total int64
	for _, pattern := range []string{refusalPrefix + "count:user:*", refusalPrefix + "cooldown:user:*", refusalPrefix + "session:*"} {
		for {
			keys, err := s.scan(ctx, pattern)
			if err != nil {
				return total, err
			}
			if len(keys) == 0 {
				break
			}
			n, err := s.rdb.Del(ctx, keys...).Result()
			total += n
			if err != nil {
				return total, err
			}
			if len(keys) < 1000 {
				break
			}
		}
	}
	return total, nil
}
