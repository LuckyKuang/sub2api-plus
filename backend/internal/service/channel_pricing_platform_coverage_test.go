//go:build unit

package service

import (
	"path/filepath"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// 平台覆盖矩阵：每个平台必须「可列出且有安全价」或「可列出且明确 manual /
// unsupported」，不允许静默空价格，也不允许家族子串价冒充型号价。
// 这里全部跑随仓库发布的真实目录 + 内置兜底，避免测试 fixtures 与生产数据漂移。

func newBundledCatalogReferenceService(t *testing.T) *ChannelPricingReferenceService {
	t.Helper()
	svc := NewPricingService(&config.Config{
		Pricing: config.PricingConfig{
			DataDir:      t.TempDir(),
			FallbackFile: filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"),
		},
	}, nil)
	require.NoError(t, svc.Initialize())
	t.Cleanup(svc.Stop)
	return NewChannelPricingReferenceService(svc, NewBillingService(&config.Config{}, svc))
}

func referencesByModel(t *testing.T, refs []ChannelPricingReference) map[string]ChannelPricingReference {
	t.Helper()
	byModel := make(map[string]ChannelPricingReference, len(refs))
	for _, ref := range refs {
		byModel[ref.Model] = ref
	}
	return byModel
}

func mustResolve(t *testing.T, svc *ChannelPricingReferenceService, platform, model string) ChannelPricingReference {
	t.Helper()
	ref, err := svc.Resolve(t.Context(), platform, model)
	require.NoErrorf(t, err, "platform=%s model=%s", platform, model)
	return ref
}

// 全平台形状约束 + 单模型查询与列表一致（不限于 openai）。
func TestPlatformCoverage_AllPlatformsShapeAndConsistency(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)

	for _, platform := range []string{
		PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo,
	} {
		refs, err := svc.List(t.Context(), platform)
		require.NoErrorf(t, err, "platform=%s", platform)
		require.NotEmptyf(t, refs, "platform=%s must list its supported models", platform)

		for _, ref := range refs {
			require.Equalf(t, platform, ref.Platform, "model=%s", ref.Model)
			switch ref.Status {
			case ChannelPricingStatusPriced:
				require.NotNilf(t, ref.Pricing, "model=%s priced must carry a card", ref.Model)
				require.NotEmptyf(t, ref.Source, "model=%s priced must declare a source", ref.Model)
				require.Containsf(t, ref.Pricing.Models, ref.Model, "model=%s", ref.Model)
				require.NotEmptyf(t, string(ref.Pricing.BillingMode),
					"model=%s must declare a billing mode", ref.Model)
				require.Emptyf(t, ref.ReasonCode, "model=%s priced must not carry a reason", ref.Model)
			case ChannelPricingStatusManualRequired, ChannelPricingStatusUnsupportedUnit:
				require.Nilf(t, ref.Pricing, "model=%s must not carry a zero price card", ref.Model)
				require.NotEmptyf(t, ref.ReasonCode, "model=%s must explain why", ref.Model)
			default:
				t.Fatalf("platform=%s model=%s unexpected status=%s", platform, ref.Model, ref.Status)
			}

			// 单模型查询必须与列表给出同一份判定，否则会出现「同步有价、手动添加没价」。
			single := mustResolve(t, svc, platform, ref.Model)
			require.Equalf(t, ref.Status, single.Status, "model=%s", ref.Model)
			require.Equalf(t, ref.Source, single.Source, "model=%s", ref.Model)
			require.Equalf(t, ref.MatchedModel, single.MatchedModel, "model=%s", ref.Model)
			require.Equalf(t, ref.ReasonCode, single.ReasonCode, "model=%s", ref.Model)
			require.Equalf(t, ref.Pricing, single.Pricing, "model=%s", ref.Model)
		}
	}
}

