//go:build unit

package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type wsRPMOverrideRepo struct {
	service.UserGroupRateRepository
	mu       sync.Mutex
	override *int
}

func (s *wsRPMOverrideRepo) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.override == nil {
		return nil, nil
	}
	value := *s.override
	return &value, nil
}

func TestOpenAIResponsesWebSocketRPMRefreshAndTurnAdmission(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, scenario := range []string{"global policy reduced", "cached group exemption removed", "new turn counts independently"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				repo := newWSGroupPricingAPIKeyRepoStub(1)
				zero := 0
				rpmOverride := &wsRPMOverrideRepo{}
				if scenario == "cached group exemption removed" {
					rpmOverride.override = &zero
					repo.group.RPMLimit = 1
				} else {
					repo.apiKey.User.RPMLimit = 100
					if scenario == "new turn counts independently" {
						repo.apiKey.User.RPMLimit = 1
					}
				}
				apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, rpmOverride, nil, &config.Config{})
				cache := &handlerRequestRPMCache{groupCounts: make(map[[2]int64]int)}
				if scenario == "cached group exemption removed" {
					cache.groupCounts[[2]int64{1701, wsGroupPricingGroupID}] = 1
				}
				runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload:     wsGroupPricingFirst,
					secondPayload:    wsGroupPricingSecond,
					ingressMode:      mode,
					apiKeyService:    apiKeyService,
					apiKeyCredential: wsGroupPricingKey,
					rpmCache:         cache,
					rpmOverrideRepo:  rpmOverride,
					afterFirstUpstreamRequest: func(*service.ChannelService) error {
						return repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) {
							if scenario == "global policy reduced" {
								key.User.RPMLimit = 1
							}
							if scenario == "cached group exemption removed" {
								rpmOverride.mu.Lock()
								rpmOverride.override = nil
								rpmOverride.mu.Unlock()
								key.User.UserGroupRPMOverride = nil
							}
						})
					},
					secondTurnCloseExpected: true,
					closeReason:             "billing check failed",
				})
				cache.mu.Lock()
				defer cache.mu.Unlock()
				if scenario == "cached group exemption removed" {
					require.Equal(t, 1, cache.groupCalls, "the removed exemption must be rechecked before another upstream write")
				} else {
					require.Equal(t, 2, cache.userCalls, "each response.create is one independent user request")
				}
			})
		}
	}
}

func TestOpenAIResponsesWebSocketRPMKeyMoveStillEnforcesGlobalUserLimit(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			repo.apiKey.User.RPMLimit = 100
			apiKeyService := newWSGroupPricingAPIKeyService(repo)
			cache := &handlerRequestRPMCache{}
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:     wsGroupPricingFirst,
				secondPayload:    wsGroupPricingSecond,
				ingressMode:      mode,
				apiKeyService:    apiKeyService,
				apiKeyCredential: wsGroupPricingKey,
				rpmCache:         cache,
				afterFirstUpstreamRequest: func(*service.ChannelService) error {
					return repo.adminUpdate(apiKeyService, func(key *service.APIKey, group *service.Group) {
						other := wsGroupPricingGroupID + 1
						key.GroupID = &other
						group.ID = other
						group.RateMultiplier = 99
						key.User.RPMLimit = 1
					})
				},
				secondTurnCloseExpected:          true,
				closeReason:                      "billing check failed",
				collectCompletedUsageOnRejection: true,
			})
			require.Len(t, got.upstreamPayloads, 1, "the reduced global limit blocks the replacement turn before an upstream write")
			require.Len(t, got.logs, 1, "only the completed original turn produces usage")
			require.NotNil(t, got.log.GroupID)
			require.Equal(t, wsGroupPricingGroupID, *got.log.GroupID)
			require.Equal(t, 3.0, got.log.RateMultiplier, "moving the key cannot rewrite already accepted usage to the new group's 99x rate")
			require.Greater(t, got.log.TotalCost, 0.0)
			require.InDelta(t, got.log.TotalCost*3, got.log.ActualCost, 1e-12, "usage retains the first turn's accepted original-group selling rate")
			cache.mu.Lock()
			defer cache.mu.Unlock()
			require.Equal(t, 2, cache.userCalls, "moving a key cannot freeze its user's former global RPM ceiling")
		})
	}
}

