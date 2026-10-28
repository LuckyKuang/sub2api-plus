//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRPMAdmissionScopeCountsExternalRequestOnceAcrossFallbacks(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2}, userGroupCounts: []int{1, 1, 2}}
	svc := newBillingServiceForRPM(t, cache, nil)
	user := &User{ID: 1, RPMLimit: 1}
	primary, fallback := &Group{ID: 10, RPMLimit: 1}, &Group{ID: 11, RPMLimit: 1}
	ctx := WithRPMAdmissionScope(context.Background())
	require.Same(t, ctx, WithRPMAdmissionScope(ctx), "nested admission must preserve the external request scope")

	require.NoError(t, svc.CheckRequestRPM(ctx, user, primary))
	require.NoError(t, svc.checkRPM(ctx, user, primary), "later billing eligibility must not count the accepted request again")
	require.NoError(t, svc.checkRPM(ctx, user, fallback), "a fallback is the same user request and a new group admission")
	require.NoError(t, svc.checkRPM(ctx, user, fallback))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userGroupCalls))

	require.ErrorIs(t, svc.CheckRequestRPM(WithNewRPMAdmissionScope(ctx), user, nil), ErrUserRPMExceeded,
		"a new request or WebSocket turn gets its own global RPM admission")
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userCalls))
}

func TestRPMAdmissionScopeConcurrentChecksIncrementOnce(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, nil)
	ctx := WithRPMAdmissionScope(context.Background())
	user, group := &User{ID: 2, RPMLimit: 1}, &Group{ID: 20, RPMLimit: 1}
	const callers = 32
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- svc.CheckRequestRPM(ctx, user, group)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestRPMAdmissionScopeKeepsCountsAndComparesCurrentLimits(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{3}}
	svc := newBillingServiceForRPM(t, cache, nil)
	ctx := WithRPMAdmissionScope(context.Background())
	user := &User{ID: 3, RPMLimit: 2}
	require.ErrorIs(t, svc.CheckRequestRPM(ctx, user, nil), ErrUserRPMExceeded)
	require.ErrorIs(t, svc.CheckRequestRPM(ctx, user, nil), ErrUserRPMExceeded, "rechecking a rejected request cannot erase the rejection")
	updated := *user
	updated.RPMLimit = 3
	require.NoError(t, svc.CheckRequestRPM(ctx, &updated, nil), "a refreshed limit compares against the actual admission count")
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))
}

func TestRPMAdmissionScopeSeparatesUsersAndServices(t *testing.T) {
	cache := &userRPMCacheStub{}
	first := newBillingServiceForRPM(t, cache, nil)
	second := newBillingServiceForRPM(t, cache, nil)
	ctx := WithRPMAdmissionScope(context.Background())
	group := &Group{ID: 30, RPMLimit: 10}
	require.NoError(t, first.CheckRequestRPM(ctx, &User{ID: 3, RPMLimit: 10}, group))
	require.NoError(t, first.CheckRequestRPM(ctx, &User{ID: 4, RPMLimit: 10}, group))
	require.NoError(t, second.CheckRequestRPM(ctx, &User{ID: 3, RPMLimit: 10}, group))
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestRPMAdmissionScopeCachesFailedIncrementWithoutRetrying(t *testing.T) {
	cache := &userRPMCacheStub{userErr: errors.New("redis unavailable"), userGroupErr: errors.New("redis unavailable")}
	svc := newBillingServiceForRPM(t, cache, nil)
	ctx := WithRPMAdmissionScope(context.Background())
	user, group := &User{ID: 5, RPMLimit: 1}, &Group{ID: 50, RPMLimit: 1}
	for i := 0; i < 3; i++ {
		require.NoError(t, svc.CheckRequestRPM(ctx, user, group), "the existing Redis fail-open policy is preserved")
	}
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestRequestRPMRetainsSimpleModeBypass(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, nil)
	svc.cfg.RunMode = config.RunModeSimple
	require.NoError(t, svc.CheckRequestRPM(WithRPMAdmissionScope(context.Background()), &User{ID: 6, RPMLimit: 1}, &Group{ID: 60, RPMLimit: 1}))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
}