// OpenAI：新型号精确目录价 + 音频单位不可表达。
func TestPlatformCoverage_OpenAI(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformOpenAI)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	sol := byModel["gpt-6-sol"]
	require.Equal(t, ChannelPricingStatusPriced, sol.Status)
	require.Equal(t, ChannelPricingSourceReleaseCatalog, sol.Source)
	require.Equal(t, BillingModeToken, sol.Pricing.BillingMode)
	require.InDelta(t, 2e-6, *sol.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 10e-6, *sol.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 2.5e-6, *sol.Pricing.CacheWritePrice, 1e-15)
	require.InDelta(t, 2e-7, *sol.Pricing.CacheReadPrice, 1e-15)
	require.InDelta(t, 2, *sol.Pricing.FastMultiplier, 1e-12)
	require.Len(t, sol.Pricing.Intervals, 1)
	require.Equal(t, 272000, sol.Pricing.Intervals[0].MinTokens)
	require.InDelta(t, 4e-6, *sol.Pricing.Intervals[0].InputPrice, 1e-15)
	require.InDelta(t, 15e-6, *sol.Pricing.Intervals[0].OutputPrice, 1e-15)

	luna := byModel["gpt-6-luna"]
	require.Equal(t, ChannelPricingStatusPriced, luna.Status)
	require.InDelta(t, 1e-7, *luna.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 5e-7, *luna.Pricing.OutputPrice, 1e-15)
	// Sol 与 Luna 价差 20 倍，串价会立刻在金额上暴露。
	require.NotEqual(t, *sol.Pricing.InputPrice, *luna.Pricing.InputPrice)

	// 音频按秒/按字符计费，表单无法表达：必须 unsupported，不能给 token 价卡。
	for _, model := range []string{
		"gpt-4o-mini-tts", "gpt-4o-mini-transcribe", "gpt-4o-transcribe", "gpt-4o-transcribe-diarize",
	} {
		ref := byModel[model]
		require.Equalf(t, ChannelPricingStatusUnsupportedUnit, ref.Status, "model=%s", model)
		require.Equalf(t, ReasonUnsupportedBillingDimension, ref.ReasonCode, "model=%s", model)
		require.Nilf(t, ref.Pricing, "model=%s must not carry a zero price card", model)
	}
}

// Anthropic：Opus 5.5 独立价卡，不与 Opus 5 串价。
func TestPlatformCoverage_Anthropic(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformAnthropic)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	opus55 := byModel["claude-opus-5-5"]
	require.Equal(t, ChannelPricingStatusPriced, opus55.Status)
	require.Equal(t, ChannelPricingSourceReleaseCatalog, opus55.Source)
	require.InDelta(t, 4e-6, *opus55.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 20e-6, *opus55.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 5e-6, *opus55.Pricing.CacheWritePrice, 1e-15)
	require.InDelta(t, 8e-6, *opus55.Pricing.CacheWrite1hPrice, 1e-15)
	require.InDelta(t, 2e-7, *opus55.Pricing.CacheReadPrice, 1e-15)

	opus5 := byModel["claude-opus-5"]
	require.Equal(t, ChannelPricingStatusPriced, opus5.Status)
	require.InDelta(t, 5e-6, *opus5.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 25e-6, *opus5.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 6.25e-6, *opus5.Pricing.CacheWritePrice, 1e-15)

	// 目录里没有 claude-sonnet-5：保持可见且 manual，不得套 sonnet-4 家族价。
	sonnet5 := byModel["claude-sonnet-5"]
	require.Equal(t, ChannelPricingStatusManualRequired, sonnet5.Status)
	require.Equal(t, ReasonExactPriceUnavailable, sonnet5.ReasonCode)
	require.Nil(t, sonnet5.Pricing)
}

// Gemini：同一 SKU 可以只存在于 Vertex 标签下，不能因 provider 标签不同被排除；
// embedding 的显式 0 输出价保留；音频单位 unsupported。
func TestPlatformCoverage_Gemini(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformGemini)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	flash := byModel["gemini-3.6-flash"]
	require.Equal(t, ChannelPricingStatusPriced, flash.Status,
		"a SKU stored only under vertex_ai-language-models must stay priced for the gemini platform")
	require.Equal(t, ChannelPricingSourceReleaseCatalog, flash.Source)
	require.InDelta(t, 1.5e-6, *flash.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 7.5e-6, *flash.Pricing.OutputPrice, 1e-15)

	// embedding：input 有价、output 显式 0（免费）必须编码为 0 而不是 null。
	embedding := byModel["gemini-embedding-001"]
	require.Equal(t, ChannelPricingStatusPriced, embedding.Status)
	require.InDelta(t, 1.5e-7, *embedding.Pricing.InputPrice, 1e-15)
	require.NotNil(t, embedding.Pricing.OutputPrice)
	require.InDelta(t, 0, *embedding.Pricing.OutputPrice, 1e-15)

	for _, model := range []string{
		"gemini-2.5-flash-preview-tts",
		"gemini-live-2.5-flash-preview-native-audio-09-2025",
	} {
		ref := byModel[model]
		require.Equalf(t, ChannelPricingStatusUnsupportedUnit, ref.Status, "model=%s", model)
		require.Equalf(t, ReasonUnsupportedBillingDimension, ref.ReasonCode, "model=%s", model)
		require.Nilf(t, ref.Pricing, "model=%s", model)
	}
}

