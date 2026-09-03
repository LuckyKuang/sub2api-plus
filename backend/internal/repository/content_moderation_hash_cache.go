package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	contentModerationFlaggedHashSetKey     = "content_moderation:flagged_hashes"
	contentModerationSessionBlockKeyPrefix = "content_moderation:session_block:"
	contentModerationSessionBlockScanCount = 256
)

func contentModerationSessionBlockRedisKey(blockKey string) string {
	blockKey = strings.TrimSpace(blockKey)
	if blockKey == "" {
		return ""
	}
	return contentModerationSessionBlockKeyPrefix + blockKey
}

var contentModerationEndpointClaimScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {1, 0}
end
if redis.call('EXISTS', KEYS[2]) == 1 then
  return {0, 0}
end
if redis.call('SET', KEYS[3], '1', 'NX', 'PX', ARGV[1]) then
  return {1, 1}
end
return {0, 0}
`)

type contentModerationHashCache struct {
	rdb *redis.Client
}

func NewContentModerationHashCache(rdb *redis.Client) service.ContentModerationHashCache {
	return &contentModerationHashCache{rdb: rdb}
}

func (c *contentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return nil
	}
	return c.rdb.SAdd(ctx, contentModerationFlaggedHashSetKey, inputHash).Err()
}

func (c *contentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	return c.rdb.SIsMember(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
}

func (c *contentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	deleted, err := c.rdb.SRem(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	deleted, err := c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := c.rdb.Del(ctx, contentModerationFlaggedHashSetKey).Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *contentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
}

func (c *contentModerationHashCache) RecordBlockedSession(ctx context.Context, blockKey string, ttl time.Duration) error {
	redisKey := contentModerationSessionBlockRedisKey(blockKey)
	if c == nil || c.rdb == nil || redisKey == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Duration(service.DefaultContentModerationSessionBlockTTLSeconds()) * time.Second
	}
	_, err := c.rdb.SetNX(ctx, redisKey, "1", ttl).Result()
	return err
}

func (c *contentModerationHashCache) HasBlockedSession(ctx context.Context, blockKey string) (bool, error) {
	redisKey := contentModerationSessionBlockRedisKey(blockKey)
	if c == nil || c.rdb == nil || redisKey == "" {
		return false, nil
	}
	n, err := c.rdb.Exists(ctx, redisKey).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *contentModerationHashCache) DeleteBlockedSession(ctx context.Context, blockKey string) (bool, error) {
	redisKey := contentModerationSessionBlockRedisKey(blockKey)
	if c == nil || c.rdb == nil || redisKey == "" {
		return false, nil
	}
	deleted, err := c.rdb.Del(ctx, redisKey).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearBlockedSessions(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	var (
		cursor  uint64
		deleted int64
	)
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, contentModerationSessionBlockKeyPrefix+"*", contentModerationSessionBlockScanCount).Result()
		if err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			n, err := c.rdb.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, err
			}
			deleted += n
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}

func (c *contentModerationHashCache) ClaimEndpoint(ctx context.Context, endpointID string, probeTTL time.Duration) (bool, bool, error) {
	if c == nil || c.rdb == nil {
		return true, false, nil
	}
	openKey, cooldownKey, probeKey := contentModerationEndpointStateKeys(endpointID)
	if openKey == "" {
		return false, false, nil
	}
	if probeTTL <= 0 {
		probeTTL = 5 * time.Second
	}
	result, err := contentModerationEndpointClaimScript.Run(ctx, c.rdb, []string{openKey, cooldownKey, probeKey}, probeTTL.Milliseconds()).Slice()
	if err != nil {
		return false, false, err
	}
	if len(result) != 2 {
		return false, false, redis.Nil
	}
	claimed, _ := result[0].(int64)
	halfOpen, _ := result[1].(int64)
	return claimed == 1, halfOpen == 1, nil
}

func (c *contentModerationHashCache) OpenEndpoint(ctx context.Context, endpointID string, cooldown time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	openKey, cooldownKey, probeKey := contentModerationEndpointStateKeys(endpointID)
	if openKey == "" {
		return nil
	}
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	_, err := c.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, openKey, "1", 0)
		pipe.Set(ctx, cooldownKey, "1", cooldown)
		pipe.Del(ctx, probeKey)
		return nil
	})
	return err
}

func (c *contentModerationHashCache) CloseEndpoint(ctx context.Context, endpointID string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	openKey, cooldownKey, probeKey := contentModerationEndpointStateKeys(endpointID)
	if openKey == "" {
		return nil
	}
	return c.rdb.Del(ctx, openKey, cooldownKey, probeKey).Err()
}

func (c *contentModerationHashCache) ReleaseEndpointProbe(ctx context.Context, endpointID string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	_, _, probeKey := contentModerationEndpointStateKeys(endpointID)
	if probeKey == "" {
		return nil
	}
	return c.rdb.Del(ctx, probeKey).Err()
}

func contentModerationEndpointStateKeys(endpointID string) (string, string, string) {
	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		return "", "", ""
	}
	digest := sha256.Sum256([]byte(endpointID))
	suffix := hex.EncodeToString(digest[:8])
	prefix := "content_moderation:endpoint:" + suffix
	return prefix + ":open", prefix + ":cooldown", prefix + ":probe"
}
