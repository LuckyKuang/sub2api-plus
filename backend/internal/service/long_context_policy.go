package service

import (
	"fmt"
	"sort"
)

// applyLongContextPolicy changes sale prices only. It never edits a request,
// model context window, native prompt-cache policy or supplier account costs.
func (r *ModelPricingResolver) applyLongContextPolicy(resolved *ResolvedPricing, input PricingInput, platform string) {
	if resolved == nil || input.Group == nil || resolved.Mode != BillingModeToken {
		return
	}
	rule, explicit := input.Group.BillingRatePolicy.LongContextRule(platform, input.Model)
	if !explicit {
		return
	}
	resolved.longContextRuleExplicit = true
	resolved.longContextPricingEnabled = rule.Threshold > 0
	if rule.Threshold == 0 {
		return
	}
	if resolved.BasePricing != nil {
		base := *resolved.BasePricing
		resolved.BasePricing = &base
	}
	custom := rule.Mode == "prices" || rule.Mode == "multipliers"
	resolved.longContextCustomPrices = rule.Mode == "prices"
	if !custom && len(resolved.Intervals) > 0 {
		// Keep subsequent steps and all their prices. Move only the first
		// transition, so a supplier default does not collapse multi-step cards.
		intervals := append([]PricingInterval(nil), resolved.Intervals...)
		sort.SliceStable(intervals, func(i, j int) bool { return intervals[i].MinTokens < intervals[j].MinTokens })
		first := 0
		if intervals[0].MinTokens <= 1 {
			first = 1
		}
		if first < len(intervals) {
			start := rule.Threshold
			if (intervals[first].MaxTokens != nil && start >= *intervals[first].MaxTokens) || (first+1 < len(intervals) && start >= intervals[first+1].MinTokens) {
				resolved.longContextPricingError = fmt.Errorf("long-context threshold overlaps a later pricing tier")
				return
			}
			intervals[first].MinTokens = start
			if first > 0 {
				end := rule.Threshold
				intervals[first-1].MaxTokens = &end
			}
			resolved.Intervals = intervals
			return
		}
	}
	base := resolved.BasePricing
	if base == nil {
		resolved.longContextPricingError = fmt.Errorf("long-context pricing requires a known base price")
		return
	}
	if !custom {
		if base.LongContextInputThreshold <= 0 || (base.LongContextInputMultiplier <= 1 && base.LongContextOutputMultiplier <= 1) {
			resolved.longContextPricingError = fmt.Errorf("positive long-context threshold requires high-tier prices or multipliers for model %s", input.Model)
			return
		}
		base.LongContextInputThreshold = rule.Threshold
		base.LongContextThresholdInclusive = false
		return
	}
	if rule.CacheWrite1h != nil {
		resolved.SupportsCacheBreakdown = true
		enableCacheCreationBreakdown(base)
	}
	// Compile explicit two-level pricing into the existing interval engine;
	// this preserves zero prices and independently priced read/write meters.
	high := PricingInterval{MinTokens: rule.Threshold, TierLabel: "long_context"}
	if rule.Mode == "multipliers" {
		high.InputMultiplier = rule.Input
		high.OutputMultiplier = rule.Output
		high.CacheWriteMultiplier = rule.CacheWrite
		high.CacheReadMultiplier = rule.CacheRead
		if rule.CacheWrite1h != nil {
			price := base.CacheCreation1hPrice * *rule.CacheWrite1h
			high.CacheWrite1hPrice = &price
		}
	} else {
		for _, pair := range []struct {
			value  *float64
			target **float64
		}{{rule.Input, &high.InputPrice}, {rule.Output, &high.OutputPrice}, {rule.CacheWrite, &high.CacheWritePrice}, {rule.CacheWrite1h, &high.CacheWrite1hPrice}, {rule.CacheRead, &high.CacheReadPrice}} {
			if pair.value != nil {
				price := *pair.value / 1e6
				*pair.target = &price
			}
		}
	}
	// The compiled intervals own tiering; do not add the official surcharge.
	base.LongContextInputThreshold = 0
	resolved.Intervals = []PricingInterval{high}
}
