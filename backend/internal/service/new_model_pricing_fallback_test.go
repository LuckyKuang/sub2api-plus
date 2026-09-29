//go:build unit

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	openai "github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// newEmptyCatalogBillingService 构造「远端目录不可用 / 目录较旧缺条目」的现场：
// 没有任何 LitellM 数据，只能靠项目内置同型号 fallback 计费。
func newEmptyCatalogBillingService(t *testing.T) *BillingService {
	t.Helper()
	svc := &PricingService{}
	svc.pricingData = map[string]*LiteLLMModelPricing{}
	return NewBillingService(&config.Config{}, svc)
}

func requireModelPricing(t *testing.T, svc *BillingService, model string) *ModelPricing {
	t.Helper()
	pricing, err := svc.GetModelPricing(model)
	require.NoErrorf(t, err, "model=%s must resolve its own card, not fail closed", model)
	require.NotNil(t, pricing)
	return pricing
}

// 无目录时 GPT-6 Sol/Luna 必须拿到自身价卡，而不是 UnknownModel 时报错或
// 一路掉到 matchOpenAIModel 的 DefaultTestModel（gpt-5.1-codex）。
func TestNewModelPricingNoCatalogUsesSameModelFallback(t *testing.T) {
	svc := newEmptyCatalogBillingService(t)

	sol := requireModelPricing(t, svc, "gpt-6-sol")
	require.InDelta(t, 2e-6, sol.InputPricePerToken, 1e-15)
	require.InDelta(t, 10e-6, sol.OutputPricePerToken, 1e-15)
	require.InDelta(t, 2.5e-6, sol.CacheCreationPricePerToken, 1e-15)
	require.InDelta(t, 0.2e-6, sol.CacheReadPricePerToken, 1e-15)
	// Fast 档 2 倍、长上下文 272000 阈值
	require.InDelta(t, 4e-6, sol.InputPricePerTokenPriority, 1e-15)
	require.InDelta(t, 20e-6, sol.OutputPricePerTokenPriority, 1e-15)
	require.Equal(t, 272_000, sol.LongContextInputThreshold)
	require.InDelta(t, 2, sol.LongContextInputMultiplier, 1e-12)
	require.InDelta(t, 1.5, sol.LongContextOutputMultiplier, 1e-12)

	luna := requireModelPricing(t, svc, "gpt-6-luna")
	require.InDelta(t, 0.1e-6, luna.InputPricePerToken, 1e-15)
	require.InDelta(t, 0.5e-6, luna.OutputPricePerToken, 1e-15)
	require.InDelta(t, 0.125e-6, luna.CacheCreationPricePerToken, 1e-15)
	require.InDelta(t, 0.01e-6, luna.CacheReadPricePerToken, 1e-15)

	// Sol 与 Luna 价差 20 倍，任一条串到另一条都会立刻在金额上暴露。
	require.NotEqual(t, sol.InputPricePerToken, luna.InputPricePerToken)

	// PricingService 侧同样要有同型号兜底：GetModelPricing 不能返回 nil。
	direct := svc.pricingService.GetModelPricing("gpt-6-sol")
	require.NotNil(t, direct)
	require.InDelta(t, 2e-6, direct.InputCostPerToken, 1e-15)
	lunaDirect := svc.pricingService.GetModelPricing("gpt-6-luna")
	require.NotNil(t, lunaDirect)
	require.InDelta(t, 0.5e-6, lunaDirect.OutputCostPerToken, 1e-15)

	// 未登记的 gpt-6-* 拼写不得冒充 Sol/Luna 拿到价卡。
	require.Empty(t, openai.GPT6SolOrLunaBaseModel("gpt-6"),
		"unregistered gpt-6 spelling must not resolve to a Sol/Luna card")
	require.Nil(t, svc.pricingService.GetModelPricing("gpt-6"))
}

