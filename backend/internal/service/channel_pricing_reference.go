package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	geminicli "github.com/LuckyKuang/sub2api-plus/internal/pkg/gemini"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	openai "github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"go.uber.org/zap"
)

// 渠道参考价服务：管理端「单模型查价」与「同步最新模型」必须共用同一套判定，
// 否则同一型号会在两条路径上给出不同价格（历史上 Sol/Luna 就因此出现过
// 同步列表里有、建规则时却查不到价的割裂）。
//
// 与请求计费路径的关键区别：这里**不允许**按家族子串猜价。热路径为了不中断
// 计费可以容忍 getFallbackPricing 的 "opus"→claude-opus-5 这类宽匹配，管理端
// 参考价一旦宽匹配就会把错价写进运营者配置，因此只接受：
//
//	精确目录项 → 已登记同 SKU 别名 → 同型号内置 fallback → 显式代理参考价
//
// 全都落空时返回 manual_required / unsupported_unit，绝不返回相似型号的价。

// ChannelPricingReferenceStatus 是参考价解析结果状态。
type ChannelPricingReferenceStatus string

const (
	// ChannelPricingStatusPriced 有可安全应用的完整价卡。
	ChannelPricingStatusPriced ChannelPricingReferenceStatus = "priced"
	// ChannelPricingStatusManualRequired 目录与内置兜底都没有该型号的价。
	ChannelPricingStatusManualRequired ChannelPricingReferenceStatus = "manual_required"
	// ChannelPricingStatusUnsupportedUnit 该型号只有渠道计价表单无法表达的单位
	// （音频秒/字符等），不能伪造成全零 token 价卡。
	ChannelPricingStatusUnsupportedUnit ChannelPricingReferenceStatus = "unsupported_unit"
)

// 参考价来源枚举。
const (
	ChannelPricingSourceReleaseCatalog  = "release_catalog"
	ChannelPricingSourceBuiltinFallback = "builtin_fallback"
	ChannelPricingSourceProxyReference  = "proxy_reference"
	ChannelPricingSourceNone            = "none"
)

// 稳定 reason code：前端 i18n 与运维排障都按 key 取值，不能改成自由文本。
const (
	ReasonExactPriceUnavailable       = "exact_price_unavailable"
	ReasonProviderPriceUnpublished    = "provider_price_unpublished"
	ReasonUnsupportedBillingDimension = "unsupported_billing_dimension"
	ReasonPlatformModelUnsupported    = "platform_model_unsupported"
	ReasonCatalogUnavailable          = "catalog_unavailable"
)

// 刷新状态枚举。
const (
	RefreshStatusRefreshed = "refreshed"
	RefreshStatusCurrent   = "current"
	RefreshStatusStale     = "stale"
	RefreshStatusDisabled  = "disabled"
)

// 稳定 warning code。
const (
	WarningCatalogRefreshFailed = "CATALOG_REFRESH_FAILED"
)

// 平台与 LiteLLM 目录 provider 标签的对应关系。同一平台允许有多个标签
// （Gemini 的 SKU 同时存在于 gemini 与 Vertex 标签下），但标签只用于「校验目录项
// 是否属于本平台」；平台支持哪些模型由 platformSupportedModels 决定。
//
// 标签必须与随仓库发布的 model_prices_and_context_window.json 实际值一致：
// Gemini 的 Vertex 行使用的是 vertex_ai-language-models /
// vertex_ai-embedding-models，写成裸 vertex_ai 会一条都匹配不上（平台列表看似
// 正常、参考价却全都落到 fallback）。embedding 标签只给 gemini：平台确有
// embedding SKU 且表单能表达其 token 价；antigravity 不转发 embedding 端点，
// 按端点能力逐项确认后不纳入。
//
// OpenCode Go 是聚合网关，按模型把请求分流到上游原生端点，因此它转发的型号
// 与上游同 SKU 同价：目录标签取各被聚合上游的并集。只写 opencode-go 会让
// 仅有上游目录行的型号（如 grok-4.7）在该平台掉成 manual_required。
var channelPricingCatalogProviders = map[string][]string{
	PlatformAnthropic:   {"anthropic"},
	PlatformOpenAI:      {"openai"},
	PlatformGemini:      {"gemini", "vertex_ai-language-models", "vertex_ai-embedding-models"},
	PlatformAntigravity: {"anthropic", "gemini", "vertex_ai-language-models"},
	PlatformGrok:        {"xai"},
	PlatformKimi:        {"moonshot"},
	PlatformZhipu:       {"zhipu"},
	PlatformDeepseek:    {"deepseek"},
	PlatformMiniMax:     {"minimax"},
	PlatformOpenCodeGo: {"openai", "anthropic", "gemini", "vertex_ai-language-models",
		"xai", "moonshot", "zhipu", "deepseek", "minimax", "opencode-go"},
}

// ChannelPricingReference 是单个模型的参考价解析结果。
type ChannelPricingReference struct {
	Model        string                        `json:"model"`
	MatchedModel string                        `json:"matched_model"`
	Platform     string                        `json:"platform"`
	Status       ChannelPricingReferenceStatus `json:"status"`
	Source       string                        `json:"source"`
	ReasonCode   string                        `json:"reason_code"`
	Pricing      *ChannelModelPricing          `json:"pricing"`
}

