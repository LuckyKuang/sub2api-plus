//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newOpenAIChannelRestrictionService(repo *mockChannelRepository) *ChannelService {
	channelSvc := newTestChannelService(repo)
	// Mapping tests need an actually unpriced alternate path. Keep identified
	// system cards available, while explicitly disabling the platform fallback.
	channelSvc.billingService = NewBillingService(testConfig(), nil)
	channelSvc.billingService.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	return channelSvc
}

func TestOpenAISelectAccountForModelWithExclusions_ChannelMappedRestrictionRejectsEarly(t *testing.T) {
	t.Parallel()

	channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-4o"}, InputPrice: testPtrFloat64(0)},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: {"gpt-4.1": "restriction-unpriced-channel"},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		channelService: channelSvc,
	}

	groupID := int64(10)
	_, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Contains(t, err.Error(), "channel pricing restriction")
}

func TestOpenAISelectAccountForModelWithExclusions_UpstreamRestrictionSkipsDisallowedAccount(t *testing.T) {
	t.Parallel()

	channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}, InputPrice: testPtrFloat64(0)},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    10,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "restriction-unpriced-upstream"},
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    20,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "o3-mini"},
				},
			},
		}},
		channelService: channelSvc,
	}

	groupID := int64(10)
	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
}

func TestIsUpstreamModelRestrictedByChannel_CompactMappingMatchesForwardPath(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"model_mapping":         map[string]any{"restriction-channel-model": "restriction-account-model"},
			"compact_model_mapping": map[string]any{"restriction-account-model": "restriction-compact-model"},
		},
	}
	tests := []struct {
		name                   string
		allowedUpstreamModel   string
		useCompactModelMapping bool
	}{
		{
			name:                   "legacy compact applies compact mapping after channel and account mapping",
			allowedUpstreamModel:   "restriction-compact-model",
			useCompactModelMapping: true,
		},
		{
			name:                   "native v2 stops after channel and account mapping",
			allowedUpstreamModel:   "restriction-account-model",
			useCompactModelMapping: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
				ID:                 1,
				Status:             StatusActive,
				GroupIDs:           []int64{10},
				RestrictModels:     true,
				BillingModelSource: BillingModelSourceUpstream,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformOpenAI, Models: []string{tt.allowedUpstreamModel}, InputPrice: testPtrFloat64(0)},
				},
				ModelMapping: map[string]map[string]string{
					PlatformOpenAI: {"restriction-requested-model": "restriction-channel-model"},
				},
			}, map[int64]string{10: PlatformOpenAI}))
			svc := &OpenAIGatewayService{channelService: channelSvc}
			mapping := channelSvc.ResolveChannelMapping(context.Background(), 10, "restriction-requested-model")
			require.True(t, mapping.Mapped)
			require.Equal(t, "restriction-channel-model", mapping.MappedModel)

			ctx := WithOpenAIForwardModel(
				context.Background(),
				mapping.MappedModel,
				tt.useCompactModelMapping,
			)
			require.False(t, svc.isUpstreamModelRestrictedByChannel(
				ctx, 10, account, "restriction-requested-model", true,
			))
			require.True(t, svc.isUpstreamModelRestrictedByChannel(
				context.Background(), 10, account, "restriction-requested-model", true,
			), "without the forward-model context the restriction check follows a different chain")
		})
	}
}

