package service

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
)

func (s *BillingService) pricingFallbackModelsForRequest(ctx context.Context) (map[string]string, error) {
	if s.frozenPricingFallbackModels != nil {
		return maps.Clone(s.frozenPricingFallbackModels), nil
	}
	if s.pricingFallbackModels != nil {
		models, err := s.pricingFallbackModels(ctx)
		return maps.Clone(models), err
	}
	return DefaultPricingFallbackModels(), nil
}

// GetModelPricingForPlatform resolves same-model system prices before the
// explicitly selected concrete platform's fallback model. It never follows a
// second fallback or reads group/channel retail prices for the fallback SKU.
func (s *BillingService) GetModelPricingForPlatform(ctx context.Context, platform, model string) (*ModelPricing, error) {
	if s == nil {
		return nil, ErrModelPricingUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if frozen := billingServiceForContext(ctx, s); frozen != s {
		return frozen.GetModelPricingForPlatform(ctx, platform, model)
	}
	at := s.frozenPricingAt
	if at.IsZero() {
		at = timezone.Now()
	}
	return s.platformModelPricingAt(ctx, platform, model, at)
}

func (s *BillingService) platformModelPricingAt(ctx context.Context, platform, model string, at time.Time) (*ModelPricing, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return nil, ErrModelPricingUnavailable
	}
	// Restored task cards are complete captured results. Missing evidence may
	// never be repaired using today's settings or catalog.
	if s.frozenModelPrices != nil {
		return s.getModelPricingAt(model, at)
	}
	if price, err := s.getSystemModelPricingAt(model, at, true); err == nil {
		return price, nil
	}
	if !domain.IsConcretePlatform(platform) {
		return nil, ErrModelPricingUnavailable
	}
	models, err := s.pricingFallbackModelsForRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: platform fallback configuration unavailable", ErrModelPricingUnavailable)
	}
	fallback := strings.ToLower(strings.TrimSpace(models[platform]))
	if fallback == "" || fallback == model {
		return nil, fmt.Errorf("%w for platform %s", ErrModelPricingUnavailable, platform)
	}
	price, err := s.getPlatformFallbackModelPricingAt(fallback, at)
	if err != nil {
		return nil, fmt.Errorf("%w: platform fallback model has no system token price", ErrModelPricingUnavailable)
	}
	copy := *price
	copy.FallbackModel, copy.FallbackPlatform = fallback, platform
	return &copy, nil
}

// A final fallback supplies a complete text-token price card. Incomplete
// embedding/image-token rows can still price their own SKU through the normal
// path, but cannot invent free text output for an unrelated requested model.
func (s *BillingService) getPlatformFallbackModelPricingAt(model string, at time.Time) (*ModelPricing, error) {
	billing := s
	if !s.frozenPricing {
		// Check field presence and convert the price against the same immutable
		// catalog generation even when called outside a request snapshot.
		billing = snapshotBillingService(s)
	}
	if billing.pricingService != nil {
		entry := billing.pricingService.GetIdentifiedModelPricing(model)
		if entry != nil && !entry.TokenPricingAbsent && !entry.InputTokenPriceAbsent && !entry.OutputTokenPriceAbsent &&
			validCost(entry.InputCostPerToken) && validCost(entry.OutputCostPerToken) {
			price, err := billing.getSystemModelPricingAt(model, at, true)
			if err == nil && validPlatformFallbackPricing(price) {
				return price, nil
			}
		}
	}
	// A partial dynamic row must not hide a complete identified compiled card
	// for the exact same SKU. Never follow a family or another platform here.
	if price, _ := billing.IdentifiedSameModelPricing(model); price != nil {
		price = billing.applyModelSpecificPricingPolicyEx(model, price, true, at)
		if validPlatformFallbackPricing(price) {
			if billing.pricingService != nil {
				entry := billing.pricingService.GetIdentifiedModelPricing(model)
				if entry != nil && !entry.TokenPricingAbsent && (entry.InputTokenPriceAbsent || entry.OutputTokenPriceAbsent) {
					// Preserve configured dimensions, including zero. Only absent
					// text dimensions inherit the same SKU's compiled card.
					partial, err := billing.getSystemModelPricingAt(model, at, true)
					if err != nil || !validPlatformFallbackPricing(partial) {
						return nil, ErrModelPricingUnavailable
					}
					merged := *partial
					if entry.InputTokenPriceAbsent {
						merged.InputPricePerToken = price.InputPricePerToken
						merged.InputPricePerTokenPriority = price.InputPricePerTokenPriority
					}
					if entry.OutputTokenPriceAbsent {
						merged.OutputPricePerToken = price.OutputPricePerToken
						merged.OutputPricePerTokenPriority = price.OutputPricePerTokenPriority
					}
					return &merged, nil
				}
			}
			return price, nil
		}
	}
	return nil, ErrModelPricingUnavailable
}

func validPlatformFallbackPricing(price *ModelPricing) bool {
	if price == nil || price.LongContextInputThreshold < 0 {
		return false
	}
	for _, value := range []float64{
		price.InputPricePerToken, price.OutputPricePerToken,
		price.InputPricePerTokenPriority, price.OutputPricePerTokenPriority,
		price.CacheCreationPricePerToken, price.CacheCreationPricePerTokenPriority,
		price.CacheCreation5mPrice, price.CacheCreation1hPrice,
		price.CacheReadPricePerToken, price.CacheReadPricePerTokenPriority,
		price.ImageInputPricePerToken, price.ImageOutputPricePerToken, price.ImageCacheReadPricePerToken,
		price.LongContextInputMultiplier, price.LongContextOutputMultiplier, price.UltrafastMultiplier,
	} {
		if !validCost(value) {
			return false
		}
	}
	for _, multiplier := range []*float64{price.FastMultiplier, price.FlexMultiplier} {
		if multiplier != nil && !validCost(*multiplier) {
			return false
		}
	}
	for _, multiplier := range price.ReasoningEffortMultipliers {
		if !validCost(multiplier) {
			return false
		}
	}
	return true
}