// ChannelPricingSyncSnapshot 是一次刷新的完整结果。
// Models 必须全部来自同一次快照，不能逐模型跨目录版本读取。
type ChannelPricingSyncSnapshot struct {
	Platform       string                    `json:"platform"`
	RefreshStatus  string                    `json:"refresh_status"`
	CatalogVersion string                    `json:"catalog_version"`
	LastUpdated    string                    `json:"last_updated"`
	WarningCode    string                    `json:"warning_code"`
	Models         []ChannelPricingReference `json:"models"`
}

// ErrUnsupportedPlatform 表示请求的平台没有参考价规则。这是调用方参数问题
// （400），不能和「目录不可用」（503）混用，否则前端会把参数错误当成服务故障。
var ErrUnsupportedPlatform = errors.New("unsupported platform")

// ChannelPricingReferenceService 提供平台感知的渠道参考价查询。
type ChannelPricingReferenceService struct {
	pricing *PricingService
	billing *BillingService

	// mu 串行化刷新：多个管理员同时同步不得并发装入不一致快照。
	mu sync.Mutex
}

func NewChannelPricingReferenceService(pricing *PricingService, billing *BillingService) *ChannelPricingReferenceService {
	return &ChannelPricingReferenceService{pricing: pricing, billing: billing}
}

// SupportedPlatforms 返回支持参考价查询的平台列表。
func (s *ChannelPricingReferenceService) SupportedPlatforms() []string {
	platforms := make([]string, 0, len(channelPricingCatalogProviders))
	for platform := range channelPricingCatalogProviders {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return platforms
}

// List 列出平台支持模型及其参考价，全部来自同一次目录快照。
func (s *ChannelPricingReferenceService) List(ctx context.Context, platform string) ([]ChannelPricingReference, error) {
	if !s.supportedPlatform(platform) {
		s.logEvent(ctx, "list", platform, zap.String("error_code", "UNSUPPORTED_PLATFORM"))
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, platform)
	}
	models := s.platformModels(platform)
	refs := make([]ChannelPricingReference, 0, len(models))
	for _, model := range models {
		refs = append(refs, s.resolve(platform, model))
	}
	s.logEvent(ctx, "list", platform, zap.Int("model_count", len(refs)))
	return refs, nil
}

// Resolve 解析单个模型的参考价。
func (s *ChannelPricingReferenceService) Resolve(ctx context.Context, platform, model string) (ChannelPricingReference, error) {
	if !s.supportedPlatform(platform) {
		s.logEvent(ctx, "resolve", platform, zap.String("error_code", "UNSUPPORTED_PLATFORM"))
		return ChannelPricingReference{}, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, platform)
	}
	ref := s.resolve(platform, strings.TrimSpace(model))
	// 只记录结果分布，不记录模型之外的调用方字段；模型名是管理员主动输入，
	// 记录它才能把「为什么这个型号没有价」和请求对应上。
	s.logEvent(ctx, "resolve", platform,
		zap.String("model", ref.Model),
		zap.String("status", string(ref.Status)),
		zap.String("source", ref.Source),
		zap.String("reason_code", ref.ReasonCode),
	)
	return ref, nil
}

// RefreshAndList 先按现有 Release manifest/摘要/版本链路刷新目录，再列出平台模型。
// 刷新失败时保留最后可用快照并标记 stale；完全无快照才报错，由 handler 转 503。
func (s *ChannelPricingReferenceService) RefreshAndList(ctx context.Context, platform string) (ChannelPricingSyncSnapshot, error) {
	if !s.supportedPlatform(platform) {
		s.logEvent(ctx, "refresh", platform, zap.String("error_code", "UNSUPPORTED_PLATFORM"))
		return ChannelPricingSyncSnapshot{}, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, platform)
	}
	status, warning := s.refreshCatalog(ctx)
	models, err := s.List(ctx, platform)
	if err != nil {
		return ChannelPricingSyncSnapshot{}, err
	}
	if status == RefreshStatusStale && len(models) == 0 {
		s.logEvent(ctx, "refresh", platform,
			zap.String("error_code", "PRICING_CATALOG_UNAVAILABLE"),
			zap.String("refresh_status", status))
		return ChannelPricingSyncSnapshot{}, fmt.Errorf("platform %s has no usable pricing snapshot", platform)
	}
	snapshot := ChannelPricingSyncSnapshot{
		Platform:      platform,
		RefreshStatus: status,
		WarningCode:   warning,
		Models:        models,
	}
	if s.pricing != nil {
		stats := s.pricing.GetStatus()
		snapshot.CatalogVersion = fmt.Sprintf("%v", stats["local_hash"])
		if updated, ok := stats["last_updated"].(interface{ IsZero() bool }); ok && !updated.IsZero() {
			snapshot.LastUpdated = fmt.Sprintf("%v", stats["last_updated"])
		}
	}
	s.logEvent(ctx, "refresh", platform,
		zap.String("refresh_status", snapshot.RefreshStatus),
		zap.String("warning_code", snapshot.WarningCode),
		zap.String("catalog_version", snapshot.CatalogVersion),
		zap.Int("model_count", len(snapshot.Models)),
	)
	return snapshot, nil
}