// Antigravity：Claude/Gemini 价卡按实际上游型号解析；thinking 后缀映射到同 SKU
// 基名；没有登记映射的 thinking 档保持 manual，不猜价。
func TestPlatformCoverage_Antigravity(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformAntigravity)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	claudeThinking := byModel["claude-opus-4-5-thinking"]
	require.Equal(t, ChannelPricingStatusPriced, claudeThinking.Status)
	require.Equal(t, ChannelPricingSourceReleaseCatalog, claudeThinking.Source)
	require.Equal(t, "claude-opus-4-5", claudeThinking.MatchedModel)
	require.InDelta(t, 5e-6, *claudeThinking.Pricing.InputPrice, 1e-15)

	geminiThinking := byModel["gemini-2.5-flash-thinking"]
	require.Equal(t, ChannelPricingStatusPriced, geminiThinking.Status)
	require.Equal(t, "gemini-2.5-flash", geminiThinking.MatchedModel)
	require.InDelta(t, 3e-7, *geminiThinking.Pricing.InputPrice, 1e-15)

	tiered := byModel["gemini-3.6-flash-high"]
	require.Equal(t, ChannelPricingStatusPriced, tiered.Status)
	require.Equal(t, "gemini-3.6-flash", tiered.MatchedModel)

	// gemini-3-pro-high 在目录里没有基名 gemini-3-pro：不得借 preview 型号顶替。
	for _, model := range []string{"gemini-3-pro-high", "gemini-3-pro-low"} {
		ref := byModel[model]
		require.Equalf(t, ChannelPricingStatusManualRequired, ref.Status, "model=%s", model)
		require.Equalf(t, ReasonExactPriceUnavailable, ref.ReasonCode, "model=%s", model)
		require.Nilf(t, ref.Pricing, "model=%s", model)
	}
}

