//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

type aggregateRefreshKeys struct {
	APIKeyRepository
	key *APIKey
}

func (r *aggregateRefreshKeys) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	copy := *r.key
	return &copy, nil
}

func TestAggregateRefreshPinsGroupAndUsesCurrentPolicy(t *testing.T) {
	for _, change := range []string{"price", "revoked", "removed-member", "disabled-key", "expired-key", "quota", "subscription", "platform"} {
		t.Run(change, func(t *testing.T) {
			svc, users, groups := aggregateTestService()
			key := &APIKey{ID: 11, UserID: 9, Key: "refresh-fixture", Status: StatusActive, User: users.user, GroupBindingMode: GroupBindingCustom, AggregateGroupIDs: []int64{1, 2}}
			repo := &aggregateRefreshKeys{key: key}
			svc.apiKeyRepo = repo
			scope, err := svc.AggregateScope(t.Context(), key)
			require.NoError(t, err)
			conn, err := svc.BindAggregateGroup(t.Context(), scope, "shared", APIProtocolResponses, 1)
			require.NoError(t, err)
			switch change {
			case "price":
				groups.groups[1].RateMultiplier = 2.5
			case "revoked":
				users.user.AllowedGroups = []int64{2}
			case "removed-member":
				key.AggregateGroupIDs = []int64{2}
			case "disabled-key":
				key.Status = StatusDisabled
			case "expired-key":
				expired := time.Now().Add(-time.Minute)
				key.ExpiresAt = &expired
			case "quota":
				key.Quota, key.QuotaUsed = 1, 1
			case "subscription":
				groups.groups[1].SubscriptionType = SubscriptionTypeSubscription
			case "platform":
				groups.groups[1].Platform = PlatformAnthropic
			}
			fresh, err := svc.RefreshAggregateBinding(t.Context(), conn, "shared", APIProtocolResponses)
			if change == "price" {
				require.NoError(t, err)
				require.EqualValues(t, 1, *fresh.GroupID)
				require.Equal(t, 2.5, fresh.Group.RateMultiplier)
				require.Zero(t, conn.Group.RateMultiplier, "an accepted turn keeps its own price snapshot")
			} else {
				require.Error(t, err, "live connections may not retain revoked access or switch to group 2")
			}
		})
	}
}

func TestAggregateResourcesAndFallbackRemainInAuthorizedGroup(t *testing.T) {
	svc, _, groups := aggregateTestService()
	foreign := int64(99)
	groups.groups[1].FallbackGroupID = &foreign
	groups.groups[1].FallbackGroupIDOnInvalidRequest = &foreign
	groups.groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"shared"}}
	key, err := svc.AggregateScope(t.Context(), &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	bound, err := svc.BindAggregateGroup(t.Context(), key, "", "", 1)
	require.NoError(t, err, "resource reads have no model and must reach their existing ownership checks")
	require.EqualValues(t, 1, *bound.GroupID)
	require.Nil(t, bound.Group.FallbackGroupID)
	require.Nil(t, bound.Group.FallbackGroupIDOnInvalidRequest)
	require.NotNil(t, groups.groups[1].FallbackGroupID, "ordinary keys retain the configured fallback")
}

type aggregateTestChannels struct {
	ChannelRepository
	channels []Channel
}

func (r aggregateTestChannels) ListAll(context.Context) ([]Channel, error) { return r.channels, nil }

func TestAggregateChannelAliasesAndRestrictions(t *testing.T) {
	svc, _, _ := aggregateTestService()
	price := 1.0
	svc.aggregateChannels = aggregateTestChannels{channels: []Channel{{
		ID: 20, Status: StatusActive, GroupIDs: []int64{1, 2}, RestrictModels: true,
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"public-alias": "shared"}},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"public-alias"}, InputPrice: &price}},
	}}}
	key, err := svc.AggregateScope(t.Context(), &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	bound, err := svc.BindAggregateGroup(t.Context(), key, "public-alias", APIProtocolResponses, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, *bound.GroupID)
	models, err := svc.AggregateModels(t.Context(), key, "")
	require.NoError(t, err)
	require.Equal(t, []string{"public-alias"}, models)
	_, err = svc.BindAggregateGroup(t.Context(), key, "shared", APIProtocolResponses, 0)
	require.Error(t, err, "unpriced internal aliases must not escape channel restrictions")
}