// logEvent 统一结构化日志：只记录 request ID、platform、stage、稳定错误码、
// 目录版本/条数与解析结果，不记录模型输入之外的调用方字段或凭据。
// 免费文本错误一律经由稳定 error_code 表达，禁止把原始 error 直接打日志。
func (s *ChannelPricingReferenceService) logEvent(ctx context.Context, stage, platform string, fields ...zap.Field) {
	base := []zap.Field{
		zap.String("component", "service.channel_pricing_reference"),
		zap.String("stage", stage),
		zap.String("platform", platform),
	}
	if ctx != nil {
		if requestID, ok := ctx.Value(ctxkey.RequestID).(string); ok && requestID != "" {
			base = append(base, zap.String("request_id", requestID))
		}
	}
	logger.With(append(base, fields...)...).Info("channel pricing reference event")
}

func (s *ChannelPricingReferenceService) supportedPlatform(platform string) bool {
	_, ok := channelPricingCatalogProviders[platform]
	return ok
}

// refreshCatalog 只调用已有的 syncWithRemote 链路：不允许绕过允许主机策略、
// 代理策略或已验证缓存。返回刷新状态与稳定 warning code。
// ctx 可为 nil（测试/内部调用）；生产路径由 handler 传入 gin 请求 context。
func (s *ChannelPricingReferenceService) refreshCatalog(ctx context.Context) (string, string) {
	if s.pricing == nil || !s.pricing.remoteSyncEnabled() {
		return RefreshStatusDisabled, ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.catalogFingerprint()
	if err := s.pricing.ForceUpdate(); err != nil {
		// 快照仍在：沿用旧目录并告知前端 stale，不修改任何已保存渠道。
		// 只记录稳定 warning code，不落原始 error 文本。
		s.logEvent(ctx, "refresh", "release", zap.String("warning_code", WarningCatalogRefreshFailed))
		logger.LegacyPrintf("service.pricing", "[ChannelPricing] release refresh failed, keeping last snapshot: %v", err)
		return RefreshStatusStale, WarningCatalogRefreshFailed
	}
	if s.catalogFingerprint() == before {
		return RefreshStatusCurrent, ""
	}
	return RefreshStatusRefreshed, ""
}

func (s *ChannelPricingReferenceService) catalogFingerprint() string {
	if s.pricing == nil {
		return ""
	}
	stats := s.pricing.GetStatus()
	return fmt.Sprintf("%v|%v|%v", stats["model_count"], stats["last_updated"], stats["local_hash"])
}

// platformModels 由「平台支持模型清单」与「目录 provider 行」取并集。
// 只扫 provider 行会让 OpenCode Go（目录无 opencode-go 行）与 Kimi/智谱/MiniMax
// （型号只存在于内置兜底）同步出空列表；只扫平台清单又会在目录新增 SKU 时漏掉。
//
// 聚合平台例外：OpenCode Go 只服务自己发布的型号清单，上游目录行（openai /
// anthropic / ... 的整个宇宙）可以给清单内型号查价，但不能把清单撑大，
// 否则运营者会在该平台看到一堆网关并不转发的型号。
func (s *ChannelPricingReferenceService) platformModels(platform string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	add := func(model string) {
		model = strings.ToLower(strings.TrimSpace(model))
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}

	for _, model := range platformSupportedModels(platform) {
		add(model)
	}
	if s.pricing != nil {
		for _, provider := range listExpansionProviders(platform) {
			for _, model := range s.pricing.ListModelNamesByProvider(provider) {
				add(model)
			}
		}
	}
	sort.Strings(models)
	return models
}

// platformOwnCatalogProviders 登记「只属于该平台自身」的目录标签：只有这些
// 标签的行参与平台模型清单扩充。未登记的平台默认用全部 price 标签
// （这些平台的目录行就是平台自身的模型宇宙，如 Gemini 账号实际可用的 SKU）。
var platformOwnCatalogProviders = map[string][]string{
	// OpenCode Go 是聚合网关：支持的模型以网关发布清单（DefaultOpenCodeGoModelIDs）
	// 为准；opencode-go 标签今天没有行，保留它是为了将来网关自有 SKU 能进清单。
	PlatformOpenCodeGo: {"opencode-go"},
}

func listExpansionProviders(platform string) []string {
	if own, ok := platformOwnCatalogProviders[platform]; ok {
		return own
	}
	return channelPricingCatalogProviders[platform]
}

// resolve 按严格优先级解析单个型号。
func (s *ChannelPricingReferenceService) resolve(platform, model string) ChannelPricingReference {
	ref := ChannelPricingReference{
		Model:        model,
		MatchedModel: model,
		Platform:     platform,
		Source:       ChannelPricingSourceNone,
	}
	if model == "" {
		ref.Status = ChannelPricingStatusManualRequired
		ref.ReasonCode = ReasonPlatformModelUnsupported
		return ref
	}

	// 1. 精确目录项（provider 标签必须属于本平台）。
	if catalog, matched := s.exactCatalogEntry(platform, model); catalog != nil {
		// 目录里有价但单位无法表达（音频/实时）时，宁可 unsupported 也不能
		// 套一张 token 价卡——那会把按音频 token/秒计费的型号按文本价卖。
		if unrepresentableBillingUnit(catalog) {
			ref.MatchedModel = matched
			ref.Status = ChannelPricingStatusUnsupportedUnit
			ref.ReasonCode = ReasonUnsupportedBillingDimension
			return ref
		}
		ref.MatchedModel = matched
		return s.buildPricedReference(ref, catalog, ChannelPricingSourceReleaseCatalog)
	}

	// 2. 已登记的媒体型号内置价（Grok Imagine 图片/视频分档）。
	// 放在 builtin token fallback 之前：媒体型号绝不能落到 unknown-text 兜底。
	if card, matched, ok := s.declaredMediaReference(platform, model); ok {
		ref.MatchedModel = matched
		ref.Source = ChannelPricingSourceBuiltinFallback
		ref.Status = ChannelPricingStatusPriced
		ref.Pricing = card
		return ref
	}

	// 3. 同型号内置 fallback（已登记的同 SKU 别名/精确 key，不含家族子串）。
	// sameModelFallbackCard 已带完整字段 presence，直接作为价卡返回。
	if fallback, matched, ok := s.sameModelFallback(platform, model); ok {
		ref.MatchedModel = matched
		ref.Source = ChannelPricingSourceBuiltinFallback
		ref.Status = ChannelPricingStatusPriced
		ref.Pricing = fallback
		return ref
	}

	ref.Status = ChannelPricingStatusManualRequired
	ref.ReasonCode = ReasonExactPriceUnavailable
	return ref
}

// exactCatalogEntry 只在「条目确实属于本平台」时返回。
// 用精确 key 查表（LookupExactCatalogEntry）而不是 GetIdentifiedModelPricing：
// 后者内部还有一层归一，命中的 candidate 仍是请求名，matched_model 就退化成
// 请求名，运营者看不出价来自哪个 SKU。候选 key 由 modelLookupCandidates 显式
// 生成（含已登记同 SKU 别名），保证「匹配规则」与「登记规则」在同一处。
func (s *ChannelPricingReferenceService) exactCatalogEntry(platform, model string) (*LiteLLMModelPricing, string) {
	if s.pricing == nil {
		return nil, ""
	}
	for _, candidate := range modelLookupCandidates(model) {
		entry := s.pricing.LookupExactCatalogEntry(candidate)
		if entry == nil {
			continue
		}
		if !s.providerAllowedForPlatform(platform, entry.LiteLLMProvider) {
			continue
		}
		return entry, candidate
	}
	return nil, ""
}

func (s *ChannelPricingReferenceService) providerAllowedForPlatform(platform, provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, allowed := range channelPricingCatalogProviders[platform] {
		if provider == allowed {
			return true
		}
	}
	return false
}

// sameModelFallback 只接受同型号内置价：拒绝 "claude-*" → Opus、未知 "gpt-*"
// → 默认测试模型、Kimi/GLM/MiniMax/Grok 家族子串这类宽匹配。
// 内置兜底表本身不带平台字段，因此还要模型确实属于被请求平台的家族，否则
// openai 渠道会拿到 claude-* 的内置价、anthropic 渠道会拿到 gpt-* 的内置价。
func (s *ChannelPricingReferenceService) sameModelFallback(platform, model string) (*ChannelModelPricing, string, bool) {
	if s.billing == nil {
		return nil, "", false
	}
	if !builtinFallbackBelongsToPlatform(platform, model) {
		return nil, "", false
	}
	billing, matched := s.billing.IdentifiedSameModelPricing(model)
	if billing == nil {
		return nil, "", false
	}
	card := sameModelFallbackCard(platform, model, billing)
	if card == nil {
		return nil, "", false
	}
	if matched == "" {
		matched = strings.ToLower(strings.TrimSpace(model))
	}
	return card, matched, true
}

// sameModelFallbackCard 把内置兜底价换算成完整渠道价卡。
//
// 字段 presence 规则（design：「fallback 注册信息必须同时声明字段 presence」）：
// 内置表用 0 表示「没有维护该字段」，因此 >0 才算有价；glm-4.5-flash 这类
// 显式免费条目 input/output 保持 0，而缓存等未维护字段保持 null——不能把
// 「没有该字段」渲染成 0 元。目录侧 presence 由 JSON 字段存在性决定
// （buildPricedReference），与这里互不影响。
func sameModelFallbackCard(platform, model string, billing *ModelPricing) *ChannelModelPricing {
	if billing == nil {
		return nil
	}
	card := &ChannelModelPricing{
		Platform:  platform,
		Models:    []string{model},
		Intervals: []PricingInterval{},
	}
	// 内置 token 卡一定带 input/output（免费条目显式为 0）。
	card.BillingMode = BillingModeToken
	card.InputPrice = floatRef(billing.InputPricePerToken)
	card.OutputPrice = floatRef(billing.OutputPricePerToken)
	if billing.CacheCreationPricePerToken > 0 {
		card.CacheWritePrice = floatRef(billing.CacheCreationPricePerToken)
	}
	// 5m/1h 拆分：5m 即 base cache write，1h 单独一个字段。
	if billing.SupportsCacheBreakdown && billing.CacheCreation5mPrice > 0 {
		card.CacheWritePrice = floatRef(billing.CacheCreation5mPrice)
		if billing.CacheCreation1hPrice > 0 {
			card.CacheWrite1hPrice = floatRef(billing.CacheCreation1hPrice)
		}
	}
	if billing.CacheReadPricePerToken > 0 {
		card.CacheReadPrice = floatRef(billing.CacheReadPricePerToken)
	}
	// Fast/priority 档存在时给出档倍率，让渠道侧沿用既有档位语义。
	if billing.InputPricePerTokenPriority > 0 && billing.InputPricePerToken > 0 {
		card.FastMultiplier = floatRef(billing.InputPricePerTokenPriority / billing.InputPricePerToken)
	}
	if billing.LongContextInputThreshold > 0 &&
		(billing.LongContextInputMultiplier > 1 || billing.LongContextOutputMultiplier > 1) {
		card.Intervals = append(card.Intervals, builtinLongContextInterval(billing))
	}
	return card
}

// unrepresentableBillingUnit 判断一条目录记录是否「有价但渠道计价表单无法表达」。
//
// 两类命中：
//  1. mode 为 audio_*/realtime：真实计费按音频 token / 秒，表单只有 token、
//     图片、按次三种维度。即使目录同时给了文本 token 价，套上去也会把音频
//     型号按文本价卖给运营者，因此必须 unsupported 而不是 priced。
//  2. 既没有 token 价也没有图片价（TokenPricingAbsent 且无 per-image /
//     image-token 价）：字符/秒等纯寄存器单位，伪造全零 token 价卡是假成功。
func unrepresentableBillingUnit(entry *LiteLLMModelPricing) bool {
	if entry == nil {
		return false
	}
	mode := strings.ToLower(entry.Mode)
	if strings.Contains(mode, "audio") || mode == "realtime" {
		return true
	}
	if entry.TokenPricingAbsent && entry.OutputCostPerImage == 0 &&
		entry.OutputCostPerImageToken == 0 && entry.InputCostPerImageToken == 0 {
		return true
	}
	return false
}

// declaredMediaReference 返回已登记的媒体型号参考价卡（Grok Imagine 系列）。
//
// 这些型号不在 LiteLLM 目录里，价格由项目按 xAI 官方分档维护；换算所用量价
// 与计费热路径的 getDefaultGrokImagineImagePrice / getDefaultGrokImagineVideoPrice
// 完全一致，这里只做「渠道价卡」形态转换，不新造价格。缺少这一层时，
// grok-imagine-* 会掉进 unknown-text 兜底拿到一张文本 token 价卡。
// source 记 builtin_fallback：项目维护、与请求型号同型号。
func (s *ChannelPricingReferenceService) declaredMediaReference(platform, model string) (*ChannelModelPricing, string, bool) {
	if platform != PlatformGrok {
		return nil, "", false
	}
	spec, ok := grokMediaReferenceSpec(model)
	if !ok {
		return nil, "", false
	}
	card := &ChannelModelPricing{
		Platform:        platform,
		Models:          []string{spec.model},
		BillingMode:     spec.mode,
		PerRequestPrice: floatRef(spec.basePrice),
		Intervals:       make([]PricingInterval, 0, len(spec.tiers)),
	}
	for _, tier := range spec.tiers {
		card.Intervals = append(card.Intervals, PricingInterval{
			TierLabel:       tier.label,
			PerRequestPrice: floatRef(tier.price),
		})
	}
	return card, spec.model, true
}

// grokMediaReference 是一条媒体型号的完整分档参考价。
type grokMediaReference struct {
	model     string
	mode      BillingMode
	basePrice float64 // 最低档（图片 1K / 视频 480p）按次价
	tiers     []grokMediaTier
}

type grokMediaTier struct {
	label string
	price float64
}

// grokMediaReferenceSpecs 按型号登记 Grok Imagine 图片/视频分档价。
// 图片 4K 与 2K 同价（与 getGrokImagineImageTierPrice 一致）：渠道规则缺 4K
// 档时按次计费会回落到 1K 平价，必须显式登记该档。
var grokMediaReferenceSpecs = map[string]grokMediaReference{
	xai.DefaultImagineImageFastModel: imageReferenceSpec(defaultGrokImagineImagePrice1K, defaultGrokImagineImagePrice2K),
	"grok-imagine":                   imageReferenceSpec(defaultGrokImagineImagePrice1K, defaultGrokImagineImagePrice2K),
	"grok-imagine-edit":              imageReferenceSpec(defaultGrokImagineImagePrice1K, defaultGrokImagineImagePrice2K),
	xai.DefaultImagineImageQualityModel: imageReferenceSpec(
		defaultGrokImagineImageQualityPrice1K, defaultGrokImagineImageQualityPrice2K),
	xai.DefaultImagineImage20Model: imageReferenceSpec(
		defaultGrokImagineImage20Price1K, defaultGrokImagineImage20Price2K),
	xai.DefaultImagineVideoModel: videoReferenceSpec(
		defaultGrokImagineVideoPrice480P, defaultGrokImagineVideoPrice720P),
	xai.DefaultImagineVideo15Model: videoReferenceSpec(
		defaultGrokImagineVideo15Price480P, defaultGrokImagineVideo15Price720P, defaultGrokImagineVideo15Price1080P),
	xai.DefaultImagineVideo15LegacyModel: videoReferenceSpec(
		defaultGrokImagineVideo15Price480P, defaultGrokImagineVideo15Price720P, defaultGrokImagineVideo15Price1080P),
}

func imageReferenceSpec(price1K, price2K float64) grokMediaReference {
	return grokMediaReference{
		model:     "",
		mode:      BillingModeImage,
		basePrice: price1K,
		tiers: []grokMediaTier{
			{label: ImageBillingSize2K, price: price2K},
			{label: ImageBillingSize4K, price: price2K},
		},
	}
}

func videoReferenceSpec(prices ...float64) grokMediaReference {
	labels := []string{
		VideoBillingResolution480P,
		VideoBillingResolution720P,
		VideoBillingResolution1080P,
	}
	spec := grokMediaReference{mode: BillingModeVideo}
	if len(prices) > 0 {
		spec.basePrice = prices[0]
	}
	for i := 1; i < len(prices) && i < len(labels); i++ {
		spec.tiers = append(spec.tiers, grokMediaTier{label: labels[i], price: prices[i]})
	}
	return spec
}

// grokMediaReferenceSpec 按大小写/路径归一后的型号取登记项。
func grokMediaReferenceSpec(model string) (grokMediaReference, bool) {
	native := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(native, "/"); idx >= 0 {
		native = native[idx+1:]
	}
	spec, ok := grokMediaReferenceSpecs[native]
	if !ok {
		return grokMediaReference{}, false
	}
	spec.model = native
	return spec, true
}

// buildPricedReference 把目录/内置价换算成渠道可直接套用的完整价卡。
// null 表示源数据没有该字段，0 表示明确免费，两者不得混同。
func (s *ChannelPricingReferenceService) buildPricedReference(
	ref ChannelPricingReference,
	entry *LiteLLMModelPricing,
	source string,
) ChannelPricingReference {
	pricing := &ChannelModelPricing{
		Platform:    ref.Platform,
		Models:      []string{ref.Model},
		BillingMode: BillingModeToken,
		Intervals:   []PricingInterval{},
	}
	if entry != nil {
		pricing.InputPrice = floatRef(entry.InputCostPerToken)
		pricing.OutputPrice = floatRef(entry.OutputCostPerToken)
		pricing.CacheWritePrice = floatRef(entry.CacheCreationInputTokenCost)
		pricing.CacheWrite1hPrice = floatRefOrNil(entry.CacheCreationInputTokenCostAbove1hr)
		pricing.CacheReadPrice = floatRef(entry.CacheReadInputTokenCost)
		if entry.InputCostPerImageToken > 0 {
			pricing.ImageInputPrice = floatRef(entry.InputCostPerImageToken)
		}
		if entry.OutputCostPerImageToken > 0 {
			pricing.ImageOutputPrice = floatRef(entry.OutputCostPerImageToken)
		}
		// Fast/priority 目录价存在时给出档倍率，让渠道侧沿用既有档位语义。
		if entry.InputCostPerTokenPriority > 0 && entry.InputCostPerToken > 0 {
			pricing.FastMultiplier = floatRef(entry.InputCostPerTokenPriority / entry.InputCostPerToken)
		}
		// Flex 档沿用 serviceTierCostMultiplier 的 0.5 倍默认值，无需写入价卡。
		if entry.OutputCostPerImage > 0 {
			pricing.BillingMode = BillingModeImage
			pricing.PerRequestPrice = floatRef(entry.OutputCostPerImage)
		}
		if entry.LongContextInputTokenThreshold > 0 && entry.LongContextInputCostMultiplier > 1 {
			pricing.Intervals = append(pricing.Intervals, longContextReferenceInterval(entry))
		}
	}

	ref.Source = source
	ref.Status = ChannelPricingStatusPriced
	ref.Pricing = pricing
	return ref
}

// longContextReferenceInterval 把长上下文阈值折算成渠道 token 区间，使复制到
// 渠道规则后的计费结果与官方参考价一致——只返回基础 input/output 而丢掉长上下文
// 档位不算完成。
//
// 渠道区间语义是左开右闭 (min, max]：匹配 totalTokens > min。因此阈值语义必须
// 对齐计费侧：
//   - 严格大于（OpenAI/Anthropic：>272000 才进高档）：min=threshold，
//     272001 恰好落进该区间；min 若写成 272001 会漏掉 272001 本身。
//   - 达到即进（xAI：>=200000 进高档）：min=threshold-1。
func longContextReferenceInterval(entry *LiteLLMModelPricing) PricingInterval {
	threshold := entry.LongContextInputTokenThreshold
	inclusive := strings.EqualFold(entry.LiteLLMProvider, "xai")
	minTokens := threshold
	label := fmt.Sprintf(">%d", threshold)
	if inclusive {
		minTokens = threshold - 1
		label = fmt.Sprintf(">=%d", threshold)
	}
	inputMultiplier := entry.LongContextInputCostMultiplier
	outputMultiplier := entry.LongContextOutputCostMultiplier
	if inputMultiplier <= 0 {
		inputMultiplier = 1
	}
	if outputMultiplier <= 0 {
		outputMultiplier = 1
	}
	scaled := func(value float64, multiplier float64) *float64 {
		if value <= 0 {
			return nil
		}
		v := value * multiplier
		return &v
	}
	return PricingInterval{
		MinTokens:       minTokens,
		TierLabel:       label,
		InputPrice:      scaled(entry.InputCostPerToken, inputMultiplier),
		OutputPrice:     scaled(entry.OutputCostPerToken, outputMultiplier),
		CacheWritePrice: scaled(entry.CacheCreationInputTokenCost, inputMultiplier),
		CacheReadPrice:  scaled(entry.CacheReadInputTokenCost, inputMultiplier),
	}
}

// builtinLongContextInterval 是内置兜底价卡的长上下文档位折算，阈值语义与
// longContextReferenceInterval 相同；内置表只有 xAI 兜底带 inclusive 语义。
func builtinLongContextInterval(billing *ModelPricing) PricingInterval {
	threshold := billing.LongContextInputThreshold
	minTokens := threshold
	label := fmt.Sprintf(">%d", threshold)
	if billing.LongContextThresholdInclusive {
		minTokens = threshold - 1
		label = fmt.Sprintf(">=%d", threshold)
	}
	inputMultiplier := billing.LongContextInputMultiplier
	outputMultiplier := billing.LongContextOutputMultiplier
	if inputMultiplier <= 0 {
		inputMultiplier = 1
	}
	if outputMultiplier <= 0 {
		outputMultiplier = 1
	}
	scaled := func(value float64, multiplier float64) *float64 {
		if value <= 0 {
			return nil
		}
		v := value * multiplier
		return &v
	}
	return PricingInterval{
		MinTokens:       minTokens,
		TierLabel:       label,
		InputPrice:      scaled(billing.InputPricePerToken, inputMultiplier),
		OutputPrice:     scaled(billing.OutputPricePerToken, outputMultiplier),
		CacheWritePrice: scaled(billing.CacheCreationPricePerToken, inputMultiplier),
		CacheReadPrice:  scaled(billing.CacheReadPricePerToken, inputMultiplier),
	}
}

func floatRef(value float64) *float64 {
	if value < 0 {
		value = 0
	}
	v := value
	return &v
}

func floatRefOrNil(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	v := value
	return &v
}

// modelLookupCandidates 复用 PricingService 的归一化思路（路径前缀、拼写变体、
// Gemini thinking 档位），不额外发明一套可能漂移的规则。
func modelLookupCandidates(model string) []string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 5)
	add := func(candidate string) {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	add(normalized)
	add(canonicalizeOpenAIModelAliasSpelling(normalized))
	for _, alias := range registeredSameSKUAliases(normalized) {
		add(alias)
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		add(normalized[idx+1:])
	}
	add(normalizeGeminiThinkingTierAlias(lastPricingSegment(normalized)))
	return out
}