// Grok：文本型号全覆盖（含已登记别名），媒体型号按图片/视频分档计价，
// 任何媒体型号都不得落到 unknown-text 兜底拿到 token 价卡。
func TestPlatformCoverage_Grok(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformGrok)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	// 平台清单内所有型号都必须有价：text 走目录/内置，media 走分档登记。
	for _, ref := range refs {
		require.Equalf(t, ChannelPricingStatusPriced, ref.Status, "model=%s", ref.Model)
		require.NotNilf(t, ref.Pricing, "model=%s", ref.Model)
	}

	grok := byModel["grok-4.7"]
	require.Equal(t, ChannelPricingSourceReleaseCatalog, grok.Source)
	require.InDelta(t, 2e-6, *grok.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 6e-6, *grok.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 5e-7, *grok.Pricing.CacheReadPrice, 1e-15)
	// xAI 阈值语义为「达到即进高档」：区间 min 必须取 threshold-1，
	// 否则 200000 整会被漏到基础档。
	require.Len(t, grok.Pricing.Intervals, 1)
	require.Equal(t, 199999, grok.Pricing.Intervals[0].MinTokens)
	require.Equal(t, ">=200000", grok.Pricing.Intervals[0].TierLabel)
	require.InDelta(t, 4e-6, *grok.Pricing.Intervals[0].InputPrice, 1e-15)
	require.InDelta(t, 12e-6, *grok.Pricing.Intervals[0].OutputPrice, 1e-15)
	require.InDelta(t, 1e-6, *grok.Pricing.Intervals[0].CacheReadPrice, 1e-15)

	// 已登记同 SKU 别名：日期快照/composer 变体归到同一张官方价卡。
	aliased := byModel["grok-4.20-0309-reasoning"]
	require.Equal(t, ChannelPricingStatusPriced, aliased.Status)
	require.Equal(t, ChannelPricingSourceBuiltinFallback, aliased.Source)
	require.Equal(t, "grok-4.20", aliased.MatchedModel)
	require.InDelta(t, 1.25e-6, *aliased.Pricing.InputPrice, 1e-15)

	composer := byModel["grok-composer-2.5-fast"]
	require.Equal(t, ChannelPricingStatusPriced, composer.Status)
	require.Equal(t, "grok-build-0.1", composer.MatchedModel)
	require.InDelta(t, 1e-6, *composer.Pricing.InputPrice, 1e-15)

	// 媒体型号：图片按次 + 分辨率分档；视频按次 + 清晰度分档。
	image := byModel["grok-imagine-image"]
	require.Equal(t, ChannelPricingStatusPriced, image.Status)
	require.Equal(t, BillingModeImage, image.Pricing.BillingMode)
	require.InDelta(t, 0.02, *image.Pricing.PerRequestPrice, 1e-12)
	tierPrices := map[string]float64{}
	for _, interval := range image.Pricing.Intervals {
		require.NotNil(t, interval.PerRequestPrice)
		tierPrices[interval.TierLabel] = *interval.PerRequestPrice
	}
	require.InDelta(t, 0.02, tierPrices["2K"], 1e-12)
	require.InDelta(t, 0.02, tierPrices["4K"], 1e-12)

	quality := byModel["grok-imagine-image-quality"]
	require.InDelta(t, 0.05, *quality.Pricing.PerRequestPrice, 1e-12)

	video := byModel["grok-imagine-video-1.5"]
	require.Equal(t, BillingModeVideo, video.Pricing.BillingMode)
	require.InDelta(t, 0.08, *video.Pricing.PerRequestPrice, 1e-12)
	videoTiers := map[string]float64{}
	for _, interval := range video.Pricing.Intervals {
		videoTiers[interval.TierLabel] = *interval.PerRequestPrice
	}
	require.InDelta(t, 0.14, videoTiers["720p"], 1e-12)
	require.InDelta(t, 0.25, videoTiers["1080p"], 1e-12)

	// 没有任何媒体型号拿到 token 计费模式。
	for model, ref := range byModel {
		if isGrokMediaFamilyModel(model) {
			require.NotEqualf(t, BillingModeToken, ref.Pricing.BillingMode, "model=%s", model)
		}
	}
}

// Kimi/智谱/DeepSeek/MiniMax：动态目录行不全，靠精确内置 fallback 列出；
// 免费 GLM 条目保留显式 0，未维护字段保持 null。
func TestPlatformCoverage_BuiltinOnlyPlatforms(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)

	kimi := mustResolve(t, svc, PlatformKimi, "kimi-k2.6")
	require.Equal(t, ChannelPricingStatusPriced, kimi.Status)
	require.Equal(t, ChannelPricingSourceBuiltinFallback, kimi.Source)
	require.InDelta(t, 0.95e-6, *kimi.Pricing.InputPrice, 1e-15)

	glmFree := mustResolve(t, svc, PlatformZhipu, "glm-4.5-flash")
	require.Equal(t, ChannelPricingStatusPriced, glmFree.Status)
	require.Equal(t, ChannelPricingSourceBuiltinFallback, glmFree.Source)
	// 显式免费：0 必须编码为 0，不能被当成缺字段。
	require.NotNil(t, glmFree.Pricing.InputPrice)
	require.InDelta(t, 0, *glmFree.Pricing.InputPrice, 1e-15)
	require.NotNil(t, glmFree.Pricing.OutputPrice)
	require.InDelta(t, 0, *glmFree.Pricing.OutputPrice, 1e-15)
	// 未维护的缓存字段保持 null——「没有该字段」不等于「0 元」。
	require.Nil(t, glmFree.Pricing.CacheWritePrice)
	require.Nil(t, glmFree.Pricing.CacheReadPrice)

	glm := mustResolve(t, svc, PlatformZhipu, "glm-5.3")
	require.Equal(t, ChannelPricingStatusPriced, glm.Status)
	require.InDelta(t, 1.4e-6, *glm.Pricing.InputPrice, 1e-15)

	deepseek := mustResolve(t, svc, PlatformDeepseek, "deepseek-v4-pro")
	require.Equal(t, ChannelPricingStatusPriced, deepseek.Status)
	require.Equal(t, ChannelPricingSourceReleaseCatalog, deepseek.Source)
	require.InDelta(t, 6.6e-7, *deepseek.Pricing.InputPrice, 1e-15)

	minimax := mustResolve(t, svc, PlatformMiniMax, "minimax-m3")
	require.Equal(t, ChannelPricingStatusPriced, minimax.Status)
	require.Equal(t, ChannelPricingSourceBuiltinFallback, minimax.Source)

	// 每个内置兜底平台都必须列得出型号（目录没有 moonshot/zhipu/minimax 行）。
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformDeepseek} {
		refs, err := svc.List(t.Context(), platform)
		require.NoErrorf(t, err, "platform=%s", platform)
		require.NotEmptyf(t, refs, "platform=%s must still list its supported models", platform)
	}
}

