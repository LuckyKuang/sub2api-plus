//go:build unit

package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type handlerRequestRPMCache struct {
	mu          sync.Mutex
	userCounts  map[int64]int
	groupCounts map[[2]int64]int
	userCalls   int
	groupCalls  int
}

func (s *handlerRequestRPMCache) IncrementUserRPM(_ context.Context, userID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userCounts == nil {
		s.userCounts = make(map[int64]int)
	}
	s.userCounts[userID]++
	s.userCalls++
	return s.userCounts[userID], nil
}

func (s *handlerRequestRPMCache) IncrementUserGroupRPM(_ context.Context, userID, groupID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupCounts == nil {
		s.groupCounts = make(map[[2]int64]int)
	}
	key := [2]int64{userID, groupID}
	s.groupCounts[key]++
	s.groupCalls++
	return s.groupCounts[key], nil
}

func (s *handlerRequestRPMCache) GetUserRPM(_ context.Context, userID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userCounts[userID], nil
}

func (s *handlerRequestRPMCache) GetUserGroupRPM(_ context.Context, userID, groupID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groupCounts[[2]int64{userID, groupID}], nil
}

type fallbackRPMOverrideRepo struct {
	service.UserGroupRateRepository
	groups []int64
}

func (r *fallbackRPMOverrideRepo) GetRPMOverrideByUserAndGroup(_ context.Context, _, groupID int64) (*int, error) {
	r.groups = append(r.groups, groupID)
	limit := 1
	return &limit, nil
}

func TestGatewayPromptFallbackRPMCountsRequestOnceAndChecksFallbackOverride(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback allowed", true: "fallback exhausted"}[exhausted], func(t *testing.T) {
			f := newPromptFallbackPricingFixture(t, "")
			zero := 0
			f.key.User.RPMLimit = 1
			f.key.User.UserGroupRPMOverride = &zero
			cache := &handlerRequestRPMCache{groupCounts: make(map[[2]int64]int)}
			if exhausted {
				cache.groupCounts[[2]int64{f.key.UserID, promptPricingFallbackGroup}] = 1
			}
			repo := &fallbackRPMOverrideRepo{}
			billing := service.NewBillingCacheService(f.cache, nil, nil, nil, cache, repo, f.handler.cfg, nil)
			t.Cleanup(billing.Stop)
			f.handler.billingCacheService = billing

			response := f.send()
			if exhausted {
				require.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
				require.Len(t, f.wire.calls, 1, "the exhausted fallback group cannot send another upstream request")
				require.Empty(t, f.ledger.commands)
				require.Empty(t, f.usage.logs)
			} else {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.Len(t, f.wire.calls, 2, "a single external request cannot reject its own fallback at user RPM=1")
				require.Len(t, f.ledger.commands, 1)
			}
			require.Equal(t, []int64{promptPricingFallbackGroup}, repo.groups, "the primary group's cached exemption must not apply to a different group")
			require.Equal(t, 1, cache.userCalls)
			require.Equal(t, 1, cache.groupCalls)
			require.Same(t, &zero, f.key.User.UserGroupRPMOverride, "fallback admission cannot mutate the authenticated user")
			require.Zero(t, f.cache.count(), "the admitted balance reservation is released on either outcome")
		})
	}
}

func TestCloneAPIKeyWithGroupScopesUserRPMOverrideToItsGroup(t *testing.T) {
	id, override := int64(10), 0
	user := &service.User{ID: 20, RPMLimit: 5, UserGroupRPMOverride: &override}
	key := &service.APIKey{ID: 30, GroupID: &id, Group: &service.Group{ID: id}, User: user}
	other := cloneAPIKeyWithGroup(key, &service.Group{ID: 11})
	require.NotSame(t, key, other)
	require.NotSame(t, user, other.User)
	require.Nil(t, other.User.UserGroupRPMOverride, "a different group must resolve its own override")
	require.Equal(t, 5, other.User.RPMLimit, "the user's global RPM policy remains in force")
	require.Same(t, &override, user.UserGroupRPMOverride)
	require.Equal(t, id, *key.GroupID)
	same := cloneAPIKeyWithGroup(key, &service.Group{ID: id})
	require.Same(t, &override, same.User.UserGroupRPMOverride, "same-group snapshots retain the matching auth-cache override")
}
