//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func newReferenceService(t *testing.T, catalogJSON string) *ChannelPricingReferenceService {
	t.Helper()
	pricing := &PricingService{}
	var err error
	pricing.pricingData, err = pricing.parsePricingData([]byte(catalogJSON))
	require.NoError(t, err)
	return NewChannelPricingReferenceService(pricing, NewBillingService(&config.Config{}, pricing))
}

// 单模型查询与平台列表必须给出同一份 status/source/pricing，否则管理端会出现
// 「同步时有价、手动添加时没价」的割裂。
func TestChannelPricingReferenceListMatchesResolve(t *testing.T) {
	svc := newReferenceService(t, `{
		"gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000002,"output_cost_per_token":0.00001},
		"claude-opus-5-5":{"litellm_provider":"anthropic","input_cost_per_token":0.000004,"output_cost_per_token":0.00002}
	}`)

	refs, err := svc.List(context.Background(), PlatformOpenAI)
	require.NoError(t, err)
	require.NotEmpty(t, refs)
	for _, ref := range refs {
		single, err := svc.Resolve(context.Background(), PlatformOpenAI, ref.Model)
		require.NoError(t, err)
		require.Equal(t, ref.Status, single.Status, "model=%s", ref.Model)
		require.Equal(t, ref.Source, single.Source, "model=%s", ref.Model)
		require.Equal(t, ref.MatchedModel, single.MatchedModel, "model=%s", ref.Model)
		require.Equal(t, ref.Pricing, single.Pricing, "model=%s", ref.Model)
	}
}

// 平台不支持的入参是 400 语义，必须能和「没有快照」区分开。
func TestChannelPricingReferenceUnsupportedPlatform(t *testing.T) {
	svc := newReferenceService(t, `{"gpt-5.4":{"litellm_provider":"openai","input_cost_per_token":0.0000025}}`)

	_, err := svc.Resolve(context.Background(), "not-a-platform", "gpt-6-sol")
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
	_, err = svc.List(context.Background(), "not-a-platform")
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
	_, err = svc.RefreshAndList(context.Background(), "not-a-platform")
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
}

// 相似家族价不能被当成精确参考价；找不到就必须 manual_required 且 pricing 为 nil，
// 不能返回全零价卡。
func TestChannelPricingReferenceRejectsFuzzyFamilyPrice(t *testing.T) {
	svc := newReferenceService(t, `{
		"gpt-5.6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000005},
		"claude-opus-5":{"litellm_provider":"anthropic","input_cost_per_token":0.000005},
		"claude-opus-5-5-preview":{"litellm_provider":"anthropic","input_cost_per_token":0.999}
	}`)

	for _, model := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna"} {
		ref, err := svc.Resolve(context.Background(), PlatformOpenAI, model)
		require.NoError(t, err)
		// Sol/Luna 有自己的同型号内置兜底，这里给出的是 builtin 而非精确目录等价物。
		require.Equal(t, ChannelPricingStatusPriced, ref.Status, "model=%s", model)
		require.Equal(t, ChannelPricingSourceBuiltinFallback, ref.Source, "model=%s", model)
		require.NotNil(t, ref.Pricing, "model=%s", model)
		require.Positive(t, *ref.Pricing.InputPrice, "model=%s", model)
	}

	// Opus 5.5 不能拿 Opus 5 或 Opus 5.5-preview 的价。
	ref, err := svc.Resolve(context.Background(), PlatformAnthropic, "claude-opus-5-5")
	require.NoError(t, err)
	require.Equal(t, ChannelPricingSourceBuiltinFallback, ref.Source)
	require.NotNil(t, ref.Pricing)
	require.InDelta(t, 4e-6, *ref.Pricing.InputPrice, 1e-12,
		"Opus 5.5 must use its own $4 card, not Opus 5 ($5) or the preview SKU")
}

// 跨平台不得串价：目录条的 provider 标签属于哪个平台，就只能服务哪个平台。
func TestChannelPricingReferenceNeverCrossesPlatforms(t *testing.T) {
	svc := newReferenceService(t, `{
		"gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000002},
		"claude-opus-5-5":{"litellm_provider":"anthropic","input_cost_per_token":0.000004}
	}`)

	ref, err := svc.Resolve(context.Background(), PlatformOpenAI, "claude-opus-5-5")
	require.NoError(t, err)
	require.Equal(t, ChannelPricingStatusManualRequired, ref.Status,
		"an Anthropic catalog row must not price an OpenAI channel")
	require.Nil(t, ref.Pricing)
	require.Equal(t, ReasonExactPriceUnavailable, ref.ReasonCode)

	ref, err = svc.Resolve(context.Background(), PlatformAnthropic, "gpt-6-sol")
	require.NoError(t, err)
	require.Equal(t, ChannelPricingStatusManualRequired, ref.Status,
		"an OpenAI catalog row must not price an Anthropic channel")
	require.Nil(t, ref.Pricing)
}