// registeredSameSKUAliases 返回「已登记、可逆且确定性」的同 SKU 拼写：
// thinking 后缀只改变推理行为、不改变发布单价，因此可以确定性地映射回基名。
// 只在基名确实存在时才由调用方命中；未登记的后缀（-preview 之类）不做猜测。
// Grok 别名表与 getFallbackPricing 的登记保持一致：热路径把 4.20 日期变体和
// composer 系列归到同一张价卡，参考价若另行一套就会两条路径两个价。
func registeredSameSKUAliases(model string) []string {
	out := make([]string, 0, 2)
	// Claude 4.5 一代的 -thinking 变体与基名同 SKU 同价（扩展思考开关）。
	if strings.HasPrefix(model, "claude-") && strings.HasSuffix(model, "-thinking") {
		out = append(out, strings.TrimSuffix(model, "-thinking"))
	}
	// Antigravity 的 gemini-2.5-flash-thinking 同样映射到公开基名。
	if model == "gemini-2.5-flash-thinking" {
		out = append(out, "gemini-2.5-flash")
	}
	if alias, ok := grokSameSKUAliases[model]; ok {
		out = append(out, alias)
	}
	return out
}

// grokSameSKUAliases 是 xAI 已登记的同 SKU 拼写（日期快照/推理变体/composer
// 系列归到同一张官方价卡）。unknown-text 兜底（任意 grok-* → grok-4.6）不在表内：
// 那是防计费中断的宽匹配，不能当参考价。
var grokSameSKUAliases = map[string]string{
	"grok-4.20-0309-reasoning":     "grok-4.20",
	"grok-4.20-0309-non-reasoning": "grok-4.20",
	"grok-4.20-multi-agent-0309":   "grok-4.20",
	"grok-4.20-reasoning":          "grok-4.20",
	"grok-4.20-non-reasoning":      "grok-4.20",
	"grok-composer":                "grok-build-0.1",
	"grok-composer-2.5-fast":       "grok-build-0.1",
}

