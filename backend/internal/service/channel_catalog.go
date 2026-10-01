package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
)

// CatalogGroup contains only groups already authorized by GetAvailableGroups.
// The HTTP layer converts these domain values through an explicit whitelist.
type CatalogGroup struct {
	Group  Group
	Models []CatalogOffer
}

type CatalogSource struct{ Name, Description string }
type CatalogMediaTier struct {
	Label string
	Unit  string
	Price *float64
}
type CatalogServiceTier struct {
	Name    string
	Pricing *ChannelModelPricing
}
type CatalogOffer struct {
	PlazaModel
	OfferKey     string
	BillingMode  BillingMode
	BillingUnit  string
	PriceStatus  string
	PriceReason  string
	Source       CatalogSource
	MediaTiers   []CatalogMediaTier
	ServiceTiers []CatalogServiceTier
}

// ListAvailableCatalog never discovers models, selects accounts or writes usage.
// Resolve against a request-local channel snapshot, so enumeration, mapping and
// pricing cannot join different channel-cache generations during a refresh.
func (s *ModelPlazaService) ListAvailableCatalog(ctx context.Context, groups []Group) ([]CatalogGroup, error) {
	out := make([]CatalogGroup, 0, len(groups))
	if len(groups) == 0 {
		return out, nil
	}
	channels, err := s.channelRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog channels: %w", err)
	}
	platforms := make(map[int64]string, len(groups))
	for _, g := range groups {
		platforms[g.ID] = g.Platform
	}
	active := make([]Channel, 0, len(channels))
	bound := make(map[int64]*Channel)
	for _, channel := range channels {
		if channel.Status != StatusActive {
			continue
		}
		ids := make([]int64, 0, len(channel.GroupIDs))
		for _, id := range channel.GroupIDs {
			if _, ok := platforms[id]; ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		channel.GroupIDs = ids
		channel.normalizeBillingModelSource()
		active = append(active, channel)
	}
	for i := range active {
		for _, id := range active[i].GroupIDs {
			if bound[id] != nil {
				return nil, fmt.Errorf("catalog group has conflicting channel bindings")
			}
			bound[id] = &active[i]
		}
	}
	localChannels := &ChannelService{}
	localChannels.cache.Store(populateChannelCache(active, platforms))
	resolver := NewModelPricingResolver(localChannels, s.billingService)
	references := NewChannelPricingReferenceService(s.pricingService, s.billingService)
	for _, g := range groups {
		cg := CatalogGroup{Group: g, Models: []CatalogOffer{}}
		if channel := bound[g.ID]; channel != nil {
			for _, model := range channel.SupportedModels() {
				if !isConcreteRequestPlatform(model.Platform) || (g.Platform != PlatformComposite && model.Platform != g.Platform) {
					continue
				}
				cg.Models = append(cg.Models, s.catalogOffer(ctx, &g, channel, model, resolver, references))
			}
		}
		out = append(out, cg)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Group.Name), strings.ToLower(out[j].Group.Name)
		if a != b {
			return a < b
		}
		return out[i].Group.ID < out[j].Group.ID
	})
	return out, nil
}