// 旧目录里有相似但不相同的型号时，Sol/Luna 不得串到 GPT-5.6 或 GPT-6 Astra。
func TestNewModelPricingStaleCatalogDoesNotCrossMatch(t *testing.T) {
	svc := &PricingService{}
	svc.pricingData = map[string]*LiteLLMModelPricing{
		"gpt-5.6-sol":             openAIGPT56SolFallbackPricing,
		"gpt-5.6-luna":            openAIGPT56LunaFallbackPricing,
		"gpt-5.1-codex":           openAIGPT54FallbackPricing, // DefaultTestModel 的替身
		"claude-opus-5":           {InputCostPerToken: 5e-6, OutputCostPerToken: 25e-6},
		"claude-opus-4-8":         {InputCostPerToken: 5e-6, OutputCostPerToken: 25e-6},
		"claude-opus-5-5-preview": {InputCostPerToken: 999e-6, OutputCostPerToken: 999e-6},
	}
	billing := NewBillingService(&config.Config{}, svc)

	sol := requireModelPricing(t, billing, "gpt-6-sol")
	require.InDelta(t, 2e-6, sol.InputPricePerToken, 1e-15,
		"gpt-6-sol must not inherit gpt-5.6-sol ($5) or gpt-5.1-codex")
	require.InDelta(t, 10e-6, sol.OutputPricePerToken, 1e-15)

	luna := requireModelPricing(t, billing, "gpt-6-luna")
	require.InDelta(t, 0.1e-6, luna.InputPricePerToken, 1e-15,
		"gpt-6-luna must not inherit gpt-5.6-luna ($0.20)")

	// Opus 5.5 只有同型号 fallback：目录缺条目时既不能用 Opus 5（$5/$25，1.25 倍
	// 超收），也不得被 claude-opus-5-5-preview 这种相似名带偏。
	opus := requireModelPricing(t, billing, "claude-opus-5-5")
	require.InDelta(t, 4e-6, opus.InputPricePerToken, 1e-15)
	require.InDelta(t, 20e-6, opus.OutputPricePerToken, 1e-15)
	require.InDelta(t, 5e-6, opus.CacheCreation5mPrice, 1e-15)
	require.InDelta(t, 8e-6, opus.CacheCreation1hPrice, 1e-15)
	require.InDelta(t, 0.2e-6, opus.CacheReadPricePerToken, 1e-15)
	require.True(t, opus.SupportsCacheBreakdown)
	require.InDelta(t, 8e-6, opus.InputPricePerTokenPriority, 1e-15)

	// Opus 5 自己仍是 $5/$25，两张卡互不污染。
	opus5 := requireModelPricing(t, billing, "claude-opus-5")
	require.InDelta(t, 5e-6, opus5.InputPricePerToken, 1e-15)
	require.InDelta(t, 25e-6, opus5.OutputPricePerToken, 1e-15)
}

// 完整目录存在时精确目录优先，字段 presence 不被静态兜底改写。
func TestNewModelPricingExactCatalogWinsOverStaticFallback(t *testing.T) {
	svc := &PricingService{}
	var err error
	svc.pricingData, err = svc.parsePricingData([]byte(`{
		"gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.00003},
		"claude-opus-5-5":{"litellm_provider":"anthropic","input_cost_per_token":0.000005,"output_cost_per_token":0.000025,"cache_creation_input_token_cost":0.000006,"cache_creation_input_token_cost_above_1hr":0.000009}
	}`))
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, svc)

	sol := requireModelPricing(t, billing, "gpt-6-sol")
	require.InDelta(t, 3e-6, sol.InputPricePerToken, 1e-15)
	require.InDelta(t, 30e-6, sol.OutputPricePerToken, 1e-15)

	opus := requireModelPricing(t, billing, "claude-opus-5-5")
	require.InDelta(t, 5e-6, opus.InputPricePerToken, 1e-15)
	require.InDelta(t, 25e-6, opus.OutputPricePerToken, 1e-15)
	require.InDelta(t, 6e-6, opus.CacheCreation5mPrice, 1e-15)
	require.InDelta(t, 9e-6, opus.CacheCreation1hPrice, 1e-15)
	require.True(t, opus.SupportsCacheBreakdown)
}

// Sol/Luna 长上下文档位：等于 272000 仍是基础档，272001 进高档（输入/缓存 ×2、输出 ×1.5）。
func TestNewModelPricingGPT6LongContextThreshold(t *testing.T) {
	svc := newEmptyCatalogBillingService(t)
	atThreshold := svc.shouldApplySessionLongContextPricing(
		UsageTokens{InputTokens: 272_000},
		requireModelPricing(t, svc, "gpt-6-sol"),
	)
	require.False(t, atThreshold, "272000 total input must stay on the base tier")
	above := svc.shouldApplySessionLongContextPricing(
		UsageTokens{InputTokens: 272_001},
		requireModelPricing(t, svc, "gpt-6-sol"),
	)
	require.True(t, above, "272001 total input must enter the long-context tier")
}

// Sol/Luna 的 Flex 档沿用 0.5 倍、Fast/priority 沿用 2 倍通用档位倍率。
func TestNewModelPricingGPT6ServiceTierMultipliers(t *testing.T) {
	svc := newEmptyCatalogBillingService(t)
	pricing := requireModelPricing(t, svc, "gpt-6-sol")
	require.InDelta(t, 2, configuredServiceTierMultiplier("fast", pricing), 1e-12)
	require.InDelta(t, 2, configuredServiceTierMultiplier("priority", pricing), 1e-12)
	require.InDelta(t, 0.5, configuredServiceTierMultiplier("flex", pricing), 1e-12)

	opus := requireModelPricing(t, svc, "claude-opus-5-5")
	require.InDelta(t, 2, configuredServiceTierMultiplier("fast", opus), 1e-12)
}

// HasIdentifiedTokenPricing 必须能识别三个新型号，否则上游自报模型计费会被拒。
func TestNewModelPricingHasIdentifiedTokenPricing(t *testing.T) {
	svc := newEmptyCatalogBillingService(t)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "claude-opus-5-5"} {
		require.Truef(t, svc.HasIdentifiedTokenPricing(model), "model=%s", model)
	}
}