func TestIsUpstreamModelRestrictedByChannel_PassthroughMatchesForwardPath(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"restriction-channel-model": "restriction-account-model"},
			"compact_model_mapping": map[string]any{
				"restriction-channel-model": "restriction-compact-model",
			},
		},
		Extra: map[string]any{"openai_passthrough": true},
	}
	tests := []struct {
		name                   string
		allowedUpstreamModel   string
		useCompactModelMapping bool
	}{
		{
			name:                   "native v2 keeps channel-mapped model and ignores normal account mapping",
			allowedUpstreamModel:   "restriction-channel-model",
			useCompactModelMapping: false,
		},
		{
			name:                   "legacy compact applies compact mapping to channel-mapped model",
			allowedUpstreamModel:   "restriction-compact-model",
			useCompactModelMapping: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
				ID:                 1,
				Status:             StatusActive,
				GroupIDs:           []int64{10},
				RestrictModels:     true,
				BillingModelSource: BillingModelSourceUpstream,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformOpenAI, Models: []string{tt.allowedUpstreamModel}, InputPrice: testPtrFloat64(0)},
				},
				ModelMapping: map[string]map[string]string{
					PlatformOpenAI: {"restriction-requested-model": "restriction-channel-model"},
				},
			}, map[int64]string{10: PlatformOpenAI}))
			svc := &OpenAIGatewayService{channelService: channelSvc}
			mapping := channelSvc.ResolveChannelMapping(context.Background(), 10, "restriction-requested-model")
			require.True(t, mapping.Mapped)
			require.Equal(t, "restriction-channel-model", mapping.MappedModel)

			ctx := WithOpenAIForwardModel(
				context.Background(),
				mapping.MappedModel,
				tt.useCompactModelMapping,
			)
			require.False(t, svc.isUpstreamModelRestrictedByChannel(
				ctx, 10, account, "restriction-requested-model", true,
			))
		})
	}
}

func TestIsUpstreamModelRestrictedByChannel_PassthroughFlagWithRawChatFallbackMatchesForwardPath(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"restriction-channel-model": "restriction-account-model"},
			"compact_model_mapping": map[string]any{
				"restriction-account-model": "restriction-compact-model",
			},
		},
		Extra: map[string]any{
			"openai_passthrough":         true,
			"openai_responses_supported": false,
		},
	}

	for _, useCompactModelMapping := range []bool{false, true} {
		useCompactModelMapping := useCompactModelMapping
		name := "native v2"
		if useCompactModelMapping {
			name = "legacy compact"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
				ID:                 1,
				Status:             StatusActive,
				GroupIDs:           []int64{10},
				RestrictModels:     true,
				BillingModelSource: BillingModelSourceUpstream,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformOpenAI, Models: []string{"restriction-account-model"}, InputPrice: testPtrFloat64(0)},
				},
				ModelMapping: map[string]map[string]string{
					PlatformOpenAI: {"restriction-requested-model": "restriction-channel-model"},
				},
			}, map[int64]string{10: PlatformOpenAI}))
			svc := &OpenAIGatewayService{channelService: channelSvc}
			ctx := WithOpenAIForwardModel(
				context.Background(),
				"restriction-channel-model",
				useCompactModelMapping,
			)

			require.False(t, svc.isUpstreamModelRestrictedByChannel(
				ctx, 10, account, "restriction-requested-model", true,
			))
		})
	}
}

func TestIsUpstreamModelRestrictedByChannel_ForwardModelContextMatchesNormalForwardPath(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"restriction-channel-model": "restriction-account-model"},
		},
		Extra: map[string]any{
			"openai_passthrough":         true,
			"openai_responses_supported": false,
		},
	}
	channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"restriction-account-model"}, InputPrice: testPtrFloat64(0)},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: {"restriction-requested-model": "restriction-channel-model"},
		},
	}, map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}
	ctx := WithOpenAIForwardModel(context.Background(), "restriction-channel-model", false)

	require.False(t, svc.isUpstreamModelRestrictedByChannel(
		ctx, 10, account, "restriction-requested-model", false,
	))
}

func TestOpenAISelectAccountForModelWithExclusions_StickyRestrictedUpstreamFallsBack(t *testing.T) {
	t.Parallel()

	channelSvc := newOpenAIChannelRestrictionService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}, InputPrice: testPtrFloat64(0)},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	cache := &stubGatewayCache{
		sessionBindings: map[string]int64{"openai:sticky-session": 1},
	}
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    10,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "restriction-unpriced-upstream"},
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    20,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "o3-mini"},
				},
			},
		}},
		channelService: channelSvc,
		cache:          cache,
	}

	groupID := int64(10)
	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "sticky-session", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
	require.Equal(t, 1, cache.deletedSessions["openai:sticky-session"])
	require.Equal(t, int64(2), cache.sessionBindings["openai:sticky-session"])
}
