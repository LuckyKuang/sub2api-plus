package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
)

type billingPricingSnapshotCtxKey struct{}
type legacyBillingSettlementCtxKey struct{}
type billingRateIdentityCtxKey struct{}
type billingRateIdentity struct{ platform, model, baseUnit string }

// BillingPricingSnapshot pins every customer-pricing input, including the
// catalog and channel price cards, across retries and detached settlement.
// It contains no account credentials and is never exposed through an API DTO.
type BillingPricingSnapshot struct {
	group                *Group
	personalRate         *float64
	tokenPeak            *float64
	pricingAt            time.Time
	billing              *BillingService
	resolver             *ModelPricingResolver
	channels             *ChannelService
	platform             string
	publicModel          string
	routingModel         string
	rateCalculation      string
	peakScope            string
	legacyPriceSelection bool
	captureErr           error
	fastPolicySettings   *OpenAIFastPolicySettings
	fastExecutionGroup   *Group
}

func BillingPricingSnapshotFromContext(ctx context.Context) *BillingPricingSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot, _ := ctx.Value(billingPricingSnapshotCtxKey{}).(*BillingPricingSnapshot)
	return snapshot
}

func WithoutBillingPricingSnapshot(ctx context.Context) context.Context {
	// A new pricing generation must also recapture global Fast rules. Leaving
	// the inherited context policy would freeze the previous turn's settings.
	ctx = context.WithValue(ctx, openAIFastPolicyCtxKey, (*OpenAIFastPolicySettings)(nil))
	return context.WithValue(ctx, billingPricingSnapshotCtxKey{}, (*BillingPricingSnapshot)(nil))
}

func CopyBillingPricingSnapshot(parent, base context.Context) context.Context {
	if base == nil {
		base = context.Background()
	}
	// Ungrouped OpenAI requests have no retail snapshot. Their independently
	// captured policy is still part of the accepted pricing generation.
	if policy := openAIFastPolicySettingsFromContext(parent); policy != nil {
		base = withOpenAIFastPolicyContext(base, policy)
	}
	snapshot := BillingPricingSnapshotFromContext(parent)
	if routeModel, ok := ResolvedUpstreamModelFromContext(parent); snapshot != nil && snapshot.routingModel == "" && ok {
		copy := *snapshot
		copy.routingModel = routeModel
		snapshot = &copy
	}
	return pricingSnapshotContext(base, snapshot)
}

// BindBillingPricingModel adds identity learned from an authorized resource
// lookup without reloading prices or mutating a snapshot shared by other work.
func BindBillingPricingModel(ctx context.Context, platform, publicModel string) context.Context {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		copy := *snapshot
		if platform != "" {
			copy.platform = platform
		}
		if publicModel != "" {
			copy.publicModel = publicModel
		}
		ctx = pricingSnapshotContext(ctx, &copy)
	}
	ctx = context.WithValue(ctx, billingRateIdentityCtxKey{}, billingRateIdentity{platform: platform, model: publicModel})
	return ctx
}

func (snapshot *BillingPricingSnapshot) PricingAt() time.Time {
	if snapshot == nil {
		return time.Time{}
	}
	return snapshot.pricingAt
}

// ValidateBillingPricingSnapshot is called before reservation/account selection.
// Invalid effective factors must never become a free request or a failed ledger
// write after the provider has already served it.
func ValidateBillingPricingSnapshot(ctx context.Context) error {
	snapshot := BillingPricingSnapshotFromContext(ctx)
	if snapshot == nil {
		return nil
	}
	if snapshot.captureErr != nil {
		return snapshot.captureErr
	}
	if snapshot.group == nil {
		return fmt.Errorf("billing group snapshot unavailable")
	}
	if err := snapshot.group.BillingRatePolicy.Validate(); err != nil {
		return err
	}
	model := snapshot.publicModel
	if current, ok := RequestedPublicModelFromContext(ctx); model == "" && ok {
		model = current
	}
	for _, unit := range []string{"token", "image", "second", "request", "minute", "hour", "million_characters"} {
		quote := snapshot.quote(snapshot.platform, model, unit)
		if math.IsNaN(quote.EffectiveMultiplier) || math.IsInf(quote.EffectiveMultiplier, 0) || quote.EffectiveMultiplier < 0 || quote.EffectiveMultiplier > domain.MaxBillingRateMultiplier {
			return fmt.Errorf("effective billing multiplier is outside the supported range")
		}
	}
	for _, baseUnit := range []string{"image", "second"} {
		quote := snapshot.quoteWithBase(billingRateIdentity{platform: snapshot.platform, model: model, baseUnit: baseUnit}, "request")
		if !validCost(quote.EffectiveMultiplier) || quote.EffectiveMultiplier > domain.MaxBillingRateMultiplier {
			return fmt.Errorf("effective media billing multiplier is outside the supported range")
		}
	}
	return nil
}

