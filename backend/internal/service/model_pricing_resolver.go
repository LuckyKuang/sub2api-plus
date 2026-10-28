package service

import (
	"context"
	"log/slog"
	"maps"
	"strings"
)

// PricingSource 定价来源标识
const (
	PricingSourceGroup    = "group"
	PricingSourceChannel  = "channel"
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice         float64
	defaultPerRequestPriceExplicit bool

	// 来源标识
	Source string // "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool

	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
	channelPricing *ChannelModelPricing

	longContextPricingEnabled bool
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Group → Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model   string
	GroupID *int64 // nil 表示不检查渠道
	Group   *Group
}

// Resolve 解析模型定价。
// 1. 获取基础定价（LiteLLM → Fallback）
// 2. 如果指定了 GroupID，查找渠道定价并覆盖
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		if snapshot.resolver != nil && r != snapshot.resolver {
			return snapshot.resolver.Resolve(ctx, input)
		}
		if input.Group != nil && snapshot.group != nil && snapshot.group.ID == input.Group.ID {
			input.Group = snapshot.group
		}
	}
	longContextPricingEnabled := input.Group == nil || input.Group.LongContextPricingEnabled
	resolved := r.resolveLowerPricing(ctx, input)
	if groupPricing := matchGroupModelPricingForPlatform(input.Group, input.Model, requestBillingPlatform(ctx, input.Group, "")); groupPricing != nil {
		// An empty editor card is not a zero-price declaration or a change of
		// billing mode. Unset fields inherit the next source in the price chain.
		if !hasConfiguredPricingOverrides(groupPricing) {
			resolved.longContextPricingEnabled = longContextPricingEnabled
			return resolved
		}
		if groupPricing.BillingMode == "" {
			groupPricing.BillingMode = resolved.Mode
		}
		if groupPricing.BillingMode == resolved.Mode && resolved.channelPricing != nil {
			groupPricing = overlayModelPricing(resolved.channelPricing, groupPricing)
		}
		// Group token cards only override the first-tier / flat rates.
		// Long-context ladders come from official presets, gated by the checkbox.
		if groupPricing.BillingMode == "" || groupPricing.BillingMode == BillingModeToken {
			stripped := groupPricing.Clone()
			stripped.Intervals = nil
			groupPricing = &stripped
		}
		resolved := r.resolveConfiguredPricing(groupPricing, input.Model, PricingSourceGroup)
		resolved.longContextPricingEnabled = longContextPricingEnabled
		return resolved
	}
	resolved.longContextPricingEnabled = longContextPricingEnabled
	return resolved
}

func (r *ModelPricingResolver) resolveLowerPricing(ctx context.Context, input PricingInput) *ResolvedPricing {
	var chPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		chPricing = r.lookupChannelPricingNormalized(ctx, *input.GroupID, input.Model)
		if chPricing != nil && !hasConfiguredPricingOverrides(chPricing) && (chPricing.BillingMode == "" || chPricing.BillingMode == BillingModeToken) {
			chPricing = nil
		}
		if chPricing != nil {
			mode := chPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
				resolved := &ResolvedPricing{
					Mode:           mode,
					Source:         PricingSourceChannel,
					channelPricing: chPricing,
				}
				r.applyRequestTierOverrides(chPricing, resolved)
				return resolved
			}
		}
	}

	// 1. 获取基础定价
	basePricing, source := r.resolveBasePricing(input.Model)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = PricingSourceChannel
		resolved.channelPricing = chPricing
		r.applyTokenOverrides(chPricing, resolved)
	}

	return resolved
}

