//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// V25: canonical affinity must preserve request-level model authorization.
// An allowed mapped upstream model cannot authorize a forbidden public model.
func TestCodexCanonicalChannelRestrictionRegression(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	store := newFakeOpenAISessionRouteStore()
	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:7", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
	require.NoError(t, err)
	channel := Channel{
		ID: 31, Status: StatusActive, GroupIDs: []int64{7}, RestrictModels: true,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.2"}}},
	}
	var acquired []int64
	svc := &OpenAIGatewayService{
		sessionRouteStore: store,
		cache:             &schedulerTestGatewayCache{},
		channelService:    newTestChannelService(makeStandardRepo(channel, map[int64]string{7: PlatformOpenAI})),
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{7},
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.1": "gpt-5.2"}},
		}}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}),
	}
	selected, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	if selected != nil && selected.ReleaseFunc != nil {
		defer selected.ReleaseFunc()
	}
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.ErrorContains(t, err, "channel pricing restriction")
	require.Nil(t, selected)
	require.Empty(t, acquired, "request authorization precedes account concurrency")
	route, err := store.Get(ctx, "group:7", OpenAISessionRouteDigest("session"))
	require.NoError(t, err)
	require.EqualValues(t, 101, route.OwnerAccountID)
	require.EqualValues(t, 1, route.Generation)
}

// V25: a persisted session binding cannot grant another user's shared OAuth
// response. API-key namespaces and explicit full-history rebuilding retain
// their existing independent authorization semantics.
func TestCodexCanonicalSharedResponseAuthorizationRegression(t *testing.T) {
	for _, scenario := range []string{"own-oauth", "foreign-oauth", "foreign-apikey", "movable-rebuild"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := oauthSessionPolicyContext(9001)
			groupID := int64(11)
			account := newOpenAIOAuthSessionPolicyAccount(101, groupID)
			if scenario == "foreign-apikey" {
				account.Type = AccountTypeAPIKey
			}
			store := newFakeOpenAISessionRouteStore()
			_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:11", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
			require.NoError(t, err)
			cache := &oauthSessionPolicyCache{values: make(map[string]int64)}
			var released []int64
			svc := &OpenAIGatewayService{
				sessionRouteStore: store, cache: cache,
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{releasedIDs: &released}),
			}
			if scenario == "own-oauth" {
				require.NoError(t, svc.bindOpenAIOAuthSharedResponseAccount(ctx, &account, "resp_shared", time.Hour))
			} else {
				cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, openAIOAuthSharedResponseOwnerCacheKey("resp_shared"))] = 9002
			}
			selected, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "resp_shared", "session", "gpt-5.1", nil,
				OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, scenario == "movable-rebuild", false)
			if selected != nil && selected.ReleaseFunc != nil {
				defer selected.ReleaseFunc()
			}
			if scenario == "foreign-oauth" {
				require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
				require.Nil(t, selected)
				require.Equal(t, []int64{101}, released, "denied continuation releases its acquired slot")
			} else {
				require.NoError(t, err)
				require.NotNil(t, selected)
				require.EqualValues(t, 101, selected.Account.ID)
			}
			route, err := store.Get(ctx, "group:11", OpenAISessionRouteDigest("session"))
			require.NoError(t, err)
			require.EqualValues(t, 101, route.OwnerAccountID)
			require.EqualValues(t, 1, route.Generation)
		})
	}
}
