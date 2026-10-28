//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type userGroupRateResolverRepoStub struct {
	UserGroupRateRepository

	rate  *float64
	err   error
	calls int
}

func (s *userGroupRateResolverRepoStub) GetByUserAndGroup(ctx context.Context, userID, groupID int64) (*float64, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.rate, nil
}

func TestNewUserGroupRateResolver_Defaults(t *testing.T) {
	resolver := newUserGroupRateResolver(nil, nil, 0, nil, "")

	require.NotNil(t, resolver)
	require.NotNil(t, resolver.cache)
	require.Equal(t, defaultUserGroupRateCacheTTL, resolver.cacheTTL)
	require.NotNil(t, resolver.sf)
	require.Equal(t, "service.gateway", resolver.logComponent)
}

func TestUserGroupRateResolverResolve_FallbackForNilResolverAndInvalidIDs(t *testing.T) {
	var nilResolver *userGroupRateResolver
	require.Equal(t, 1.4, nilResolver.Resolve(context.Background(), 101, 202, 1.4))

	resolver := newUserGroupRateResolver(nil, nil, time.Second, nil, "service.test")
	require.Equal(t, 1.4, resolver.Resolve(context.Background(), 0, 202, 1.4))
	require.Equal(t, 1.4, resolver.Resolve(context.Background(), 101, 0, 1.4))
}

func TestUserGroupRateResolverResolve_InvalidCacheEntryLoadsRepoAndCaches(t *testing.T) {
	resetGatewayHotpathStatsForTest()

	rate := 1.7
	repo := &userGroupRateResolverRepoStub{rate: &rate}
	cache := gocache.New(time.Minute, time.Minute)
	cache.Set("101:202", "bad-cache", time.Minute)
	resolver := newUserGroupRateResolver(repo, cache, time.Minute, nil, "service.test")

	got := resolver.Resolve(context.Background(), 101, 202, 1.2)
	require.Equal(t, rate, got)
	require.Equal(t, 1, repo.calls)

	cached, ok := cache.Get("101:202")
	require.True(t, ok)
	require.Equal(t, userGroupRateResolution{rate: rate, personal: true}, cached)

	hit, miss, load, _, fallback := GatewayUserGroupRateCacheStats()
	require.Equal(t, int64(0), hit)
	require.Equal(t, int64(1), miss)
	require.Equal(t, int64(1), load)
	require.Equal(t, int64(0), fallback)
}

func TestUserGroupRateSnapshotInheritanceUsesCurrentGroupDefault(t *testing.T) {
	repo := &userGroupRateResolverRepoStub{}
	resolver := newUserGroupRateResolver(repo, nil, time.Minute, nil, "service.test")
	rate, personal, err := resolver.ResolveSnapshot(context.Background(), 101, 202, .8)
	require.NoError(t, err)
	require.Equal(t, .8, rate)
	require.False(t, personal)
	// An absent personal override remains inheritance after admin changes the
	// group; cached absence must not become an old effective selling price.
	rate, personal, err = resolver.ResolveSnapshot(context.Background(), 101, 202, 1.6)
	require.NoError(t, err)
	require.Equal(t, 1.6, rate)
	require.False(t, personal)
	require.Equal(t, 1, repo.calls)
}

func TestUserGroupRateSnapshotCachesExplicitFreeOverride(t *testing.T) {
	free := 0.0
	repo := &userGroupRateResolverRepoStub{rate: &free}
	resolver := newUserGroupRateResolver(repo, nil, time.Minute, nil, "service.test")
	for _, base := range []float64{.8, 1.6} {
		rate, personal, err := resolver.ResolveSnapshot(context.Background(), 101, 202, base)
		require.NoError(t, err)
		require.Zero(t, rate)
		require.True(t, personal)
	}
	require.Equal(t, 1, repo.calls)
}

func TestGatewayServiceGetUserGroupRateMultiplier_FallbacksAndUsesExistingResolver(t *testing.T) {
	var nilSvc *GatewayService
	require.Equal(t, 1.3, nilSvc.getUserGroupRateMultiplier(context.Background(), 101, 202, 1.3))

	rate := 1.9
	repo := &userGroupRateResolverRepoStub{rate: &rate}
	resolver := newUserGroupRateResolver(repo, nil, time.Minute, nil, "service.gateway")
	svc := &GatewayService{userGroupRateResolver: resolver}

	got := svc.getUserGroupRateMultiplier(context.Background(), 101, 202, 1.2)
	require.Equal(t, rate, got)
	require.Equal(t, 1, repo.calls)
}