func (s *GatewayService) WithBillingPricingSnapshot(ctx context.Context, key *APIKey, at time.Time, publicModel ...string) context.Context {
	if s == nil {
		return ctx
	}
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(s.userGroupRateRepo, s.userGroupRateCache, resolveUserGroupRateCacheTTL(s.cfg), &s.userGroupRateSF, "service.gateway")
	}
	channels := s.channelService
	if channels == nil && s.resolver != nil {
		channels = s.resolver.channelService
	}
	existing := BillingPricingSnapshotFromContext(ctx)
	ctx = withBillingPricingSnapshot(ctx, billingKeyWithDefaultRate(key, s.cfg), at, s.billingService, channels, resolver.ResolveFreshSnapshot, publicModel...)
	if existing == nil {
		ctx = freezeInflightFastPolicy(ctx, s.settingService)
	}
	return ctx
}

func (s *OpenAIGatewayService) WithBillingPricingSnapshot(ctx context.Context, key *APIKey, at time.Time, publicModel ...string) context.Context {
	if s == nil {
		return ctx
	}
	ctx = withOpenAIDefaultBillingPlatform(ctx, key)
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
	}
	channels := s.channelService
	if channels == nil && s.resolver != nil {
		channels = s.resolver.channelService
	}
	existing := BillingPricingSnapshotFromContext(ctx)
	ctx = withBillingPricingSnapshot(ctx, billingKeyWithDefaultRate(key, s.cfg), at, s.billingService, channels, resolver.ResolveFreshSnapshot, publicModel...)
	if existing == nil {
		ctx = freezeInflightFastPolicy(ctx, s.settingService)
	}
	return ctx
}

func cloneBillingGroup(group *Group) *Group {
	if group == nil {
		return nil
	}
	// Group is a small configuration entity; a JSON round trip also detaches
	// nested pointers/maps in custom cards, so later admin updates cannot leak in.
	pricingGroup := *group
	pricingGroup.AccountGroups = nil
	data, err := json.Marshal(&pricingGroup)
	if err != nil {
		return nil
	}
	var cloned Group
	if json.Unmarshal(data, &cloned) != nil {
		return nil
	}
	return &cloned
}

func snapshotBillingService(service *BillingService) *BillingService {
	if service == nil {
		return nil
	}
	// Price generations are immutable: reloaders publish a new map under mu.
	// Pinning that generation is O(1), instead of copying the full catalog for
	// every inference request. Compiled fallback cards never change at runtime.
	copy := &BillingService{cfg: service.cfg, fallbackPrices: service.fallbackPrices, frozenPricing: true}
	if service.pricingService != nil {
		service.pricingService.mu.RLock()
		copy.pricingService = &PricingService{pricingData: service.pricingService.pricingData, localHash: service.pricingService.localHash, localVersion: service.pricingService.localVersion, lastUpdated: service.pricingService.lastUpdated}
		service.pricingService.mu.RUnlock()
	}
	return copy
}