func lastPricingSegment(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// platformSupportedModels 是各平台支持模型清单的单一入口。
func platformSupportedModels(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0)
		for _, model := range geminicli.DefaultModels() {
			ids = append(ids, strings.TrimPrefix(model.Name, "models/"))
		}
		return ids
	case PlatformAntigravity:
		ids := make([]string, 0)
		for _, model := range antigravity.DefaultModels() {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformOpenCodeGo:
		return DefaultOpenCodeGoModelIDs()
	case PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax:
		return builtinFallbackFamilyModelIDs(platform)
	default: // PlatformAnthropic
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

// builtinFallbackFamilyPrefixes 是「平台 ↔ 内置兜底型号前缀」的单一事实来源，
// 同时驱动平台模型清单（builtinFallbackFamilyModelIDs）和平台归属校验
// （builtinFallbackBelongsToPlatform）。内置兜底表自身不带平台字段，没有这层
// 过滤就会把 claude-* 的内置价返回给 openai 渠道、或把 gpt-* 的价返回给 anthropic。
var builtinFallbackFamilyPrefixes = map[string][]string{
	PlatformAnthropic:   {"claude-"},
	PlatformOpenAI:      {"gpt-", "codex"},
	PlatformGemini:      {"gemini-"},
	PlatformAntigravity: {"claude-", "gemini-"},
	PlatformGrok:        {"grok-"},
	PlatformKimi:        {"kimi-", "k3"},
	PlatformZhipu:       {"glm-"},
	PlatformDeepseek:    {"deepseek-"},
	PlatformMiniMax:     {"minimax-"},
	// OpenCode 是聚合网关：同时转发上面多家上游的型号，因此并集各家前缀，
	// 再加上自身私有系列。
	PlatformOpenCodeGo: {"claude-", "gpt-", "gemini-", "grok-", "codex",
		"kimi-", "k3", "glm-", "deepseek-", "minimax-",
		"longcat-", "mimo-", "muse-spark-", "qwen", "hy", "omen"},
}

// builtinFallbackBelongsToPlatform 判断型号是否属于该平台维护的内置兜底。
// 只按前缀判定「归属」，不按前缀给价；实际价格仍由 IdentifiedSameModelPricing
// 的精确匹配决定。
func builtinFallbackBelongsToPlatform(platform, model string) bool {
	prefixes, ok := builtinFallbackFamilyPrefixes[platform]
	if !ok {
		return false
	}
	native := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(native, "/"); idx >= 0 {
		native = native[idx+1:]
	}
	for _, family := range []string{"claude-", "gpt-"} {
		if idx := strings.Index(native, family); idx > 0 {
			native = native[idx:]
			break
		}
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(native, prefix) {
			return true
		}
	}
	return false
}

// builtinFallbackFamilyModelIDs 返回平台在内置兜底表中维护的型号。
// Kimi/智谱/MiniMax/DeepSeek 的动态目录行不全甚至为空，但这些型号项目里有
// 精确内置价；同步时只扫 provider 行会让它们整个平台消失。
func builtinFallbackFamilyModelIDs(platform string) []string {
	ids := make([]string, 0)
	if billingReferenceFallbackIDs == nil {
		return ids
	}
	for id := range billingReferenceFallbackIDs() {
		if builtinFallbackBelongsToPlatform(platform, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// billingReferenceFallbackIDs 提供内置兜底表的型号清单。
// 由 init 阶段注入，避免参考价服务直接持有 BillingService 私有表。
var billingReferenceFallbackIDs func() map[string]struct{}

// IdentifiedSameModelPricing 只返回同型号（或已登记同 SKU 别名）的内置价。
//
// getFallbackPricing 为了让热路径不中断计费，会把任意含 "claude" 的名字归到
// Sonnet、任意含 "opus" 的名字归到某个 Opus 档。参考价如果复用那条链路，就会把
// 家族价当成型号价写进运营者配置，因此这里做严格匹配：
//
//   - 精确 key（大小写/空白归一后）
//   - 已登记拼写变体（gpt-5.6 ↔ gpt-5.6-sol、claude-opus-4-5 ↔ claude-opus-4.5）
//   - 已登记同 SKU 别名（claude-*-thinking、gemini-2.5-flash-thinking → 基名）
//   - gemini-*-tiered thinking 档 → 基名
//   - normalizeKnownOpenAICodexModel 归一后的同 SKU 名称
func (s *BillingService) IdentifiedSameModelPricing(model string) (*ModelPricing, string) {
	if s == nil {
		return nil, ""
	}
	lookup := strings.ToLower(strings.TrimSpace(model))
	if lookup == "" {
		return nil, ""
	}
	if pricing := s.fallbackPrices[lookup]; pricing != nil {
		return pricing, lookup
	}
	// 已登记拼写变体：小数点/连字符互换（claude-opus-4.5 ↔ claude-opus-4-5）。
	for _, variant := range []string{
		strings.ReplaceAll(lookup, ".", "-"),
		strings.ReplaceAll(lookup, "-5-5", "-5.5"),
	} {
		if variant == lookup {
			continue
		}
		if pricing := s.fallbackPrices[variant]; pricing != nil {
			return pricing, variant
		}
	}
	// 已登记同 SKU 别名：thinking 后缀只改变推理行为，单价与基名一致。
	for _, alias := range registeredSameSKUAliases(lookup) {
		if pricing := s.fallbackPrices[alias]; pricing != nil {
			return pricing, alias
		}
	}
	// Gemini thinking 档位（-high/-low/-medium/-tiered）与基名同价。
	if base := normalizeGeminiThinkingTierAlias(lastPricingSegment(lookup)); base != lookup {
		if pricing := s.fallbackPrices[base]; pricing != nil {
			return pricing, base
		}
	}
	// OpenAI/Codex 同 SKU 归一（gpt-5.6 → gpt-5.6-sol 等）。
	if normalized := normalizeKnownOpenAICodexModel(lookup); normalized != "" && normalized != lookup {
		if pricing := s.fallbackPrices[normalized]; pricing != nil {
			return pricing, normalized
		}
	}
	return nil, ""
}
