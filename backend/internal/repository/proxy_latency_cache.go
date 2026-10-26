package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

const proxyLatencyKeyPrefix = "proxy:latency:"

const proxyLatencySnapshotTTL = 24 * time.Hour

func proxyLatencyKey(proxy *service.Proxy) (string, error) {
	if proxy == nil || proxy.ID <= 0 {
		return "", errors.New("proxy observation owner is unavailable")
	}
	// PostgreSQL stores timestamps to microsecond precision. Ent create returns
	// its original in-memory timestamp, while other nodes load the stored value.
	// Normalize both representations before deriving this private cache key.
	snapshot := *proxy
	snapshot.CreatedAt = snapshot.CreatedAt.UTC().Round(time.Microsecond)
	snapshot.UpdatedAt = snapshot.UpdatedAt.UTC().Round(time.Microsecond)
	if snapshot.ExpiresAt != nil {
		expires := snapshot.ExpiresAt.UTC().Round(time.Microsecond)
		snapshot.ExpiresAt = &expires
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%s%d:%x", proxyLatencyKeyPrefix, proxy.ID, digest), nil
}

type proxyLatencyCache struct {
	rdb *redis.Client
}

func NewProxyLatencyCache(rdb *redis.Client) service.ProxyLatencyCache {
	return &proxyLatencyCache{rdb: rdb}
}

func (c *proxyLatencyCache) GetProxyLatencies(ctx context.Context, proxies []service.Proxy) (map[int64]*service.ProxyLatencyInfo, error) {
	results := make(map[int64]*service.ProxyLatencyInfo)
	if len(proxies) == 0 {
		return results, nil
	}

	keys := make([]string, 0, len(proxies))
	for index := range proxies {
		key, err := proxyLatencyKey(&proxies[index])
		if err != nil {
			return results, err
		}
		keys = append(keys, key)
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return results, err
	}

	for i, raw := range values {
		if raw == nil {
			continue
		}
		var payload []byte
		switch v := raw.(type) {
		case string:
			payload = []byte(v)
		case []byte:
			payload = v
		default:
			continue
		}
		var info service.ProxyLatencyInfo
		if err := json.Unmarshal(payload, &info); err != nil {
			continue
		}
		results[proxies[i].ID] = &info
	}

	return results, nil
}

func (c *proxyLatencyCache) SetProxyLatency(ctx context.Context, proxy *service.Proxy, info *service.ProxyLatencyInfo) error {
	if info == nil {
		return nil
	}
	key, err := proxyLatencyKey(proxy)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	pipeline := c.rdb.TxPipeline()
	pipeline.Set(ctx, key, payload, proxyLatencySnapshotTTL)
	// Older ID-only observations have no verifiable route owner.
	pipeline.Del(ctx, fmt.Sprintf("%s%d", proxyLatencyKeyPrefix, proxy.ID))
	_, err = pipeline.Exec(ctx)
	return err
}
