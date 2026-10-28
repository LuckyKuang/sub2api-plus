package service

import (
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
)

// BillingRateQuote is the customer-facing rate decision shared by catalog,
// subscriptions, reservation and final settlement. Price-card time/service-tier
// adjustments remain in the existing unit-price pipeline.
type BillingRateQuote struct {
	Platform            string    `json:"platform"`
	Model               string    `json:"model"`
	BillingUnit         string    `json:"billing_unit"`
	BaseMultiplier      float64   `json:"base_multiplier"`
	PolicyMultiplier    float64   `json:"policy_multiplier"`
	PlatformMultiplier  float64   `json:"platform_multiplier"`
	ModelMultiplier     float64   `json:"model_multiplier"`
	RateCalculation     string    `json:"rate_calculation"`
	PeakScope           string    `json:"peak_scope"`
	PeakMultiplier      float64   `json:"peak_multiplier"`
	EffectiveMultiplier float64   `json:"effective_multiplier"`
	BaseSource          string    `json:"base_source"`
	Source              string    `json:"source"`
	Pattern             string    `json:"pattern,omitempty"`
	PolicyVersion       int64     `json:"policy_version"`
	AsOf                time.Time `json:"as_of"`
}

const (
	BillingRateCalculationProduct        = "product_v2"
	BillingRateCalculationLegacyOverride = "override_v1"
	BillingPeakScopeAllMeters            = "all_meters"
	BillingPeakScopeTokenOnly            = "token_only"
)

func ResolveBillingRateQuote(group *Group, platform, publicModel, billingUnit string, personalRate *float64, pricingAt time.Time) BillingRateQuote {
	return resolveBillingRateQuote(group, platform, publicModel, billingUnit, personalRate, pricingAt, BillingRateCalculationProduct, BillingPeakScopeAllMeters)
}

// Historical override calculation is used only when reading accepted task evidence.
func resolveBillingRateQuote(group *Group, platform, publicModel, billingUnit string, personalRate *float64, pricingAt time.Time, calculation, peakScope string) BillingRateQuote {
	if pricingAt.IsZero() {
		pricingAt = timezone.Now()
	}
	quote := BillingRateQuote{Platform: strings.TrimSpace(platform), Model: publicModel, BillingUnit: billingUnit, BaseMultiplier: 1, PolicyMultiplier: 1, PlatformMultiplier: 1, ModelMultiplier: 1, RateCalculation: calculation, PeakScope: peakScope, PeakMultiplier: 1, Source: "default", BaseSource: "group", AsOf: pricingAt}
	if group == nil {
		return quoteWithEffectiveRate(quote)
	}
	quote.BaseMultiplier = group.RateMultiplier
	if personalRate != nil {
		quote.BaseMultiplier, quote.BaseSource = *personalRate, "personal"
	}
	switch billingUnit {
	case "image":
		if group.ImageRateIndependent {
			quote.BaseMultiplier, quote.BaseSource = group.ImageRateMultiplier, "media"
		}
	case "video", "second":
		if group.VideoRateIndependent {
			quote.BaseMultiplier, quote.BaseSource = group.VideoRateMultiplier, "media"
		}
	}
	if peakScope == BillingPeakScopeAllMeters || billingUnit == "token" {
		quote.PeakMultiplier = group.PeakMultiplierAt(pricingAt)
	}
	// Preserve the existing handling of historical negative base rates.
	if quote.BaseMultiplier < 0 {
		quote.BaseMultiplier = 0
	}
	match := group.BillingRatePolicy.Resolve(platform, publicModel, billingUnit)
	quote.PolicyMultiplier, quote.Source, quote.Pattern = match.Multiplier, match.Source, match.Pattern
	quote.PlatformMultiplier, quote.ModelMultiplier = match.PlatformMultiplier, match.ModelMultiplier
	if calculation == BillingRateCalculationLegacyOverride {
		quote.PolicyMultiplier = match.PlatformMultiplier
		if match.Source == "model" {
			quote.PolicyMultiplier = match.ModelMultiplier
		}
	}
	quote.PolicyVersion = group.BillingPolicyVersion
	return quoteWithEffectiveRate(quote)
}

func quoteWithEffectiveRate(quote BillingRateQuote) BillingRateQuote {
	quote.EffectiveMultiplier = quote.BaseMultiplier * quote.PolicyMultiplier * quote.PeakMultiplier
	return quote
}