func withBillingPricingSnapshot(ctx context.Context, key *APIKey, at time.Time, billing *BillingService, channels *ChannelService, personal func(context.Context, int64, int64, float64) (float64, bool, error), publicModel ...string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		if routeModel, ok := ResolvedUpstreamModelFromContext(ctx); snapshot.routingModel == "" && ok {
			copy := *snapshot
			copy.routingModel = routeModel
			return pricingSnapshotContext(ctx, &copy)
		}
		return ctx
	}
	if key == nil || key.Group == nil {
		return ctx
	}
	sourceGroup := key.Group
	if groupMediaPricingLooksIncomplete(sourceGroup) && channels != nil && channels.groupRepo != nil && key.GroupID != nil {
		loaded, err := channels.groupRepo.GetByIDLite(ctx, *key.GroupID)
		if err != nil || loaded == nil {
			return pricingSnapshotContext(ctx, &BillingPricingSnapshot{captureErr: fmt.Errorf("billing group pricing snapshot unavailable")})
		}
		sourceGroup = loaded
	}
	if legacy, _ := ctx.Value(legacyBillingSettlementCtxKey{}).(bool); legacy {
		copy := *sourceGroup
		copy.BillingRatePolicy, copy.BillingPolicyVersion = domain.BillingRatePolicy{}, 0
		sourceGroup = &copy
	}
	group := cloneBillingGroup(sourceGroup)
	if group == nil {
		return context.WithValue(ctx, billingPricingSnapshotCtxKey{}, &BillingPricingSnapshot{captureErr: fmt.Errorf("billing group snapshot unavailable")})
	}
	if group.ID <= 0 && key.GroupID != nil {
		group.ID = *key.GroupID
	}
	if at.IsZero() {
		at = timezone.Now()
	}
	pinBillingComponentDefaults(group)
	snapshot := &BillingPricingSnapshot{group: group, pricingAt: at, billing: snapshotBillingService(billing), platform: requestBillingPlatform(ctx, group, "")}
	snapshot.routingModel, _ = ResolvedUpstreamModelFromContext(ctx)
	if legacy, _ := ctx.Value(legacyBillingSettlementCtxKey{}).(bool); legacy {
		snapshot.legacyPriceSelection = true
		snapshot.peakScope = BillingPeakScopeTokenOnly
	}
	peak := group.PeakMultiplierAt(at)
	snapshot.tokenPeak = &peak
	if snapshot.billing != nil {
		snapshot.billing.frozenPricingAt = at
	}
	snapshot.publicModel, _ = RequestedPublicModelFromContext(ctx)
	if len(publicModel) > 0 && publicModel[0] != "" {
		snapshot.publicModel = publicModel[0]
	}
	if key.User != nil && personal != nil {
		rate, overridden, err := personal(ctx, key.User.ID, group.ID, group.RateMultiplier)
		if err != nil {
			snapshot.captureErr = fmt.Errorf("billing personal rate snapshot unavailable: %w", err)
		}
		if overridden {
			snapshot.personalRate = &rate
		}
	}
	if channels != nil {
		cache, err := channels.loadCache(ctx)
		if err == nil && cache.loadFailed {
			err = fmt.Errorf("channel pricing cache unavailable")
		}
		if err != nil {
			snapshot.captureErr = fmt.Errorf("billing channel snapshot unavailable: %w", err)
		}
		if err == nil {
			local := &ChannelService{}
			// Cache generations are immutable, just like the model catalog. Keep
			// their exact platform index and custom lookup semantics.
			local.frozenCache = cache
			local.cache.Store(cache)
			snapshot.channels = local
			if snapshot.platform == "" {
				snapshot.platform = cache.groupPlatform[group.ID]
			}
		}
	}
	snapshot.resolver = NewModelPricingResolver(snapshot.channels, snapshot.billing)
	return context.WithValue(ctx, billingPricingSnapshotCtxKey{}, snapshot)
}

func billingKeyWithDefaultRate(key *APIKey, cfg *config.Config) *APIKey {
	if key == nil || key.Group == nil || key.GroupID != nil {
		return key
	}
	copy := *key
	group := *key.Group
	group.RateMultiplier = 1
	if cfg != nil && cfg.Default.RateMultiplier > 0 {
		group.RateMultiplier = cfg.Default.RateMultiplier
	}
	copy.Group = &group
	return &copy
}

// requestBillingPlatform uses resolved routing identity, never model-name guessing.
func requestBillingPlatform(ctx context.Context, group *Group, fallback string) string {
	if ctx != nil {
		if forced, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && forced != "" {
			return forced
		}
		if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
			return platform
		}
	}
	if group != nil && group.Platform != PlatformComposite {
		return group.Platform
	}
	return fallback
}

func publicBillingModel(ctx context.Context, original, fallback string) string {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.publicModel != "" {
		return snapshot.publicModel
	}
	if model, ok := RequestedPublicModelFromContext(ctx); ok {
		return model
	}
	if original != "" {
		return original
	}
	return fallback
}

