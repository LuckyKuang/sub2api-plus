package service

import (
	"context"
	"fmt"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

type userGroupRateResolver struct {
	repo         UserGroupRateRepository
	cache        *gocache.Cache
	cacheTTL     time.Duration
	sf           *singleflight.Group
	logComponent string
}

func newUserGroupRateResolver(repo UserGroupRateRepository, cache *gocache.Cache, cacheTTL time.Duration, sf *singleflight.Group, logComponent string) *userGroupRateResolver {
	if cacheTTL <= 0 {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cache == nil {
		cache = gocache.New(cacheTTL, time.Minute)
	}
	if logComponent == "" {
		logComponent = "service.gateway"
	}
	if sf == nil {
		sf = &singleflight.Group{}
	}

	return &userGroupRateResolver{
		repo:         repo,
		cache:        cache,
		cacheTTL:     cacheTTL,
		sf:           sf,
		logComponent: logComponent,
	}
}

type userGroupRateResolution struct {
	rate     float64
	personal bool
}

func (r *userGroupRateResolver) cachedResolution(key string) (userGroupRateResolution, bool) {
	if r.cache == nil {
		return userGroupRateResolution{}, false
	}
	cached, ok := r.cache.Get(key)
	if !ok {
		return userGroupRateResolution{}, false
	}
	resolved, valid := cached.(userGroupRateResolution)
	return resolved, valid
}

// ResolveFreshSnapshot reads the authoritative personal override for a new
// admission. Sale quotes also read this repository directly, so a committed
// admin edit must take effect without waiting for a process-local TTL. Do not
// coalesce it with a legacy load or publish it into that cache: an older
// in-flight load could otherwise overwrite the newly admitted generation.
// The caller freezes the returned value for retries and delayed settlement.
func (r *userGroupRateResolver) ResolveFreshSnapshot(ctx context.Context, userID, groupID int64, fallback float64) (float64, bool, error) {
	if r == nil || r.repo == nil || userID <= 0 || groupID <= 0 {
		return fallback, false, nil
	}
	userRate, err := r.repo.GetByUserAndGroup(ctx, userID, groupID)
	if err != nil {
		return fallback, false, err
	}
	if userRate == nil {
		return fallback, false, nil
	}
	return *userRate, true, nil
}

// ResolveSnapshot keeps source information and dependency errors, so request
// callers can distinguish cached inheritance from an explicit personal rate.
// New price admissions use ResolveFreshSnapshot instead.
func (r *userGroupRateResolver) ResolveSnapshot(ctx context.Context, userID, groupID int64, fallback float64) (float64, bool, error) {
	if r == nil || userID <= 0 || groupID <= 0 {
		return fallback, false, nil
	}
	key := fmt.Sprintf("%d:%d", userID, groupID)
	if cached, ok := r.cachedResolution(key); ok {
		userGroupRateCacheHitTotal.Add(1)
		if !cached.personal {
			return fallback, false, nil
		}
		return cached.rate, cached.personal, nil
	}
	if r.repo == nil {
		return fallback, false, nil
	}
	userGroupRateCacheMissTotal.Add(1)
	value, err, shared := r.sf.Do(key, func() (any, error) {
		if cached, ok := r.cachedResolution(key); ok {
			userGroupRateCacheHitTotal.Add(1)
			return cached, nil
		}
		userGroupRateCacheLoadTotal.Add(1)
		userRate, loadErr := r.repo.GetByUserAndGroup(ctx, userID, groupID)
		if loadErr != nil {
			return nil, loadErr
		}
		resolved := userGroupRateResolution{rate: fallback, personal: userRate != nil}
		if userRate != nil {
			resolved.rate = *userRate
		}
		if r.cache != nil {
			r.cache.Set(key, resolved, r.cacheTTL)
		}
		return resolved, nil
	})
	if shared {
		userGroupRateCacheSFSharedTotal.Add(1)
	}
	if err != nil {
		return fallback, false, err
	}
	resolved, ok := value.(userGroupRateResolution)
	if !ok {
		return fallback, false, fmt.Errorf("unexpected user group rate resolution")
	}
	if !resolved.personal {
		return fallback, false, nil
	}
	return resolved.rate, resolved.personal, nil
}

func (r *userGroupRateResolver) Resolve(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	rate, _, err := r.ResolveSnapshot(ctx, userID, groupID, groupDefaultMultiplier)
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return groupDefaultMultiplier
	}
	return rate
}