// overlayModelPricing overlays only explicit group values on a same-mode
// channel card. It preserves unspecified prices and service-tier factors.
// Retained channel time metadata is inert for group-owned pricing: only
// PricingSourceChannel applies channel time multipliers or publishes schedules.
func overlayModelPricing(lower, upper *ChannelModelPricing) *ChannelModelPricing {
	merged := upper.Clone()
	for _, field := range []struct {
		target    **float64
		inherited *float64
	}{
		{&merged.InputPrice, lower.InputPrice}, {&merged.OutputPrice, lower.OutputPrice},
		{&merged.CacheWritePrice, lower.CacheWritePrice}, {&merged.CacheWrite1hPrice, lower.CacheWrite1hPrice},
		{&merged.CacheReadPrice, lower.CacheReadPrice}, {&merged.ImageInputPrice, lower.ImageInputPrice},
		{&merged.ImageOutputPrice, lower.ImageOutputPrice}, {&merged.PerRequestPrice, lower.PerRequestPrice},
		{&merged.FastMultiplier, lower.FastMultiplier}, {&merged.FlexMultiplier, lower.FlexMultiplier},
	} {
		if *field.target == nil && field.inherited != nil {
			value := *field.inherited
			*field.target = &value
		}
	}
	if merged.ReasoningEffortMultipliers == nil {
		merged.ReasoningEffortMultipliers = maps.Clone(lower.ReasoningEffortMultipliers)
	}
	if len(merged.Intervals) == 0 {
		merged.Intervals = lower.Clone().Intervals
	}
	if merged.TimePricing == nil {
		merged.TimePricing = lower.Clone().TimePricing
	}
	return &merged
}

func hasFlatTokenPrice(config *ChannelModelPricing) bool {
	return config != nil && (config.InputPrice != nil || config.OutputPrice != nil || config.CacheWritePrice != nil ||
		config.CacheWrite1hPrice != nil || config.CacheReadPrice != nil || config.ImageInputPrice != nil || config.ImageOutputPrice != nil)
}

func hasIntervalTokenPrice(interval *PricingInterval) bool {
	return interval != nil && (interval.InputPrice != nil || interval.OutputPrice != nil || interval.CacheWritePrice != nil ||
		interval.CacheWrite1hPrice != nil || interval.CacheReadPrice != nil)
}

func hasConfiguredPricingOverrides(config *ChannelModelPricing) bool {
	return config != nil && (hasFlatTokenPrice(config) || config.PerRequestPrice != nil ||
		config.FastMultiplier != nil || config.FlexMultiplier != nil || len(config.ReasoningEffortMultipliers) > 0 ||
		len(filterValidIntervals(config.Intervals)) > 0 || config.TimePricing != nil)
}

func (r *ModelPricingResolver) resolveConfiguredPricing(config *ChannelModelPricing, model, source string) *ResolvedPricing {
	mode := config.BillingMode
	if mode == "" {
		mode = BillingModeToken
	}
	resolved := &ResolvedPricing{Mode: mode, Source: source, channelPricing: config}
	if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
		r.applyRequestTierOverrides(config, resolved)
		return resolved
	}
	resolved.BasePricing, _ = r.resolveBasePricing(model)
	resolved.SupportsCacheBreakdown = resolved.BasePricing != nil && resolved.BasePricing.SupportsCacheBreakdown
	r.applyTokenOverrides(config, resolved)
	return resolved
}

func matchGroupModelPricing(group *Group, model string) *ChannelModelPricing {
	return matchGroupModelPricingForPlatform(group, model, requestBillingPlatform(context.Background(), group, ""))
}

func matchGroupModelPricingForPlatform(group *Group, model, platform string) *ChannelModelPricing {
	if group == nil {
		return nil
	}
	model = normalizeChannelPricingModelName(model)
	var best *ChannelModelPricing
	bestModel, bestPlatform := -1, -1
	for i := range group.ModelPricing {
		entry := &group.ModelPricing[i]
		entryPlatform := entry.Platform
		platformSpecificity := 1
		if group.Platform == PlatformComposite && (entryPlatform == "" || entryPlatform == PlatformComposite) {
			// Legacy composite cards were stored as group-wide explicit prices.
			// Preserve them; concrete-platform cards win at equal model specificity.
			platformSpecificity = 0
		} else {
			if entryPlatform == "" {
				entryPlatform = group.Platform
			}
			if entryPlatform != platform {
				continue
			}
		}
		for _, pattern := range entry.Models {
			normalized := normalizeChannelPricingModelName(pattern)
			specificity := -1
			if normalized == model {
				specificity = len(normalized) + 257
			} else if strings.HasSuffix(normalized, "*") {
				prefix := strings.TrimSuffix(normalized, "*")
				if strings.HasPrefix(model, prefix) {
					specificity = len(prefix)
				}
			}
			if specificity < 0 || specificity < bestModel || (specificity == bestModel && platformSpecificity <= bestPlatform) {
				continue
			}
			copy := entry.Clone()
			best, bestModel, bestPlatform = &copy, specificity, platformSpecificity
		}
	}
	return best
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
	pricing, err := r.billingService.GetModelPricing(model)
	if err != nil {
		slog.Debug("failed to get model pricing from LiteLLM, using fallback",
			"model", model, "error", err)
		return nil, PricingSourceFallback
	}
	return pricing, PricingSourceLiteLLM
}