func TestAggregateGeminiNativeMixedPool(t *testing.T) {
	svc, users, groups := aggregateTestService()
	users.user.AllowedGroups = []int64{3}
	groups.groups[2].Platform = PlatformGemini
	accounts := svc.aggregateAccounts.(*aggregateTestAccounts)
	account := Account{ID: 30, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-mixed": "gemini-3.8-flash"}}, Extra: map[string]any{"mixed_scheduling": true}}
	accounts.byGroup[3] = []Account{account}
	key, err := svc.AggregateScope(t.Context(), &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	bound, err := svc.BindAggregateGroup(t.Context(), key, "gemini-mixed", "gemini", 0)
	require.NoError(t, err, "native Gemini keeps its existing mixed Antigravity pool")
	require.EqualValues(t, 3, *bound.GroupID)
	_, err = svc.BindAggregateGroup(t.Context(), key, "gemini-mixed", APIProtocolAnthropic, 0)
	require.Error(t, err, "mixed membership cannot enable a protocol bridge")
	account.Extra["mixed_scheduling"] = false
	accounts.byGroup[3] = []Account{account}
	_, err = svc.BindAggregateGroup(t.Context(), key, "gemini-mixed", "gemini", 0)
	require.Error(t, err, "mixed scheduling requires explicit account consent")
}

func TestAggregateForcedPlatformRestrictsDiscoveryScope(t *testing.T) {
	svc, users, _ := aggregateTestService()
	users.user.AllowedGroups = []int64{1, 2, 3}
	ctx := context.WithValue(t.Context(), ctxkey.ForcePlatform, PlatformAnthropic)
	key, err := svc.AggregateScope(ctx, &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	require.Len(t, key.AggregateGroups, 1)
	require.EqualValues(t, 3, key.AggregateGroups[0].ID)
	models, err := svc.AggregateModels(ctx, key, "")
	require.NoError(t, err)
	require.Empty(t, models, "forced discovery must not list OpenAI models from other groups")
}

// Requirements: docs/AGGREGATE_API_KEYS.md. Expectations below are literal
// grants, membership, endpoints and group IDs, independent of resolver output.
type aggregateTestGroups struct {
	GroupRepository
	groups []Group
}

func (r *aggregateTestGroups) ListActive(context.Context) ([]Group, error) { return r.groups, nil }
func (r *aggregateTestGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	for i := range r.groups {
		if r.groups[i].ID == id {
			copy := r.groups[i]
			return &copy, nil
		}
	}
	return nil, ErrGroupNotFound
}

type aggregateTestAccounts struct {
	AccountRepository
	byGroup map[int64][]Account
}

func (r *aggregateTestAccounts) ListModelAvailabilityCandidates(_ context.Context, id *int64, _ []string, _ bool) ([]Account, error) {
	return r.byGroup[*id], nil
}

func aggregateTestService() (*APIKeyService, *visibilityUserRepo, *aggregateTestGroups) {
	user := &visibilityUserRepo{user: &User{ID: 9, Status: StatusActive, Balance: 100, AllowedGroups: []int64{1, 2}, RestrictPublicGroups: true}}
	groups := &aggregateTestGroups{groups: []Group{
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SortOrder: 5},
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SortOrder: 5},
		{ID: 3, Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SortOrder: -1},
		{ID: 4, Platform: PlatformComposite, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard},
		{ID: 5, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 6, Platform: PlatformOpenAI, Status: "inactive", SubscriptionType: SubscriptionTypeStandard},
	}}
	accounts := &aggregateTestAccounts{byGroup: map[int64][]Account{}}
	for _, id := range []int64{1, 2} {
		accounts.byGroup[id] = []Account{{ID: id * 10, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"shared": "gpt-5.4"}}}}
	}
	return &APIKeyService{userRepo: user, groupRepo: groups, aggregateAccounts: accounts}, user, groups
}