func (s *ModelPlazaService) catalogOffer(ctx context.Context, g *Group, ch *Channel, model SupportedModel, resolver *ModelPricingResolver, references *ChannelPricingReferenceService) CatalogOffer {
	key := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s\x00%s", g.ID, ch.ID, model.Platform, strings.ToLower(model.Name))))
	o := CatalogOffer{
		PlazaModel: PlazaModel{Name: model.Name, Platform: model.Platform},
		OfferKey:   fmt.Sprintf("%x", key[:16]), BillingUnit: "unknown",
		PriceStatus: "unknown", PriceReason: "pricing_unavailable",
		Source: CatalogSource{Name: ch.Name, Description: ch.Description},
	}
	if model.Pricing != nil {
		o.BillingMode = model.Pricing.BillingMode
	}
	if s.billingService == nil {
		return o
	}
	ctx = WithResolvedTargetPlatform(ctx, model.Platform)
	mapping := resolver.channelService.ResolveChannelMapping(ctx, g.ID, model.Name)
	if mapping.BillingModelSource == BillingModelSourceUpstream || mapping.BillingModelSource == BillingModelSourceResponse {
		o.PriceReason = "request_dependent"
		return o
	}
	billingModel := model.Name
	if mapping.BillingModelSource == BillingModelSourceChannelMapped && mapping.Mapped {
		billingModel = mapping.MappedModel
	}
	reference := references.resolve(model.Platform, billingModel)
	if reference.Status == ChannelPricingStatusUnsupportedUnit {
		o.PriceReason = "unsupported_unit"
		return o
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: billingModel, Group: g, GroupID: &g.ID})
	if resolved == nil {
		return o
	}
	o.BillingMode = resolved.Mode
	if o.BillingMode == "" {
		o.BillingMode = BillingModeToken
	}
	if resolved.Source != PricingSourceGroup && resolved.Source != PricingSourceChannel {
		if media, ok := grokMediaReferenceSpec(billingModel); ok {
			o.BillingMode = media.mode
		}
	}
	// Legacy media cards can charge per output while group overrides charge per
	// image/second. Without the actual result there is no single safe unit or
	// rate to advertise. Token-configured media still follows the token path.
	if o.BillingMode != BillingModeToken {
		imageModel := isOpenAIImageGenerationModel(billingModel) || isGeminiCompatibleImageModel(billingModel)
		videoModel := isGrokVideoBillingModel(billingModel)
		if (imageModel && o.BillingMode != BillingModeImage) || (videoModel && o.BillingMode != BillingModeVideo) {
			o.PriceReason = "request_dependent"
			return o
		}
	}
	// Reference lookup is local and exact-SKU aware; it never refreshes the catalog.
	if reference.Pricing != nil && reference.Pricing.BillingMode == BillingModeToken {
		p := reference.Pricing
		o.OfficialPricing = &PlazaOfficialPricing{InputPrice: p.InputPrice, OutputPrice: p.OutputPrice, CacheWritePrice: p.CacheWritePrice, CacheWrite1hPrice: p.CacheWrite1hPrice, CacheReadPrice: p.CacheReadPrice, Intervals: p.Intervals}
	}
	if o.BillingMode == BillingModeToken {
		o.BillingUnit = "token"
		input := ContextPricingScheduleInput{Model: billingModel, Group: g, Platform: model.Platform}
		if resolved.Source == PricingSourceLiteLLM && isDeepSeekModel(billingModel) {
			now := time.Now().UTC()
			input.PricingAt = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		}
		sched, err := s.billingService.ResolveContextPricingSchedule(ctx, resolver, input)
		if err != nil || sched == nil || len(sched.Tiers) == 0 {
			return o
		}
		o.Pricing, err = s.catalogTokenPricing(ctx, resolver, resolved, input, sched)
		if err != nil {
			return o
		}
		o.LongContextBasis = sched.Basis
		o.TimePricing = sched.TimePricing
		if !input.PricingAt.IsZero() {
			o.TimePricing = catalogDeepSeekTimePricing()
		}
		tiers := []string{"priority", "flex"}
		if resolved.BasePricing != nil && resolved.BasePricing.UltrafastMultiplier > 0 {
			tiers = append(tiers, "ultrafast")
		}
		for _, tier := range tiers {
			input.ServiceTier = tier
			tierSchedule, err := s.billingService.ResolveContextPricingSchedule(ctx, resolver, input)
			if err != nil || tierSchedule == nil || len(tierSchedule.Tiers) == 0 {
				continue
			}
			pricing, err := s.catalogTokenPricing(ctx, resolver, resolved, input, tierSchedule)
			if err == nil {
				o.ServiceTiers = append(o.ServiceTiers, CatalogServiceTier{Name: tier, Pricing: pricing})
			}
		}
	} else {
		s.fillCatalogMedia(ctx, &o, g, billingModel, resolver, resolved)
	}
	if o.Pricing != nil {
		o.PriceStatus, o.PriceReason = "resolved", ""
	}
	return o
}

// Token mode ignores per-request fields left on a configured card. Image-token
// prices must also follow billing: input zero falls back to text input, whereas
// explicit output zero stays free, and service-tier multipliers affect both.
func (s *ModelPlazaService) catalogTokenPricing(ctx context.Context, resolver *ModelPricingResolver, resolved *ResolvedPricing, input ContextPricingScheduleInput, schedule *ContextPricingSchedule) (*ChannelModelPricing, error) {
	pricing := plazaPricingFromSchedule(nil, schedule)
	base := resolver.GetIntervalPricing(resolved, 1)
	if base == nil {
		return pricing, nil
	}
	pricing.ReasoningEffortMultipliers = maps.Clone(base.ReasoningEffortMultipliers)
	raw := resolved.channelPricing
	if base.ImageInputPricePerToken != 0 || (raw != nil && raw.ImageInputPrice != nil) ||
		base.ImageOutputPricePerToken != 0 || base.ImageOutputPriceExplicit {
		cost, err := s.billingService.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: ctx, Model: input.Model, Group: input.Group,
			Tokens:         UsageTokens{InputTokens: 1, ImageInputTokens: 1, OutputTokens: 1, ImageOutputTokens: 1},
			RateMultiplier: 1, ServiceTier: input.ServiceTier, PricingAt: input.PricingAt,
			Resolver: resolver, Resolved: resolved,
		})
		if err != nil {
			return nil, err
		}
		pricing.ImageInputPrice = &cost.ImageInputCost
		pricing.ImageOutputPrice = &cost.ImageOutputCost
	}
	return pricing, nil
}