// OpenCode Go：29 个默认型号全部列出；没有精确价的 14 个保持 manual_required；
// 有上游目录行的型号（grok-4.7）不得因 provider 不匹配消失；清单不得被上游
// 目录宇宙撑大（聚合网关只转发自己发布的型号）。
func TestPlatformCoverage_OpenCodeGo(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)
	refs, err := svc.List(t.Context(), PlatformOpenCodeGo)
	require.NoError(t, err)
	byModel := referencesByModel(t, refs)

	require.Len(t, refs, 29)
	for _, id := range DefaultOpenCodeGoModelIDs() {
		require.Containsf(t, byModel, id, "opencode_go must keep %s visible", id)
	}
	// 上游目录宇宙不得进入聚合平台清单。
	require.NotContains(t, byModel, "gpt-4o-mini-tts")
	require.NotContains(t, byModel, "claude-opus-5-5")

	for _, model := range []string{
		"kimi-k2.7-code", "longcat-2.0", "mimo-v2.5", "mimo-v2.5-pro",
		"muse-spark-1.3-contributor", "muse-spark-1.2-contributor",
		"qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.7-plus",
		"qwen3.6-plus", "hy4-preview", "hy3", "omen-alpha",
	} {
		ref, ok := byModel[model]
		require.Truef(t, ok, "opencode_go must keep %s visible", model)
		require.Equalf(t, ChannelPricingStatusManualRequired, ref.Status, "model=%s", model)
		require.Equalf(t, ReasonExactPriceUnavailable, ref.ReasonCode, "model=%s", model)
		require.Nilf(t, ref.Pricing, "model=%s", model)
	}

	// grok-4.7 只有 xai 目录行：聚合平台必须能经上游标签取到该价。
	grok := byModel["grok-4.7"]
	require.Equal(t, ChannelPricingStatusPriced, grok.Status,
		"an upstream catalog row must not disappear from the aggregator platform")
	require.Equal(t, ChannelPricingSourceReleaseCatalog, grok.Source)
	require.InDelta(t, 2e-6, *grok.Pricing.InputPrice, 1e-15)

	// 其余型号经内置兜底或上游目录拿到价。
	for _, model := range []string{"glm-5.3", "kimi-k2.6", "deepseek-v4-pro", "gpt-5.6-luna"} {
		ref := byModel[model]
		require.Equalf(t, ChannelPricingStatusPriced, ref.Status, "model=%s", model)
		require.NotNilf(t, ref.Pricing, "model=%s", model)
	}
}

// 平台边界：未知平台是调用方参数错误（ErrUnsupportedPlatform），与目录不可用区分。
func TestPlatformCoverage_UnknownPlatformIsParameterError(t *testing.T) {
	svc := newBundledCatalogReferenceService(t)

	for _, platform := range []string{"", "unknown", "OPENAI_", "opencode-go"} {
		_, err := svc.Resolve(t.Context(), platform, "gpt-6-sol")
		require.ErrorIsf(t, err, ErrUnsupportedPlatform, "platform=%q", platform)
		_, err = svc.List(t.Context(), platform)
		require.ErrorIsf(t, err, ErrUnsupportedPlatform, "platform=%q", platform)
	}
}