func billingRateSnapshotMap(quote BillingRateQuote) map[string]any {
	return map[string]any{"platform": quote.Platform, "model": quote.Model, "billing_unit": quote.BillingUnit,
		"base_multiplier": quote.BaseMultiplier, "policy_multiplier": quote.PolicyMultiplier, "peak_multiplier": quote.PeakMultiplier,
		"platform_multiplier": quote.PlatformMultiplier, "model_multiplier": quote.ModelMultiplier, "rate_calculation": quote.RateCalculation, "peak_scope": quote.PeakScope,
		"effective_multiplier": quote.EffectiveMultiplier, "base_source": quote.BaseSource, "source": quote.Source,
		"pattern": quote.Pattern, "policy_version": quote.PolicyVersion, "as_of": quote.AsOf.UTC().Format(time.RFC3339Nano)}
}

func (snapshot *BillingPricingSnapshot) quote(platform, model, unit string) BillingRateQuote {
	quote := resolveBillingRateQuote(snapshot.group, platform, model, unit, snapshot.personalRate, snapshot.pricingAt, snapshot.calculation(), snapshot.peakMeterScope())
	if (snapshot.peakMeterScope() == BillingPeakScopeAllMeters || unit == "token") && snapshot.tokenPeak != nil {
		quote.PeakMultiplier = *snapshot.tokenPeak
		quote = quoteWithEffectiveRate(quote)
	}
	return quote
}

func (snapshot *BillingPricingSnapshot) peakMeterScope() string {
	if snapshot.peakScope == BillingPeakScopeTokenOnly {
		return BillingPeakScopeTokenOnly
	}
	return BillingPeakScopeAllMeters
}

func (snapshot *BillingPricingSnapshot) calculation() string {
	if snapshot.rateCalculation == BillingRateCalculationLegacyOverride {
		return BillingRateCalculationLegacyOverride
	}
	return BillingRateCalculationProduct
}

func billingServiceForContext(ctx context.Context, fallback *BillingService) *BillingService {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.billing != nil {
		return snapshot.billing
	}
	return fallback
}

func pricingResolverForContext(ctx context.Context, fallback *ModelPricingResolver) *ModelPricingResolver {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.resolver != nil {
		return snapshot.resolver
	}
	return fallback
}

func channelServiceForContext(ctx context.Context, fallback *ChannelService) *ChannelService {
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		return snapshot.channels
	}
	return fallback
}

func prepareUsagePricing(ctx context.Context, snapshot *BillingPricingSnapshot, key *APIKey, account *Account, original, model string) (context.Context, *APIKey) {
	if snapshot == nil {
		snapshot = BillingPricingSnapshotFromContext(ctx)
	}
	ctx = pricingSnapshotContext(ctx, snapshot)
	platform := ""
	if account != nil {
		platform = account.Platform
	}
	public := publicBillingModel(ctx, original, model)
	if snapshot != nil {
		if snapshot.platform != "" {
			platform = snapshot.platform
		}
		if snapshot.publicModel != "" {
			public = snapshot.publicModel
		}
		if key != nil {
			cloned := *key
			cloned.Group = snapshot.group
			key = &cloned
		}
	}
	if key != nil && (snapshot == nil || snapshot.platform == "") {
		platform = requestBillingPlatform(ctx, key.Group, platform)
	}
	ctx = context.WithValue(ctx, billingRateIdentityCtxKey{}, billingRateIdentity{platform: platform, model: public})
	if platform != "" {
		ctx = WithResolvedTargetPlatform(ctx, platform)
		if snapshot != nil && snapshot.platform != "" {
			ctx = context.WithValue(ctx, ctxkey.ForcePlatform, snapshot.platform)
		}
	}
	return ctx, key
}

func customerBillingRate(ctx context.Context, key *APIKey, unit string, fallback float64) float64 {
	if ctx == nil {
		ctx = context.Background()
	}
	identity, _ := ctx.Value(billingRateIdentityCtxKey{}).(billingRateIdentity)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		if identity.platform == "" {
			identity.platform = snapshot.platform
		}
		if identity.model == "" {
			identity.model = snapshot.publicModel
		}
		return snapshot.quoteWithBase(identity, unit).EffectiveMultiplier
	}
	if key == nil || key.Group == nil {
		return fallback
	}
	if identity.platform == "" {
		identity.platform = requestBillingPlatform(ctx, key.Group, "")
	}
	return fallback * key.Group.BillingRatePolicy.Resolve(identity.platform, identity.model, unit).Multiplier
}