// lookupChannelPricingNormalized 查找渠道定价：先用字面模型名做精确/通配匹配，
// 未命中时用与官方兜底价一致的归一化模型名再查一次。
//
// 官方兜底价对 OpenAI/Codex 族会把 gpt-5.6-luna-high 这类变体名归一化到基名
// （billing_service.go 的 normalizeKnownOpenAICodexModel 分支），而渠道定价此前
// 只认字面名。两者不对称导致：管理员只配基名、请求模型带 effort 后缀时，渠道定价
// 未命中而官方兜底命中，计费候选循环首个成功即返回，渠道定价永远轮不到（issue #5256）。
//
// 字面名优先，保证管理员对具体变体的显式配价不被基名覆盖；非 OpenAI 模型
// normalizeKnownOpenAICodexModel 返回空串，此处天然 no-op。
func (r *ModelPricingResolver) lookupChannelPricingNormalized(ctx context.Context, groupID int64, model string) *ChannelModelPricing {
	if r.channelService == nil {
		return nil
	}
	if pricing := r.channelService.GetChannelModelPricing(ctx, groupID, model); pricing != nil {
		return pricing
	}
	if isUnregisteredGPT61TerraPricingModel(model) {
		return nil
	}
	normalized := normalizeKnownOpenAICodexModel(model)
	if normalized == "" || strings.EqualFold(normalized, strings.TrimSpace(model)) {
		return nil
	}
	return r.channelService.GetChannelModelPricing(ctx, groupID, normalized)
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.Intervals = filterValidIntervals(chPricing.Intervals)
	for i := range resolved.Intervals {
		if resolved.Intervals[i].CacheWrite1hPrice != nil {
			resolved.SupportsCacheBreakdown = true
			break
		}
	}
	if resolved.BasePricing == nil {
		if !hasFlatTokenPrice(chPricing) {
			return
		}
		resolved.BasePricing = &ModelPricing{}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	applyChannelTokenPriceOverrides(resolved.BasePricing, chPricing)
	if chPricing.CacheWrite1hPrice != nil {
		resolved.SupportsCacheBreakdown = true
		enableCacheCreationBreakdown(resolved.BasePricing)
	}
	for i := range chPricing.Intervals {
		if chPricing.Intervals[i].CacheWrite1hPrice != nil {
			resolved.SupportsCacheBreakdown = true
			enableCacheCreationBreakdown(resolved.BasePricing)
			break
		}
	}
	if chPricing.FastMultiplier != nil {
		resolved.BasePricing.FastMultiplier = chPricing.FastMultiplier
	}
	if chPricing.FlexMultiplier != nil {
		resolved.BasePricing.FlexMultiplier = chPricing.FlexMultiplier
	}
	if chPricing.ReasoningEffortMultipliers != nil {
		resolved.BasePricing.ReasoningEffortMultipliers = maps.Clone(chPricing.ReasoningEffortMultipliers)
	}
	applyChannelImagePriceOverrides(chPricing, resolved.BasePricing)

	// 区间未命中时回退到上面已经应用渠道覆盖的基础价。
	resolved.Intervals = filterValidIntervals(chPricing.Intervals)
}

// applyChannelImagePriceOverrides 应用渠道图片输入/输出价，规则与其他 token 字段一致：
//   - 渠道填写了（含 0）：覆盖目录价。图片输出价同时标记 ImageOutputPriceExplicit，
//     显式 0 表示图片输出免费，computeTokenBreakdown 不再回退文本输出价。
//   - 渠道留空（nil）：沿用目录价（LiteLLM 价格表或内置兜底价）。目录也没有图片价时，
//     computeTokenBreakdown 分别回退文本输入/输出价。
//
// 此前留空会被归零，图片输出还被标成显式 0：只建了空价卡的图片模型，图片输出
// 一律按 0 计费，图片输入按文本价计费。
func applyChannelImagePriceOverrides(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing == nil || pricing == nil {
		return
	}
	if chPricing.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
		pricing.ImageOutputPriceExplicit = true
	}
	if chPricing.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(chPricing.Intervals)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
		resolved.defaultPerRequestPriceExplicit = true
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheWrite1hPrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil || iv.InputMultiplier != nil ||
			iv.OutputMultiplier != nil || iv.CacheWriteMultiplier != nil ||
			iv.CacheReadMultiplier != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 如果有区间列表，找到匹配区间并构造 ModelPricing；否则直接返回 BasePricing。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	iv := FindMatchingInterval(resolved.Intervals, totalContextTokens)
	if iv == nil {
		return resolved.BasePricing
	}
	if resolved.BasePricing == nil && !hasIntervalTokenPrice(iv) {
		return nil
	}

	pricing := intervalToModelPricing(iv, resolved.BasePricing, resolved.channelPricing)
	// BasePricing 为 nil（仅配置区间）时拷贝不到该标志，从 resolved 回填，
	// 保证 computeCacheCreationCost 的 5m/1h 分档判断不被区间路径吞掉。
	pricing.SupportsCacheBreakdown = resolved.SupportsCacheBreakdown
	return pricing
}