// Derive the display periods from the billing function, not a second table.
func catalogDeepSeekTimePricing() *TimePricingSchedule {
	location := time.FixedZone("Asia/Shanghai", 8*3600)
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, location) // Monday
	schedule := &TimePricingSchedule{Timezone: "Asia/Shanghai", WeekdaysOnly: true}
	for hour := 0; hour < 24; {
		multiplier := deepseekPeakMultiplierAt(day.Add(time.Duration(hour) * time.Hour))
		start := hour
		for hour++; hour < 24 && deepseekPeakMultiplierAt(day.Add(time.Duration(hour)*time.Hour)) == multiplier; hour++ {
		}
		if multiplier != 1 {
			schedule.Periods = append(schedule.Periods, TimePricingPeriod{StartTime: fmt.Sprintf("%02d:00", start), EndTime: fmt.Sprintf("%02d:00", hour), Multiplier: multiplier})
		}
	}
	return schedule
}

// Media quotes use the same pure cost functions and precedence as gateway usage.
// No request is submitted; one unit at each supported size is priced locally.
func (s *ModelPlazaService) fillCatalogMedia(ctx context.Context, o *CatalogOffer, g *Group, model string, resolver *ModelPricingResolver, resolved *ResolvedPricing) {
	labels := []string{""}
	switch o.BillingMode {
	case BillingModeImage:
		o.BillingUnit, labels = "image", []string{"1K", "2K", "4K"}
	case BillingModeVideo:
		o.BillingUnit, labels = "second", []string{"480p", "720p", "1080p"}
	case BillingModePerRequest:
		o.BillingUnit = "request"
		for _, tier := range resolved.RequestTiers {
			if tier.TierLabel != "" {
				labels = append(labels, tier.TierLabel)
			}
		}
	default:
		o.PriceReason = "unsupported_unit"
		return
	}
	// An empty configured card is unknown, even if an internal float default is 0.
	if pricingNeedsFallback(resolved.channelPricing) && o.BillingMode == BillingModePerRequest {
		return
	}
	key := &APIKey{Group: g}
	p := &ChannelModelPricing{BillingMode: o.BillingMode, ReasoningEffortMultipliers: reasoningEffortMultipliersFromPricing(resolved.channelPricing)}
	for _, label := range labels {
		var cost *CostBreakdown
		var err error
		switch {
		case resolved.channelPricing == nil && o.BillingMode == BillingModeImage:
			cost = s.billingService.CalculateImageCost(model, label, 1, imagePriceConfigFromAPIKey(key), 1)
		case resolved.channelPricing == nil && o.BillingMode == BillingModeVideo:
			cost = s.billingService.CalculateVideoCost(model, label, 1, 1, videoPriceConfigFromAPIKey(key), 1)
		case o.BillingMode == BillingModeImage && resolved.Source != PricingSourceGroup && apiKeyHasConfiguredImagePrice(key, label):
			cost = s.billingService.CalculateImageCost(model, label, 1, imagePriceConfigFromAPIKey(key), 1)
		case o.BillingMode == BillingModeVideo && resolved.Source != PricingSourceGroup && apiKeyHasConfiguredVideoPrice(key, model, label):
			cost = s.billingService.CalculateVideoCost(model, label, 1, 1, videoPriceConfigFromAPIKey(key), 1)
		default:
			if pricingNeedsFallback(resolved.channelPricing) {
				return
			}
			cost, err = s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, Group: g, GroupID: &g.ID, Resolver: resolver, Resolved: resolved, RateMultiplier: 1, RequestCount: 1, UsageUnits: 1, SizeTier: label})
		}
		if err != nil || cost == nil {
			return
		}
		price := cost.ActualCost
		if p.PerRequestPrice == nil {
			p.PerRequestPrice = &price
		}
		if label != "" {
			o.MediaTiers = append(o.MediaTiers, CatalogMediaTier{Label: label, Unit: o.BillingUnit, Price: &price})
		}
	}
	// Context-sensitive per-request tiers are retained with actual resolved prices.
	if o.BillingMode == BillingModePerRequest {
		for _, tier := range resolved.RequestTiers {
			if tier.TierLabel != "" {
				continue
			}
			cost, err := s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, Group: g, GroupID: &g.ID, Resolver: resolver, Resolved: resolved, RateMultiplier: 1, RequestCount: 1, Tokens: UsageTokens{InputTokens: tier.MinTokens + 1}})
			if err != nil {
				return
			}
			price := cost.ActualCost
			p.Intervals = append(p.Intervals, PricingInterval{MinTokens: tier.MinTokens, MaxTokens: tier.MaxTokens, PerRequestPrice: &price})
		}
	}
	o.Pricing = p
}
