package service

import (
	"context"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
)

func (f *ChannelMonitorQuotaFetcher) fetchCNQuotaGeneration(ctx context.Context, account *Account, now time.Time) *domain.MonitorQuotaSnapshot {
	key := "monitor-cn-quota:" + cnQuotaGeneration(account)
	readCache := func() *domain.MonitorQuotaSnapshot {
		f.mu.Lock()
		defer f.mu.Unlock()
		if entry, ok := f.cnGenerationCache[key]; ok && time.Now().Before(entry.expiry) {
			return entry.snapshot
		}
		return nil
	}
	if snapshot := readCache(); snapshot != nil {
		return snapshot
	}
	ch := f.flight.DoChan(key, func() (any, error) {
		if snapshot := readCache(); snapshot != nil {
			return snapshot, nil
		}
		queryCtx, cancel := context.WithTimeout(context.Background(), monitorQuotaFetchTimeout)
		defer cancel()
		snapshot := f.fetchCNQuota(queryCtx, account, time.Now())
		ttl := monitorQuotaFetchCacheTTL
		if !snapshot.Success {
			ttl = monitorQuotaErrorCacheTTL
		}
		f.mu.Lock()
		if f.cnGenerationCache == nil {
			f.cnGenerationCache = map[string]monitorQuotaCacheEntry{}
		}
		for entryKey, entry := range f.cnGenerationCache {
			if time.Now().After(entry.expiry) {
				delete(f.cnGenerationCache, entryKey)
			}
		}
		f.cnGenerationCache[key] = monitorQuotaCacheEntry{snapshot: snapshot, expiry: time.Now().Add(ttl)}
		f.mu.Unlock()
		return snapshot, nil
	})
	select {
	case <-ctx.Done():
		return quotaErrorSnapshot("cn_quota", "context canceled", now)
	case result := <-ch:
		snapshot, ok := result.Val.(*domain.MonitorQuotaSnapshot)
		if result.Err != nil || !ok || snapshot == nil {
			return quotaErrorSnapshot("cn_quota", "quota fetch failed", now)
		}
		return snapshot
	}
}