func TestUserGroupRateFreshSnapshotIgnoresPositiveAndAbsentTTLStates(t *testing.T) {
	stored := 0.2
	repo := &userGroupRateResolverRepoStub{rate: &stored}
	a := newUserGroupRateResolver(repo, nil, time.Hour, nil, "service.test.a")
	b := newUserGroupRateResolver(repo, nil, time.Hour, nil, "service.test.b")
	require.Equal(t, 0.2, a.Resolve(context.Background(), 101, 202, 1))
	require.Equal(t, 0.2, b.Resolve(context.Background(), 101, 202, 1))
	updated := 0.7
	repo.rate = &updated
	for _, resolver := range []*userGroupRateResolver{a, b} {
		rate, personal, err := resolver.ResolveFreshSnapshot(context.Background(), 101, 202, 1)
		require.NoError(t, err)
		require.True(t, personal)
		require.Equal(t, 0.7, rate, "new admissions must not inherit either process's old cached price")
	}
	// Removing an override immediately restores the request's group default.
	repo.rate = nil
	rate, personal, err := a.ResolveFreshSnapshot(context.Background(), 101, 202, 1.6)
	require.NoError(t, err)
	require.False(t, personal)
	require.Equal(t, 1.6, rate)
	free := 0.0
	repo.rate = &free
	rate, personal, err = a.ResolveFreshSnapshot(context.Background(), 101, 202, 1.6)
	require.NoError(t, err)
	require.True(t, personal)
	require.Zero(t, rate, "an explicit free override remains distinct from absent inheritance")
	require.Equal(t, 6, repo.calls, "each admission reads once; process TTL/singleflight is reserved for legacy callers")
	require.Equal(t, 0.2, a.Resolve(context.Background(), 101, 202, 9), "fresh reads do not publish back into the legacy TTL cache")
}

func TestUserGroupRateFreshSnapshotRejectsDependencyFailureDespiteCachedPrice(t *testing.T) {
	stored := 0.2
	repo := &userGroupRateResolverRepoStub{rate: &stored}
	resolver := newUserGroupRateResolver(repo, nil, time.Hour, nil, "service.test")
	require.Equal(t, 0.2, resolver.Resolve(context.Background(), 101, 202, 1))
	repo.err = errors.New("personal rate store unavailable")
	rate, personal, err := resolver.ResolveFreshSnapshot(context.Background(), 101, 202, 1)
	require.ErrorIs(t, err, repo.err)
	require.False(t, personal)
	require.Equal(t, 1.0, rate)
	require.Equal(t, 2, repo.calls)
}

func TestBillingAdmissionSnapshotReadsPersonalEditsWithoutWaitingForTTL(t *testing.T) {
	stored := 0.2
	repo := &userGroupRateResolverRepoStub{rate: &stored}
	resolver := newUserGroupRateResolver(repo, nil, time.Hour, nil, "service.test")
	require.Equal(t, 0.2, resolver.Resolve(context.Background(), 101, 202, 1))
	groupID := int64(202)
	key := &APIKey{User: &User{ID: 101}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}}
	for _, gateway := range []struct {
		name     string
		snapshot func(context.Context, *APIKey, time.Time, ...string) context.Context
	}{
		{"gateway", (&GatewayService{userGroupRateResolver: resolver}).WithBillingPricingSnapshot},
		{"openai", (&OpenAIGatewayService{userGroupRateResolver: resolver}).WithBillingPricingSnapshot},
	} {
		t.Run(gateway.name, func(t *testing.T) {
			updated := 0.7
			repo.rate, repo.err = &updated, nil
			admitted := gateway.snapshot(context.Background(), key, time.Now(), "public-model")
			require.NoError(t, ValidateBillingPricingSnapshot(admitted))
			require.Equal(t, 0.7, BillingPricingSnapshotFromContext(admitted).quote(PlatformOpenAI, "public-model", "token").EffectiveMultiplier)
			repo.rate = nil
			inherited := gateway.snapshot(context.Background(), key, time.Now(), "public-model")
			require.NoError(t, ValidateBillingPricingSnapshot(inherited))
			require.Equal(t, 1.0, BillingPricingSnapshotFromContext(inherited).quote(PlatformOpenAI, "public-model", "token").EffectiveMultiplier)
			free := 0.0
			repo.rate = &free
			gratis := gateway.snapshot(context.Background(), key, time.Now(), "public-model")
			require.NoError(t, ValidateBillingPricingSnapshot(gratis))
			require.Zero(t, BillingPricingSnapshotFromContext(gratis).quote(PlatformOpenAI, "public-model", "token").EffectiveMultiplier)
			repo.err = errors.New("personal rate store unavailable")
			unavailable := gateway.snapshot(context.Background(), key, time.Now(), "public-model")
			require.Error(t, ValidateBillingPricingSnapshot(unavailable), "a cached price cannot hide an unknown newly admitted price")
			require.Equal(t, 0.7, BillingPricingSnapshotFromContext(admitted).quote(PlatformOpenAI, "public-model", "token").EffectiveMultiplier, "in-flight work retains the price it actually admitted")
		})
	}
}
