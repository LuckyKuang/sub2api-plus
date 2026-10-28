package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// This private storage format is deliberately separate from the public rate
// explanation. It pins only the models needed by the task, never credentials or
// the process configuration, so a delayed settlement survives reload/restart.
type durableBillingPricingSnapshot struct {
	IdentifiedImages       map[string]map[string]bool    `json:"identified_images,omitempty"`
	Version                int                           `json:"version"`
	RateCalculation        string                        `json:"rate_calculation"`
	PeakScope              string                        `json:"peak_scope"`
	Group                  *Group                        `json:"group"`
	PersonalRate           *float64                      `json:"personal_rate,omitempty"`
	TokenPeak              *float64                      `json:"token_peak"`
	PricingAt              time.Time                     `json:"pricing_at"`
	Platform               string                        `json:"platform"`
	PublicModel            string                        `json:"public_model"`
	RoutingModel           string                        `json:"routing_model,omitempty"`
	Channel                *Channel                      `json:"channel,omitempty"`
	Models                 map[string]*ModelPricing      `json:"models"`
	MissingModels          map[string]bool               `json:"missing_models,omitempty"`
	Images                 map[string]map[string]float64 `json:"images"`
	Videos                 map[string]map[string]float64 `json:"videos"`
	CacheStorage           map[string]float64            `json:"cache_storage"`
	DeepSeekPeakMultiplier float64                       `json:"deepseek_peak_multiplier"`
}

func pinBillingComponentDefaults(group *Group) {
	for _, field := range []struct {
		target **float64
		value  float64
	}{
		{&group.WebSearchPricePerCall, defaultWebSearchPricePerCall},
		{&group.SearchPricePer1k, defaultSearchPricePer1k},
		{&group.AudioRealtimePricePerMin, defaultAudioRealtimePricePerMin},
		{&group.AudioTTSPricePerMillionChars, defaultAudioTTSPricePerMillionChars},
		{&group.AudioSTTPricePerHour, defaultAudioSTTPricePerHour},
	} {
		if *field.target == nil {
			value := field.value
			*field.target = &value
		}
	}
}

func ExportBillingPricingSnapshot(ctx context.Context, models ...string) (json.RawMessage, error) {
	snapshot := BillingPricingSnapshotFromContext(ctx)
	if snapshot == nil {
		return nil, nil
	}
	if err := ValidateBillingPricingSnapshot(ctx); err != nil {
		return nil, err
	}
	version := 2
	if snapshot.legacyPriceSelection {
		version = 1
	}
	stored := durableBillingPricingSnapshot{Version: version, RateCalculation: snapshot.calculation(), PeakScope: snapshot.peakMeterScope(), Group: cloneBillingGroup(snapshot.group), PersonalRate: snapshot.personalRate,
		TokenPeak: snapshot.tokenPeak,
		PricingAt: snapshot.pricingAt, Platform: snapshot.platform, PublicModel: snapshot.publicModel,
		DeepSeekPeakMultiplier: deepseekPeakMultiplierAt(snapshot.pricingAt),
		MissingModels:          make(map[string]bool),
		Models:                 make(map[string]*ModelPricing), Images: make(map[string]map[string]float64), Videos: make(map[string]map[string]float64), CacheStorage: make(map[string]float64)}
	stored.IdentifiedImages = make(map[string]map[string]bool)
	stored.RoutingModel = snapshot.routingModel
	if snapshot.billing != nil && snapshot.billing.frozenDeepSeekPeakMultiplier != nil {
		stored.DeepSeekPeakMultiplier = *snapshot.billing.frozenDeepSeekPeakMultiplier
	}
	if routeModel, ok := ResolvedUpstreamModelFromContext(ctx); stored.RoutingModel == "" && ok {
		stored.RoutingModel = routeModel
	}
	if stored.Group == nil {
		return nil, fmt.Errorf("billing group snapshot unavailable")
	}
	pinBillingComponentDefaults(stored.Group)
	if snapshot.channels != nil {
		channel, err := snapshot.channels.GetChannelForGroup(ctx, stored.Group.ID)
		if err != nil {
			return nil, fmt.Errorf("billing channel snapshot unavailable: %w", err)
		}
		stored.Channel = channel.Clone()
	}
	models = append(append([]string(nil), models...), snapshot.publicModel)
	if stored.RoutingModel != "" {
		models = append(models, stored.RoutingModel)
	}
	seen := make(map[string]bool)
	key := &APIKey{Group: stored.Group}
	for i := 0; i < len(models); i++ {
		model := strings.ToLower(strings.TrimSpace(models[i]))
		if model == "" || seen[model] {
			continue
		}
		if len(seen) >= 64 || len(model) > 512 {
			return nil, fmt.Errorf("billing snapshot model limit exceeded")
		}
		seen[model] = true
		if snapshot.billing == nil || snapshot.billing.frozenMissingModels[model] {
			stored.Models[model] = nil
			stored.MissingModels[model] = true
			continue
		} // explicit missing evidence, never an invented zero-priced card
		if snapshot.billing.frozenModelPrices != nil {
			if _, captured := snapshot.billing.frozenModelPrices[model]; !captured {
				return nil, fmt.Errorf("billing model is absent from the task pricing snapshot")
			}
		}
		if snapshot.channels != nil {
			mapping := snapshot.channels.ResolveChannelMapping(ctx, stored.Group.ID, model)
			if mapping.MappedModel != "" {
				models = append(models, mapping.MappedModel)
			}
		}
		price, _ := snapshot.billing.getModelPricingAt(model, snapshot.pricingAt)
		if price != nil {
			copy := *price
			fast, flex := configuredServiceTierMultiplier("fast", price), configuredServiceTierMultiplier("flex", price)
			if price.FastMultiplier != nil || !usePriorityServiceTierPricing("fast", price) {
				copy.FastMultiplier = &fast
			}
			copy.FlexMultiplier = &flex
			if copy.UltrafastMultiplier <= 0 {
				copy.UltrafastMultiplier = configuredServiceTierMultiplier(OpenAIFastTierUltrafast, price)
			}
			price = &copy
		}
		stored.Models[model] = price
		stored.Images[model] = make(map[string]float64)
		stored.IdentifiedImages[model] = make(map[string]bool)
		for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
			price := snapshot.billing.getImageUnitPrice(model, tier, imagePriceConfigFromAPIKey(key))
			if !validCost(price) {
				return nil, fmt.Errorf("image unit price is missing from the task snapshot")
			}
			stored.Images[model][tier] = price
			stored.IdentifiedImages[model][tier] = snapshot.billing.hasIdentifiedImageUnitPrice(model, tier, imagePriceConfigFromAPIKey(key))
		}
		stored.Videos[model] = make(map[string]float64)
		for _, resolution := range []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P} {
			price := snapshot.billing.getVideoUnitPrice(model, resolution, videoPriceConfigFromAPIKey(key))
			if !validCost(price) {
				return nil, fmt.Errorf("video unit price is missing from the task snapshot")
			}
			stored.Videos[model][resolution] = price
		}
		stored.CacheStorage[model] = snapshot.billing.CacheStoragePricePerTokenHour(model)
	}
	return json.Marshal(stored)
}