func TestRefreshOpenAIWSTurnRPMDoesNotDependOnBillingGroupCompatibility(t *testing.T) {
	for _, change := range []string{"moved group", "removed group", "platform changed", "subscription changed", "missing group snapshot"} {
		t.Run(change, func(t *testing.T) {
			oldGroupID, oldOverride, foreignOverride := int64(77), 0, 999
			conn := &service.APIKey{
				ID: 5, UserID: 9, Key: "sk-same-owner", GroupID: &oldGroupID,
				User:  &service.User{ID: 9, Balance: 12, RPMLimit: 100, UserGroupRPMOverride: &oldOverride},
				Group: &service.Group{ID: oldGroupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 3},
			}
			latestGroupID := oldGroupID
			latest := &service.APIKey{
				ID: 5, UserID: 9, Key: conn.Key, GroupID: &latestGroupID,
				User:  &service.User{ID: 9, Balance: 123, RPMLimit: 1, UserGroupRPMOverride: &foreignOverride},
				Group: &service.Group{ID: oldGroupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 99},
			}
			switch change {
			case "moved group":
				latestGroupID++
				latest.Group.ID = latestGroupID
			case "removed group":
				latest.GroupID = nil
				latest.Group = nil
			case "platform changed":
				latest.Group.Platform = service.PlatformAnthropic
			case "subscription changed":
				latest.Group.SubscriptionType = service.SubscriptionTypeSubscription
			case "missing group snapshot":
				latest.Group = nil
			}
			var turns openAIWSTurnBillingAPIKeys
			turns.set(1, conn)
			turn2 := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, nil }), conn)
			turns.set(2, turn2)
			require.NotSame(t, conn, turn2)
			require.NotSame(t, conn.User, turn2.User)
			require.Equal(t, 1, turn2.User.RPMLimit, "global user admission is independent of the connection's frozen billing identity")
			require.Nil(t, turn2.User.UserGroupRPMOverride, "an incompatible auth group cannot donate its override to the original group")
			require.Same(t, conn.Group, turn2.Group)
			require.Equal(t, oldGroupID, *turn2.GroupID)
			require.Equal(t, 12.0, turn2.User.Balance)
			history := turns.forTurn(1, conn)
			require.Same(t, conn, history)
			require.Equal(t, 100, history.User.RPMLimit)
			require.Equal(t, 3.0, history.Group.RateMultiplier, "already accepted usage retains the original group price")
			require.Same(t, &oldOverride, history.User.UserGroupRPMOverride)
		})
	}
}

func TestRefreshOpenAIWSTurnRPMWithoutBillingGroup(t *testing.T) {
	conn := &service.APIKey{ID: 5, UserID: 9, Key: "sk-ungrouped", User: &service.User{ID: 9, RPMLimit: 100}}
	latest := &service.APIKey{ID: 5, UserID: 9, Key: conn.Key, User: &service.User{ID: 9, RPMLimit: 1}}
	got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, nil }), conn)
	require.Equal(t, 1, got.User.RPMLimit)
	require.Nil(t, got.GroupID)
	require.Nil(t, got.Group)
	require.Equal(t, 100, conn.User.RPMLimit)
}

func TestRefreshOpenAIWSTurnRPMKeyMoveQueriesOriginalGroupOverride(t *testing.T) {
	oldGroupID, newGroupID, exempt := int64(77), int64(78), 0
	conn := &service.APIKey{
		ID: 5, UserID: 9, Key: "sk-group-moved", GroupID: &oldGroupID,
		User:  &service.User{ID: 9, RPMLimit: 100, UserGroupRPMOverride: &exempt},
		Group: &service.Group{ID: oldGroupID, Platform: service.PlatformOpenAI},
	}
	latest := &service.APIKey{
		ID: 5, UserID: 9, Key: conn.Key, GroupID: &newGroupID,
		User:  &service.User{ID: 9, RPMLimit: 100, UserGroupRPMOverride: &exempt},
		Group: &service.Group{ID: newGroupID, Platform: service.PlatformOpenAI},
	}
	turn := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, nil }), conn)
	cache := &handlerRequestRPMCache{groupCounts: map[[2]int64]int{{9, oldGroupID}: 1}}
	overrides := &fallbackRPMOverrideRepo{}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, cache, overrides, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	err := billing.CheckRequestRPM(service.WithNewRPMAdmissionScope(context.Background()), turn.User, turn.Group)
	require.ErrorIs(t, err, service.ErrGroupRPMExceeded, "neither group's cached exemption can bypass the original group's current override")
	require.Equal(t, []int64{oldGroupID}, overrides.groups)
	require.Equal(t, 1, cache.groupCalls)
	require.Zero(t, cache.userCalls, "an original-group rejection ends this admission before the global counter")
}
