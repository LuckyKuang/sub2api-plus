//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

const platformFallbackAdmissionGroupID int64 = 71

func platformFallbackAdmissionChannels(platform string, billing *BillingService) *ChannelService {
	cache := populateChannelCache([]Channel{{
		ID:                 3,
		Status:             StatusActive,
		GroupIDs:           []int64{platformFallbackAdmissionGroupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceRequested,
	}}, map[int64]string{platformFallbackAdmissionGroupID: platform})
	return &ChannelService{frozenCache: cache, billingService: billing}
}

func platformFallbackAdmissionGroup() *Group {
	return &Group{
		ID:             platformFallbackAdmissionGroupID,
		Platform:       PlatformOpenAI,
		Status:         StatusActive,
		Hydrated:       true,
		RateMultiplier: 1,
	}
}

func TestPlatformPricingFallbackAdmission_SystemSameSKUBypassesMissingChannelCard(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"customer-exact-sku": {"litellm_provider":"openai", "input_cost_per_token":0.000002, "output_cost_per_token":0.000006},
		"customer-free-sku": {"litellm_provider":"openai", "input_cost_per_token":0, "output_cost_per_token":0}
	}`)
	billing := NewBillingService(&config.Config{}, catalog)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
	for _, model := range []string{"customer-exact-sku", "customer-free-sku", "gpt-5.4"} {
		t.Run(model, func(t *testing.T) {
			require.Nil(t, channels.GetChannelModelPricing(context.Background(), platformFallbackAdmissionGroupID, model))
			require.False(t, channels.IsModelRestricted(context.Background(), platformFallbackAdmissionGroupID, model),
				"a same-SKU system price, including explicit zero, satisfies price admission when platform fallback is disabled")
		})
	}
}

func TestPlatformPricingFallbackAdmission_AllConcretePlatformDefaultsAllowUnknownSKU(t *testing.T) {
	for _, platform := range []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu",
		"deepseek", "minimax", "stepfun", "opencode_go", "typesafe", "command_code", "cline",
	} {
		t.Run(platform, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			channels := platformFallbackAdmissionChannels(platform, billing)
			require.False(t, channels.IsModelRestricted(context.Background(), platformFallbackAdmissionGroupID, "unpublished-request-sku"),
				"every concrete platform has its own usable default price fallback")
		})
	}
}

func TestPlatformPricingFallbackAdmission_DisabledOrUnpricedFallbackRejectsUnknownSKU(t *testing.T) {
	for _, fallback := range []string{"", "unpriced-fallback-sku"} {
		t.Run(fallback, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			billing.frozenPricingFallbackModels = map[string]string{
				PlatformOpenAI: fallback,
				PlatformGrok:   "grok-4.6",
			}
			channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
			require.True(t, channels.IsModelRestricted(context.Background(), platformFallbackAdmissionGroupID, "unpublished-request-sku"),
				"an unavailable OpenAI price cannot borrow the configured Grok fallback")
		})
	}
}

func TestPlatformPricingFallbackAdmission_ResolvedPlatformOwnsFallback(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "", PlatformGrok: "grok-4.6"}
	channels := platformFallbackAdmissionChannels(PlatformComposite, billing)
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformGrok)
	require.False(t, channels.IsModelRestricted(ctx, platformFallbackAdmissionGroupID, "gpt-6.1-terra-unpublished-sku"),
		"a model prefix cannot replace the resolved concrete platform")
	ctx = WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
	require.True(t, channels.IsModelRestricted(ctx, platformFallbackAdmissionGroupID, "gpt-6.1-terra-unpublished-sku"))
}

func TestPlatformPricingFallbackAdmission_ExplicitGroupPricesPrecedeMissingSystemPrices(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
	}{
		{name: "exact token", card: ChannelModelPricing{Models: []string{"unpublished-request-sku"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6)}},
		{name: "explicit zero token", card: ChannelModelPricing{Models: []string{"unpublished-request-sku"}, BillingMode: BillingModeToken, InputPrice: billingFloat(0)}},
		{name: "wildcard token", card: ChannelModelPricing{Models: []string{"unpublished-*"}, BillingMode: BillingModeToken, OutputPrice: billingFloat(3e-6)}},
		{name: "request price", card: ChannelModelPricing{Models: []string{"unpublished-request-sku"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(1)}},
		{name: "explicit zero request", card: ChannelModelPricing{Models: []string{"unpublished-request-sku"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
			channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
			group := platformFallbackAdmissionGroup()
			tc.card.Platform = PlatformOpenAI
			group.ModelPricing = []ChannelModelPricing{tc.card}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			require.False(t, channels.IsModelRestricted(ctx, group.ID, "unpublished-request-sku"))

			key := &APIKey{Group: group, GroupID: &group.ID, User: &User{ID: 1}}
			ctx = withBillingPricingSnapshot(ctx, key, time.Now(), billing, channels, nil, "unpublished-request-sku")
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			require.False(t, channels.IsModelRestricted(ctx, group.ID, "unpublished-request-sku"),
				"the accepted request's frozen group price must also satisfy scheduler admission")
		})
	}
}

func TestPlatformPricingFallbackAdmission_EmptyAndMultiplierOnlyGroupCardsCannotInventPrices(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
	}{
		{name: "empty token", card: ChannelModelPricing{BillingMode: BillingModeToken}},
		{name: "empty request", card: ChannelModelPricing{BillingMode: BillingModePerRequest}},
		{name: "fast multiplier", card: ChannelModelPricing{FastMultiplier: billingFloat(2)}},
		{name: "interval multiplier", card: ChannelModelPricing{Intervals: []PricingInterval{{MinTokens: 0, InputMultiplier: billingFloat(2)}}}},
		{name: "group token interval has no flat price", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{MinTokens: 0, InputPrice: billingFloat(1e-6)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
			channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
			group := platformFallbackAdmissionGroup()
			tc.card.Platform, tc.card.Models = PlatformOpenAI, []string{"unpublished-request-sku"}
			group.ModelPricing = []ChannelModelPricing{tc.card}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			require.True(t, channels.IsModelRestricted(ctx, group.ID, "unpublished-request-sku"))
			resolver := NewModelPricingResolver(channels, billing)
			resolved := resolver.Resolve(ctx, PricingInput{Model: "unpublished-request-sku", Group: group, GroupID: &group.ID})
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: "unpublished-request-sku", Group: group, GroupID: &group.ID,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1,
				Resolver: resolver, Resolved: resolved,
			})
			require.ErrorIs(t, err, ErrModelPricingUnavailable,
				"group token cards override flat rates; an interval alone cannot supply a price at settlement")
			require.Nil(t, cost)
		})
	}
}

func TestPlatformPricingFallbackAdmission_GroupPriceCannotCrossGroupOrPlatformBoundary(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
	group := platformFallbackAdmissionGroup()
	group.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"unpublished-request-sku"}, InputPrice: billingFloat(0)}}
	group.ID++
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	require.True(t, channels.IsModelRestricted(ctx, platformFallbackAdmissionGroupID, "unpublished-request-sku"))

	group.ID = platformFallbackAdmissionGroupID
	group.ModelPricing[0].Platform = PlatformAnthropic
	ctx = context.WithValue(context.Background(), ctxkey.Group, group)
	require.True(t, channels.IsModelRestricted(ctx, group.ID, "unpublished-request-sku"))
}

func TestPlatformPricingFallbackAdmission_GroupRepositoryAndSnapshotRetainExplicitPrice(t *testing.T) {
	const model = "unpublished-request-sku"
	billing := NewBillingService(&config.Config{}, nil)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
	group := platformFallbackAdmissionGroup()
	group.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{model}, InputPrice: billingFloat(0)}}
	channels.groupRepo = &mockGroupRepoForGateway{groups: map[int64]*Group{group.ID: group}}
	require.False(t, channels.IsModelRestricted(context.Background(), group.ID, model),
		"callers without a hydrated context group still use the owning group's explicit price")

	key := &APIKey{Group: group, GroupID: &group.ID, User: &User{ID: 1}}
	ctx := withBillingPricingSnapshot(context.Background(), key, time.Now(), billing, channels, nil, model)
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	group.ModelPricing = nil
	ctx = context.WithValue(ctx, ctxkey.Group, group)
	require.False(t, channels.IsModelRestricted(ctx, group.ID, model),
		"an accepted request retains its frozen price when the live group changes")
	require.True(t, channels.IsModelRestricted(context.Background(), group.ID, model),
		"a later request sees the current unpriced group")
}

type platformFallbackAdmissionAccountRepo struct {
	stubOpenAIAccountRepo
	listCalls int
}

func (r *platformFallbackAdmissionAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	r.listCalls++
	return r.stubOpenAIAccountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
}

func platformFallbackAdmissionAccount(id int64, mapping map[string]any) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Priority:    int(id),
		GroupIDs:    []int64{platformFallbackAdmissionGroupID},
		Credentials: map[string]any{"model_mapping": mapping},
	}
}

func TestPlatformPricingFallbackAdmission_OpenAISchedulerHonorsSystemFallbackGroupAndDisabledCases(t *testing.T) {
	for _, tc := range []struct {
		name          string
		model         string
		disable       bool
		groupPrice    bool
		wantAvailable bool
	}{
		{name: "same SKU system price", model: "gpt-5.4", disable: true, wantAvailable: true},
		{name: "platform fallback", model: "unpublished-request-sku", wantAvailable: true},
		{name: "disabled fallback", model: "unpublished-request-sku", disable: true},
		{name: "explicit group price", model: "unpublished-request-sku", disable: true, groupPrice: true, wantAvailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			if tc.disable {
				billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
			}
			channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
			group := platformFallbackAdmissionGroup()
			if tc.groupPrice {
				group.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{tc.model}, InputPrice: billingFloat(0)}}
			}
			repo := &platformFallbackAdmissionAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{
				accounts: []Account{platformFallbackAdmissionAccount(1, nil)},
			}}
			svc := &OpenAIGatewayService{accountRepo: repo, channelService: channels, billingService: billing}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			account, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", tc.model, nil)
			if tc.wantAvailable {
				require.NoError(t, err)
				require.NotNil(t, account)
				require.Equal(t, int64(1), account.ID)
				require.Equal(t, tc.model, account.GetMappedModel(tc.model), "pricing fallback must not replace the forwarded model")
				require.Positive(t, repo.listCalls)
			} else {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, account)
				require.Contains(t, err.Error(), "channel pricing restriction")
				require.Zero(t, repo.listCalls, "unpriced requests must be rejected before account selection")
			}
		})
	}
}

func TestPlatformPricingFallbackAdmission_AccountAllowlistStillRejectsOriginalUnknownModel(t *testing.T) {
	const model = "unpublished-request-sku"
	for _, tc := range []struct {
		name        string
		accountType string
		allowSecond bool
	}{
		{name: "API key only unauthorized account", accountType: AccountTypeAPIKey},
		{name: "API key authorized account remains eligible", accountType: AccountTypeAPIKey, allowSecond: true},
		{name: "OAuth only unauthorized account", accountType: AccountTypeOAuth},
		{name: "OAuth authorized account remains eligible", accountType: AccountTypeOAuth, allowSecond: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
			accounts := []Account{platformFallbackAdmissionAccount(1, map[string]any{"gpt-5.4": "gpt-5.4"})}
			if tc.allowSecond {
				accounts = append(accounts, platformFallbackAdmissionAccount(2, map[string]any{model: model}))
			}
			for i := range accounts {
				accounts[i].Type = tc.accountType
			}
			svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: accounts}, channelService: channels, billingService: billing}
			group := platformFallbackAdmissionGroup()
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			require.False(t, channels.IsModelRestricted(ctx, group.ID, model), "the request has a platform fallback price")
			account, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", model, nil)
			if tc.allowSecond {
				require.NoError(t, err)
				require.NotNil(t, account)
				require.Equal(t, int64(2), account.ID, "a lower-priority permitted account wins over a higher-priority account that only allows the fallback SKU")
			} else {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, account)
			}
		})
	}
}

func TestPlatformPricingFallbackAdmission_PriceResolutionDoesNotGrantGroupAllowlistPermission(t *testing.T) {
	// Group allowlists are enforced at the HTTP/WS boundary. Verify that resolving
	// fallback pricing leaves the original request and permission configuration intact.
	const model = "unpublished-request-sku"
	group := platformFallbackAdmissionGroup()
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}
	billing := NewBillingService(&config.Config{}, nil)
	channels := platformFallbackAdmissionChannels(PlatformOpenAI, billing)
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	require.False(t, group.ModelAllowlist.Allows(model))
	require.False(t, channels.IsModelRestricted(ctx, group.ID, model))
	resolved := NewModelPricingResolver(channels, billing).Resolve(ctx, PricingInput{Model: model, Group: group, GroupID: &group.ID})
	require.Equal(t, PricingSourcePlatformFallback, resolved.Source)
	require.NotNil(t, resolved.BasePricing)
	require.Equal(t, "gpt-6.1-sol", resolved.BasePricing.FallbackModel)
	require.False(t, group.ModelAllowlist.Allows(model), "a priced request must remain forbidden by its original group allowlist")
	require.Equal(t, []string{"gpt-5.4"}, group.ModelAllowlist.Models)
}