func TestAggregateAllDynamicAndCustomFixedScope(t *testing.T) {
	svc, user, groups := aggregateTestService()
	key := &APIKey{ID: 11, UserID: 9, GroupBindingMode: GroupBindingAll, User: &User{ID: 9, AllowedGroups: []int64{99}}}
	ctx := context.Background()
	scope, err := svc.AggregateScope(ctx, key)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, []int64{scope.AggregateGroups[0].ID, scope.AggregateGroups[1].ID})
	require.Nil(t, key.Group)
	require.Empty(t, key.AggregateGroups, "auth snapshot must remain immutable")
	// Even authorized Composite, subscription and disabled groups never enter.
	user.user.AllowedGroups = []int64{1, 2, 3, 4, 5, 6}
	scope, err = svc.AggregateScope(ctx, key)
	require.NoError(t, err)
	require.Len(t, scope.AggregateGroups, 3)
	require.EqualValues(t, 3, scope.AggregateGroups[0].ID)
	custom := *key
	custom.GroupBindingMode = GroupBindingCustom
	custom.AggregateGroupIDs = []int64{2}
	scope, err = svc.AggregateScope(ctx, &custom)
	require.NoError(t, err)
	require.Len(t, scope.AggregateGroups, 1)
	require.EqualValues(t, 2, scope.AggregateGroups[0].ID)
	user.user.AllowedGroups = []int64{1}
	scope, err = svc.AggregateScope(ctx, &custom)
	require.NoError(t, err)
	require.Empty(t, scope.AggregateGroups, "revoked membership cannot survive the cached key")
	groups.groups[1].SubscriptionType = SubscriptionTypeSubscription
	scope, err = svc.AggregateScope(ctx, key)
	require.NoError(t, err)
	require.Empty(t, scope.AggregateGroups, "type changes remove balance candidates immediately")
	require.Equal(t, []int64{2}, custom.AggregateGroupIDs, "custom configuration is never silently rewritten")
}

func TestAggregateNativeSelectionAndUnion(t *testing.T) {
	svc, _, _ := aggregateTestService()
	ctx := context.Background()
	key := &APIKey{ID: 11, UserID: 9, GroupBindingMode: GroupBindingAll, Quota: 40, QuotaUsed: 7, Usage5h: 3}
	scope, err := svc.AggregateScope(ctx, key)
	require.NoError(t, err)
	bound, err := svc.BindAggregateGroup(ctx, scope, "shared", APIProtocolResponses, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, *bound.GroupID, "SortOrder ties use group ID")
	require.Nil(t, key.GroupID)
	require.Equal(t, 40.0, bound.Quota)
	require.Equal(t, 7.0, bound.QuotaUsed)
	require.Equal(t, 3.0, bound.Usage5h)
	accounts := svc.aggregateAccounts.(*aggregateTestAccounts)
	accounts.byGroup[1][0].Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}
	bound, err = svc.BindAggregateGroup(ctx, scope, "shared", APIProtocolResponses, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, *bound.GroupID, "native Responses skips a Chat-only credential")
	_, err = svc.BindAggregateGroup(ctx, scope, "shared", APIProtocolAnthropic, 0)
	require.Error(t, err, "aggregate mode must not bridge native Messages to OpenAI")
	_, err = svc.BindAggregateGroup(ctx, scope, "unknown-alias", APIProtocolResponses, 0)
	require.Error(t, err)
	_, err = svc.BindAggregateGroup(ctx, scope, "shared", APIProtocolResponses, 99)
	require.Error(t, err, "hint cannot escape authorized scope")
	models, err := svc.AggregateModels(ctx, scope, "")
	require.NoError(t, err)
	require.Equal(t, []string{"shared"}, models)
	scope.AggregateGroups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"other"}}
	_, err = svc.BindAggregateGroup(ctx, scope, "shared", APIProtocolResponses, 0)
	require.Error(t, err, "allowlist applies before selecting a group")
	models, err = svc.AggregateModels(ctx, scope, APIProtocolResponses)
	require.NoError(t, err)
	require.Empty(t, models)
}