func RestoreBillingPricingSnapshot(ctx context.Context, data json.RawMessage) (context.Context, error) {
	if len(data) == 0 {
		return legacyBillingSettlementContext(ctx), nil
	} // tasks created before the snapshot schema
	var stored durableBillingPricingSnapshot
	if err := json.Unmarshal(data, &stored); err != nil {
		return ctx, fmt.Errorf("invalid billing snapshot: %w", err)
	}
	if (stored.Version != 1 && stored.Version != 2) || stored.Group == nil || stored.PricingAt.IsZero() || stored.Group.AccountGroups != nil || stored.Models == nil || stored.Images == nil || stored.Videos == nil || stored.CacheStorage == nil || stored.DeepSeekPeakMultiplier < 1 || stored.TokenPeak == nil || !validCost(*stored.TokenPeak) {
		return ctx, fmt.Errorf("unsupported or incomplete billing snapshot")
	}
	// Version 1 tasks accepted before the product calculation had no discriminator.
	// Preserve their agreed charge, including platform zero overridden by a model.
	if stored.RateCalculation == "" {
		stored.RateCalculation = BillingRateCalculationLegacyOverride
	}
	if stored.RateCalculation != BillingRateCalculationProduct && stored.RateCalculation != BillingRateCalculationLegacyOverride {
		return ctx, fmt.Errorf("unsupported billing rate calculation")
	}
	// Accepted records without this field used peak pricing only for tokens,
	// including the first product_v2 records. Never reprice their other meters.
	if stored.PeakScope == "" {
		stored.PeakScope = BillingPeakScopeTokenOnly
	}
	if stored.PeakScope != BillingPeakScopeAllMeters && stored.PeakScope != BillingPeakScopeTokenOnly {
		return ctx, fmt.Errorf("unsupported billing peak scope")
	}
	if stored.IdentifiedImages == nil {
		stored.IdentifiedImages = make(map[string]map[string]bool)
	}
	billing := &BillingService{frozenPricing: true, frozenPricingAt: stored.PricingAt, frozenModelPrices: stored.Models,
		frozenMissingModels:         stored.MissingModels,
		frozenIdentifiedImagePrices: stored.IdentifiedImages,
		frozenImagePrices:           stored.Images, frozenVideoPrices: stored.Videos, frozenCacheStoragePrices: stored.CacheStorage,
		frozenDeepSeekPeakMultiplier: &stored.DeepSeekPeakMultiplier}
	snapshot := &BillingPricingSnapshot{group: stored.Group, personalRate: stored.PersonalRate, pricingAt: stored.PricingAt,
		rateCalculation:      stored.RateCalculation,
		peakScope:            stored.PeakScope,
		legacyPriceSelection: stored.Version == 1,
		tokenPeak:            stored.TokenPeak,
		platform:             stored.Platform, publicModel: stored.PublicModel, routingModel: stored.RoutingModel, billing: billing}
	if stored.Channel != nil {
		groupPlatform := stored.Group.Platform
		if groupPlatform == "" {
			groupPlatform = stored.Platform
		}
		cache := populateChannelCache([]Channel{*stored.Channel}, map[int64]string{stored.Group.ID: groupPlatform})
		snapshot.channels = &ChannelService{frozenCache: cache}
		snapshot.channels.cache.Store(cache)
	}
	snapshot.resolver = NewModelPricingResolver(snapshot.channels, billing)
	ctx = pricingSnapshotContext(ctx, snapshot)
	if err := ValidateBillingPricingSnapshot(ctx); err != nil {
		return ctx, err
	}
	return ctx, nil
}