// 目录里没有 provider 行的平台不得整平台消失；没有精确价的型号必须可见且 manual。
func TestChannelPricingReferenceListsBuiltinOnlyPlatforms(t *testing.T) {
	svc := newReferenceService(t, `{"gpt-5.4":{"litellm_provider":"openai","input_cost_per_token":0.0000025}}`)

	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformOpenCodeGo} {
		refs, err := svc.List(context.Background(), platform)
		require.NoError(t, err)
		require.NotEmptyf(t, refs, "platform=%s must still list its supported models", platform)
		for _, ref := range refs {
			require.Equal(t, platform, ref.Platform)
			switch ref.Status {
			case ChannelPricingStatusPriced:
				require.NotNil(t, ref.Pricing, "model=%s", ref.Model)
				require.NotNil(t, ref.Pricing.InputPrice, "model=%s", ref.Model)
				require.Equal(t, ChannelPricingSourceBuiltinFallback, ref.Source, "model=%s", ref.Model)
			case ChannelPricingStatusManualRequired, ChannelPricingStatusUnsupportedUnit:
				require.Nil(t, ref.Pricing, "model=%s must not carry a zero price card", ref.Model)
				require.NotEmpty(t, ref.ReasonCode, "model=%s", ref.Model)
			default:
				t.Fatalf("platform=%s model=%s unexpected status=%s", platform, ref.Model, ref.Status)
			}
		}
	}

	refs, err := svc.List(context.Background(), PlatformOpenCodeGo)
	require.NoError(t, err)
	seen := map[string]ChannelPricingReference{}
	for _, ref := range refs {
		seen[ref.Model] = ref
	}
	for _, model := range []string{
		"kimi-k2.7-code", "longcat-2.0", "mimo-v2.5", "qwen3.8-max",
		"hy4-preview", "omen-alpha",
	} {
		ref, ok := seen[model]
		require.Truef(t, ok, "opencode_go must keep %s visible", model)
		require.Equal(t, ChannelPricingStatusManualRequired, ref.Status, "model=%s", model)
		require.Equal(t, ReasonExactPriceUnavailable, ref.ReasonCode, "model=%s", model)
	}
}

// 长上下文档位必须折进完整价卡，只返回基础 input/output 不算完成。
// 渠道区间是左开右闭 (min, max]：272000 阈值必须折成 min=272000，
// 这样 272000 走基础档、272001 落进高挡——少一个 token 就和官方计费不一致。
func TestChannelPricingReferenceCarriesLongContextTier(t *testing.T) {
	svc := newReferenceService(t, `{
		"gpt-6-sol":{
			"litellm_provider":"openai",
			"input_cost_per_token":0.000002,
			"output_cost_per_token":0.00001,
			"cache_creation_input_token_cost":0.0000025,
			"cache_read_input_token_cost":0.0000002,
			"long_context_input_token_threshold":272000,
			"long_context_input_cost_multiplier":2,
			"long_context_output_cost_multiplier":1.5
		}
	}`)

	ref, err := svc.Resolve(context.Background(), PlatformOpenAI, "gpt-6-sol")
	require.NoError(t, err)
	require.Equal(t, ChannelPricingStatusPriced, ref.Status)
	require.Len(t, ref.Pricing.Intervals, 1)
	interval := ref.Pricing.Intervals[0]
	require.Equal(t, 272000, interval.MinTokens)
	require.Equal(t, ">272000", interval.TierLabel)
	require.InDelta(t, 4e-6, *interval.InputPrice, 1e-12)
	require.InDelta(t, 15e-6, *interval.OutputPrice, 1e-12)
	// 输入与缓存在高档都是 2 倍。
	require.InDelta(t, 4e-7, *interval.CacheReadPrice, 1e-12)
	require.InDelta(t, 5e-6, *interval.CacheWritePrice, 1e-12)

	// 区间必须能接住 272001 本身（左开右闭），否则 272001 会按基础档计费。
	require.Nil(t, findLongContextMatch(ref.Pricing.Intervals, 272000),
		"272000 total input must stay on the base tier")
	require.NotNil(t, findLongContextMatch(ref.Pricing.Intervals, 272001),
		"272001 total input must enter the long-context tier")
}

// findLongContextMatch 用渠道区间既有的左开右闭语义检查某个 token 数是否落进高档。
func findLongContextMatch(intervals []PricingInterval, totalTokens int) *PricingInterval {
	return FindMatchingInterval(intervals, totalTokens)
}

// 未配置远端刷新时不得硬编码成 refreshed/current。
func TestChannelPricingReferenceRefreshDisabledWithoutRemote(t *testing.T) {
	svc := newReferenceService(t, `{"gpt-5.4":{"litellm_provider":"openai","input_cost_per_token":0.0000025}}`)

	snapshot, err := svc.RefreshAndList(context.Background(), PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, RefreshStatusDisabled, snapshot.RefreshStatus)
	require.Empty(t, snapshot.WarningCode)
	require.NotEmpty(t, snapshot.Models)
}