// intervalToModelPricing 将区间定价转换为 ModelPricing
func intervalToModelPricing(iv *PricingInterval, base *ModelPricing, chPricing *ChannelModelPricing) *ModelPricing {
	pricing := &ModelPricing{}
	if base != nil {
		*pricing = *base
	}
	applyMultiplier := func(value float64, multiplier *float64) float64 {
		if multiplier == nil {
			return value
		}
		return value * *multiplier
	}
	if iv.InputPrice != nil {
		pricing.InputPricePerTokenPriority = channelTierOverridePrice(pricing.InputPricePerToken, pricing.InputPricePerTokenPriority, *iv.InputPrice)
		pricing.InputPricePerToken = *iv.InputPrice
	} else if iv.InputMultiplier != nil {
		pricing.InputPricePerToken = applyMultiplier(pricing.InputPricePerToken, iv.InputMultiplier)
		pricing.InputPricePerTokenPriority = applyMultiplier(pricing.InputPricePerTokenPriority, iv.InputMultiplier)
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerTokenPriority = channelTierOverridePrice(pricing.OutputPricePerToken, pricing.OutputPricePerTokenPriority, *iv.OutputPrice)
		pricing.OutputPricePerToken = *iv.OutputPrice
	} else if iv.OutputMultiplier != nil {
		pricing.OutputPricePerToken = applyMultiplier(pricing.OutputPricePerToken, iv.OutputMultiplier)
		pricing.OutputPricePerTokenPriority = applyMultiplier(pricing.OutputPricePerTokenPriority, iv.OutputMultiplier)
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerTokenPriority = channelTierOverridePrice(pricing.CacheCreationPricePerToken, pricing.CacheCreationPricePerTokenPriority, *iv.CacheWritePrice)
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		if iv.CacheWrite1hPrice == nil {
			pricing.CacheCreation1hPrice = *iv.CacheWritePrice
		}
	} else if iv.CacheWriteMultiplier != nil {
		pricing.CacheCreationPricePerToken = applyMultiplier(pricing.CacheCreationPricePerToken, iv.CacheWriteMultiplier)
		pricing.CacheCreationPricePerTokenPriority = applyMultiplier(pricing.CacheCreationPricePerTokenPriority, iv.CacheWriteMultiplier)
		pricing.CacheCreation5mPrice = applyMultiplier(pricing.CacheCreation5mPrice, iv.CacheWriteMultiplier)
		pricing.CacheCreation1hPrice = applyMultiplier(pricing.CacheCreation1hPrice, iv.CacheWriteMultiplier)
	}
	if iv.CacheWrite1hPrice != nil {
		enableCacheCreationBreakdown(pricing)
		pricing.CacheCreation1hPrice = *iv.CacheWrite1hPrice
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerTokenPriority = channelTierOverridePrice(pricing.CacheReadPricePerToken, pricing.CacheReadPricePerTokenPriority, *iv.CacheReadPrice)
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
	} else if iv.CacheReadMultiplier != nil {
		pricing.CacheReadPricePerToken = applyMultiplier(pricing.CacheReadPricePerToken, iv.CacheReadMultiplier)
		pricing.CacheReadPricePerTokenPriority = applyMultiplier(pricing.CacheReadPricePerTokenPriority, iv.CacheReadMultiplier)
	}
	// 区间不携带图片价，沿用渠道级配置；渠道留空时保留基础价里的目录图片价。
	applyChannelImagePriceOverrides(chPricing, pricing)
	return pricing
}

