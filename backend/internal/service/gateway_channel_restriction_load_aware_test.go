//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// loadAwareRestrictionFixture 构造走负载感知路径（load_batch 开启 + 并发服务存在）的
// GatewayService：分组 10 绑定 upstream 计费基准的渠道，claude-sonnet-4-6 有显式价格。
// 账号 1 把 restriction-unpriced-request 映射为自身（无价格），账号 2 映射为 claude-sonnet-4-6（有价格）。
type loadAwareRestrictionFixture struct {
	svc              *GatewayService
	ctx              context.Context
	groupID          int64
	cache            *mockGatewayCacheForPlatform
	concurrencyCache *mockConcurrencyCache
}

func newLoadAwareRestrictionFixture(t *testing.T, restrict bool, sessionBindings map[string]int64, routing map[string][]int64) *loadAwareRestrictionFixture {
	t.Helper()

	groupID := int64(10)
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		RestrictModels:     restrict,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4-6"}, InputPrice: testPtrFloat64(0)},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{groupID: PlatformAnthropic}))

	accountRepo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID: 1, Platform: PlatformAnthropic, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 5,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"restriction-unpriced-request": "restriction-unpriced-request"},
				},
			},
			{
				ID: 2, Platform: PlatformAnthropic, Priority: 2, Status: StatusActive, Schedulable: true, Concurrency: 5,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"restriction-unpriced-request": "claude-sonnet-4-6"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range accountRepo.accounts {
		accountRepo.accountsByID[accountRepo.accounts[i].ID] = &accountRepo.accounts[i]
	}

	group := &Group{
		ID:                  groupID,
		Platform:            PlatformAnthropic,
		Status:              StatusActive,
		Hydrated:            true,
		ModelRoutingEnabled: len(routing) > 0,
		ModelRouting:        routing,
	}
	groupRepo := &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}}

	cfg := testConfig()
	channelSvc.billingService = NewBillingService(cfg, nil)
	channelSvc.billingService.frozenPricingFallbackModels = map[string]string{PlatformAnthropic: ""}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cache := &mockGatewayCacheForPlatform{sessionBindings: sessionBindings}
	concurrencyCache := &mockConcurrencyCache{}

	svc := &GatewayService{
		accountRepo:        accountRepo,
		groupRepo:          groupRepo,
		channelService:     channelSvc,
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	return &loadAwareRestrictionFixture{
		svc:              svc,
		ctx:              context.WithValue(context.Background(), ctxkey.Group, group),
		groupID:          groupID,
		cache:            cache,
		concurrencyCache: concurrencyCache,
	}
}

func TestSelectAccountWithLoadAwareness_UpstreamRestrictionSkipsDisallowedAccount(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, true, nil, nil)
	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "", "restriction-unpriced-request", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, int64(2), result.Account.ID, "上游模型没有有效价格的高优先级账号必须被跳过")
	require.Equal(t, 1, f.concurrencyCache.loadBatchCalls, "应经过 Layer 2 负载感知选择")
}

func TestSelectAccountWithLoadAwareness_UpstreamRestrictionRejectsWhenAllAccountsDisallowed(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, true, nil, nil)
	// 账号 2 也改为映射到无有效价格的模型
	f.svc.accountRepo.(*mockAccountRepoForPlatform).accounts[1].Credentials = map[string]any{
		"model_mapping": map[string]any{"restriction-unpriced-request": "restriction-unpriced-request"},
	}

	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "", "restriction-unpriced-request", nil, "", 0)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.ErrorContains(t, err, "channel pricing restriction")
	require.Nil(t, result)
	require.Equal(t, 0, f.concurrencyCache.acquireAccountCalls, "没有合规候选时不应尝试占用任何账号槽位")
}

func TestSelectAccountWithLoadAwareness_UpstreamRestrictionIgnoresStickyAccount(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, true, map[string]int64{"sticky": 1}, nil)
	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "sticky", "restriction-unpriced-request", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, int64(2), result.Account.ID, "粘性账号的上游模型无有效价格时不得沿用粘性会话")
	require.Equal(t, int64(2), f.cache.sessionBindings["sticky"], "粘性会话应重新绑定到合规账号")
}

func TestSelectAccountWithLoadAwareness_UpstreamRestrictionFiltersRoutedAccounts(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, true, nil, map[string][]int64{"restriction-unpriced-request": {1}})
	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "", "restriction-unpriced-request", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, int64(2), result.Account.ID, "模型路由指向的账号被渠道限制时应回退到合规账号")
}

func TestSelectAccountWithLoadAwareness_UpstreamRestrictionRoutedStickyAccountNotHonored(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, true, map[string]int64{"sticky": 1}, map[string][]int64{"restriction-unpriced-request": {1, 2}})
	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "sticky", "restriction-unpriced-request", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, int64(2), result.Account.ID, "路由列表内的粘性账号被渠道限制时不得沿用")
}

func TestSelectAccountWithLoadAwareness_RestrictModelsDisabledKeepsPriorityOrder(t *testing.T) {
	t.Parallel()

	f := newLoadAwareRestrictionFixture(t, false, nil, nil)
	result, err := f.svc.SelectAccountWithLoadAwareness(f.ctx, &f.groupID, "", "restriction-unpriced-request", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	require.Equal(t, int64(1), result.Account.ID, "未开启限制时不应因定价列表过滤账号")
}