func customerBillingQuote(ctx context.Context, key *APIKey, unit string, fallback float64, at time.Time) BillingRateQuote {
	if ctx == nil {
		ctx = context.Background()
	}
	identity, _ := ctx.Value(billingRateIdentityCtxKey{}).(billingRateIdentity)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		if identity.platform == "" {
			identity.platform = snapshot.platform
		}
		if identity.model == "" {
			identity.model = snapshot.publicModel
		}
		return snapshot.quoteWithBase(identity, unit)
	}
	var group *Group
	if key != nil {
		group = key.Group
	}
	quote := ResolveBillingRateQuote(group, identity.platform, identity.model, unit, nil, at)
	if group == nil {
		quote.BaseMultiplier, quote.BaseSource = fallback, "default"
	}
	quote.EffectiveMultiplier = fallback
	return quote
}

func withBillingBaseUnit(ctx context.Context, unit string) context.Context {
	identity, _ := ctx.Value(billingRateIdentityCtxKey{}).(billingRateIdentity)
	identity.baseUnit = unit
	return context.WithValue(ctx, billingRateIdentityCtxKey{}, identity)
}

func (snapshot *BillingPricingSnapshot) quoteWithBase(identity billingRateIdentity, unit string) BillingRateQuote {
	quote := snapshot.quote(identity.platform, identity.model, unit)
	if unit == "request" {
		media := snapshot.quote(identity.platform, identity.model, identity.baseUnit)
		if (identity.baseUnit == "image" || identity.baseUnit == "second") && media.BaseSource == "media" {
			quote.BaseMultiplier, quote.BaseSource = media.BaseMultiplier, media.BaseSource
			quote = quoteWithEffectiveRate(quote)
		}
	}
	return quote
}

func audioBillingUnit(mode string) string {
	switch mode {
	case "tts":
		return "million_characters"
	case "stt":
		return "hour"
	default:
		return "minute"
	}
}

func billingUnitForMode(mode string) string {
	switch mode {
	case "per_request":
		return "request"
	case "image":
		return "image"
	case "video":
		return "second"
	default:
		return "token"
	}
}

func applyUsageBillingRateSnapshot(ctx context.Context, log *UsageLog, key *APIKey, unit string, at time.Time) {
	quote := customerBillingQuote(ctx, key, unit, log.RateMultiplier, at)
	log.RateMultiplier = quote.EffectiveMultiplier
	log.BillingPolicyVersion = quote.PolicyVersion
	log.BillingRateSnapshot = billingRateSnapshotMap(quote)
	if legacy, _ := ctx.Value(legacyBillingSettlementCtxKey{}).(bool); legacy {
		log.BillingRateSnapshot["pricing_state"] = "legacy_current_prices"
	}
}

func appendUsageBillingComponent(ctx context.Context, log *UsageLog, key *APIKey, unit string, at time.Time) {
	if log.BillingRateSnapshot == nil || log.BillingRateSnapshot["billing_unit"] == unit {
		return
	}
	components, _ := log.BillingRateSnapshot["components"].([]map[string]any)
	for _, component := range components {
		if component["billing_unit"] == unit {
			return
		}
	}
	quote := customerBillingQuote(ctx, key, unit, log.RateMultiplier, at)
	log.BillingRateSnapshot["components"] = append(components, billingRateSnapshotMap(quote))
}

func pricingSnapshotContext(ctx context.Context, snapshot *BillingPricingSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil {
		return ctx
	}
	if snapshot.fastPolicySettings != nil {
		ctx = withOpenAIFastPolicyContext(ctx, snapshot.fastPolicySettings)
	}
	return context.WithValue(ctx, billingPricingSnapshotCtxKey{}, snapshot)
}

func billingKeyWithUser(key *APIKey, user *User) *APIKey {
	if key == nil || key.User != nil || user == nil {
		return key
	}
	copy := *key
	copy.User = user
	return &copy
}

func legacyBillingSettlementContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, legacyBillingSettlementCtxKey{}, true)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		if snapshot.group == nil {
			return WithoutBillingPricingSnapshot(ctx)
		}
		copy := *snapshot
		copy.group = cloneBillingGroup(snapshot.group)
		if copy.group == nil {
			return WithoutBillingPricingSnapshot(ctx)
		}
		copy.group.BillingRatePolicy, copy.group.BillingPolicyVersion = domain.BillingRatePolicy{}, 0
		ctx = pricingSnapshotContext(ctx, &copy)
	}
	return ctx
}

func validateUsageCost(cost *CostBreakdown) error {
	if cost == nil {
		return nil
	}
	if cost.pricingErr != nil {
		return cost.pricingErr
	}
	if !validCost(cost.TotalCost) || !validCost(cost.ActualCost) {
		return fmt.Errorf("billing cost is unavailable or outside the supported range")
	}
	return nil
}

func validateDurableBillingModel(ctx context.Context, model string) error {
	snapshot := BillingPricingSnapshotFromContext(ctx)
	if snapshot == nil || snapshot.billing == nil || snapshot.billing.frozenModelPrices == nil {
		return nil
	}
	if snapshot.billing.frozenMissingModels[strings.ToLower(strings.TrimSpace(model))] {
		return fmt.Errorf("billing model has no price evidence in the task snapshot")
	}
	if _, captured := snapshot.billing.frozenModelPrices[strings.ToLower(strings.TrimSpace(model))]; !captured {
		return fmt.Errorf("billing model is absent from the task pricing snapshot")
	}
	return nil
}

// ValidateBillingPricingMeter verifies the persisted price evidence before a
// deferred task consumes its settlement claim. Explicit zero cards are valid;
// absent cards cannot be reconstructed from current defaults after creation.
func ValidateBillingPricingMeter(ctx context.Context, model, unit string) error {
	snapshot := BillingPricingSnapshotFromContext(ctx)
	if snapshot == nil {
		return nil
	}
	if err := ValidateBillingPricingSnapshot(ctx); err != nil {
		return err
	}
	if err := validateDurableBillingModel(ctx, model); err != nil {
		return err
	}
	if snapshot.billing == nil || snapshot.resolver == nil {
		return fmt.Errorf("task billing price evidence is unavailable")
	}
	resolved := snapshot.resolver.Resolve(ctx, PricingInput{Group: snapshot.group, GroupID: &snapshot.group.ID, Model: model})
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo {
		if resolved.channelPricing != nil && resolved.channelPricing.PerRequestPrice != nil && validCost(*resolved.channelPricing.PerRequestPrice) {
			return nil
		}
		for _, tier := range resolved.RequestTiers {
			if tier.PerRequestPrice != nil && validCost(*tier.PerRequestPrice) {
				return nil
			}
		}
		return fmt.Errorf("task request price evidence is unavailable")
	}
	if unit == "token" || (resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel) {
		pricingContext := 0 // Deferred token tasks currently report output tokens only.
		if !resolved.longContextPricingEnabled {
			pricingContext = 1
		}
		base, _ := snapshot.billing.getModelPricingAt(model, snapshot.pricingAt)
		explicitOutput := resolved.channelPricing != nil && resolved.channelPricing.OutputPrice != nil
		if tier := FindMatchingInterval(resolved.Intervals, pricingContext); tier != nil && tier.OutputPrice != nil {
			explicitOutput = true
		}
		if base == nil && !explicitOutput {
			return fmt.Errorf("task output token price evidence is unavailable")
		}
		pricing := snapshot.resolver.GetIntervalPricing(resolved, pricingContext)
		if pricing == nil {
			return fmt.Errorf("task token price evidence is unavailable")
		}
		if !validCost(pricing.InputPricePerToken) || !validCost(pricing.OutputPricePerToken) {
			return fmt.Errorf("task token price evidence is invalid")
		}
		return nil
	}
	if unit == "second" {
		for _, resolution := range []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P} {
			if !validCost(snapshot.billing.getVideoUnitPrice(model, resolution, videoPriceConfigFromAPIKey(&APIKey{Group: snapshot.group}))) {
				return fmt.Errorf("task video price evidence is unavailable")
			}
		}
		return nil
	}
	return fmt.Errorf("task billing meter is unsupported")
}