// GetRequestTierPrice 根据层级标签获取按次价格
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	price, _ := requestTierPrice(resolved, tierLabel)
	return price
}

func requestTierPrice(resolved *ResolvedPricing, tierLabel string) (float64, bool) {
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == tierLabel && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPriceByContext 根据 context token 数获取按次价格
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	price, _ := requestTierPriceByContext(resolved, totalContextTokens)
	return price
}

func requestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) (float64, bool) {
	iv := FindMatchingInterval(resolved.RequestTiers, totalContextTokens)
	if iv != nil && iv.PerRequestPrice != nil {
		return *iv.PerRequestPrice, true
	}
	return 0, false
}

func defaultRequestPrice(resolved *ResolvedPricing) (float64, bool) {
	if resolved == nil {
		return 0, false
	}
	return resolved.DefaultPerRequestPrice, resolved.defaultPerRequestPriceExplicit || resolved.DefaultPerRequestPrice > 0
}

// preferredConfiguredPublicPricingModel preserves an explicit retail alias
// price across routing/account mappings. An explicitly selected channel billing
// source keeps its contract; an actual channel mapping may choose another card.
func preferredConfiguredPublicPricingModel(ctx context.Context, resolver *ModelPricingResolver, group *Group, publicModel, source string, routedModel ...string) string {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.legacyPriceSelection {
		return ""
	}
	publicModel = strings.TrimSpace(publicModel)
	if resolver == nil || group == nil || publicModel == "" {
		return ""
	}
	var mapping ChannelMappingResult
	channelInput := publicModel
	if len(routedModel) > 0 && routedModel[0] != "" {
		channelInput = routedModel[0]
	}
	if routeModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		channelInput = routeModel
	}
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.routingModel != "" {
		channelInput = snapshot.routingModel
	}
	if resolver.channelService != nil {
		mapping = resolver.channelService.ResolveChannelMapping(ctx, group.ID, channelInput)
	}
	if source == "" {
		source = mapping.BillingModelSource
	}
	if source == BillingModelSourceUpstream || source == BillingModelSourceResponse ||
		(source == BillingModelSourceChannelMapped && mapping.Mapped && !strings.EqualFold(mapping.MappedModel, channelInput)) {
		return ""
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: publicModel, Group: group, GroupID: &group.ID})
	if resolved == nil || (resolved.Source != PricingSourceGroup && resolved.Source != PricingSourceChannel) {
		return ""
	}
	if resolved.Mode == BillingModeToken {
		if resolved.BasePricing != nil && hasFlatTokenPrice(resolved.channelPricing) {
			return publicModel
		}
		for i := range resolved.Intervals {
			if hasIntervalTokenPrice(&resolved.Intervals[i]) {
				return publicModel
			}
		}
		return ""
	}
	if _, priced := maxPerRequestPrice(resolved); priced {
		return publicModel
	}
	return ""
}