func TestAggregateBindingValidationAndLegacyIsolation(t *testing.T) {
	svc, user, _ := aggregateTestService()
	ctx := context.Background()
	for _, test := range []struct {
		mode             string
		ids              []int64
		group            *int64
		conversion       bool
		client, endpoint string
	}{
		{"invalid", nil, nil, false, "", ""},
		{GroupBindingAll, []int64{1}, nil, false, "", ""},
		{GroupBindingCustom, nil, nil, false, "", ""},
		{GroupBindingCustom, []int64{1, 1}, nil, false, "", ""},
		{GroupBindingCustom, []int64{-1}, nil, false, "", ""},
		{GroupBindingCustom, []int64{3}, nil, false, "", ""},
		{GroupBindingCustom, []int64{4}, nil, false, "", ""},
		{GroupBindingCustom, []int64{5}, nil, false, "", ""},
		{GroupBindingCustom, []int64{6}, nil, false, "", ""},
		{GroupBindingAll, nil, nil, true, "custom", "/v1/responses"},
		{GroupBindingAll, nil, nil, false, "custom", ""},
	} {
		require.Error(t, svc.validateGroupBinding(ctx, user.user, test.mode, test.ids, test.group, test.conversion, test.client, test.endpoint))
	}
	require.NoError(t, svc.validateGroupBinding(ctx, user.user, GroupBindingAll, nil, nil, false, "", ""))
	require.NoError(t, svc.validateGroupBinding(ctx, user.user, GroupBindingCustom, []int64{1, 2}, nil, false, "", ""))
	for _, legacy := range []*APIKey{{}, {GroupBindingMode: GroupBindingSingle}, {Group: &Group{Platform: PlatformComposite}, ConversionEnabled: true}} {
		got, err := (&APIKeyService{}).AggregateScope(ctx, legacy)
		require.NoError(t, err)
		require.Same(t, legacy, got)
	}
}

func TestAggregateScopeUpdatePreservesCountersAndSnapshot(t *testing.T) {
	svc, user, groups := aggregateTestService()
	repo := &updateFieldsAPIKeyRepoStub{key: &APIKey{ID: 11, UserID: 9, Key: "fixture", Status: StatusActive, GroupBindingMode: GroupBindingAll, QuotaUsed: 17, Usage5h: 4, User: user.user}}
	svc.apiKeyRepo = repo
	mode, ids := GroupBindingCustom, []int64{2}
	updated, err := svc.Update(context.Background(), 11, 9, UpdateAPIKeyRequest{GroupBindingMode: &mode, AggregateGroupIDs: &ids})
	require.NoError(t, err)
	require.Equal(t, []APIKeyUpdateFields{{GroupBinding: true}}, repo.updateFields)
	require.Equal(t, 17.0, updated.QuotaUsed)
	require.Equal(t, 4.0, updated.Usage5h)
	snapshot := svc.snapshotFromAPIKey(context.Background(), updated)
	roundTrip := svc.snapshotToAPIKey("fixture", snapshot)
	require.Equal(t, GroupBindingCustom, roundTrip.GroupBindingMode)
	require.Equal(t, []int64{2}, roundTrip.AggregateGroupIDs)
	scope, err := svc.AggregateScope(context.Background(), roundTrip)
	require.NoError(t, err)
	require.Len(t, scope.AggregateGroups, 1)
	groups.groups[0].Status = "inactive"
	scope, err = svc.AggregateScope(context.Background(), roundTrip)
	require.NoError(t, err)
	require.Empty(t, scope.AggregateGroups)
	single := GroupBindingSingle
	_, err = svc.Update(context.Background(), 11, 9, UpdateAPIKeyRequest{GroupBindingMode: &single})
	require.Error(t, err)
	require.Len(t, repo.updateFields, 1, "invalid mode change must not persist")
}
