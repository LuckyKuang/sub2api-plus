package domain

import (
	"fmt"
	"maps"
	"math"
	"strings"
	"unicode"
)

// BillingRatePolicy changes customer charges, never upstream account costs.
// Platform and model factors multiply independently; an absent factor is one.
type BillingRatePolicy struct {
	PlatformMultipliers map[string]float64 `json:"platform_multipliers"`
	ModelMultipliers    []BillingModelRate `json:"model_multipliers"`
}

type BillingModelRate struct {
	Platform    string  `json:"platform"`
	Model       string  `json:"model"`
	Multiplier  float64 `json:"multiplier"`
	BillingUnit string  `json:"billing_unit,omitempty"`
}

type BillingRateMatch struct {
	Multiplier         float64 `json:"multiplier"`
	PlatformMultiplier float64 `json:"platform_multiplier"`
	ModelMultiplier    float64 `json:"model_multiplier"`
	Source             string  `json:"source"`
	Pattern            string  `json:"pattern,omitempty"`
}

const MaxBillingRateMultiplier = 999999.9999

func (p BillingRatePolicy) Clone() BillingRatePolicy {
	return BillingRatePolicy{PlatformMultipliers: maps.Clone(p.PlatformMultipliers), ModelMultipliers: append([]BillingModelRate(nil), p.ModelMultipliers...)}
}

func ValidBillingRateUnit(unit string) bool {
	switch unit {
	case "", "token", "image", "request", "second", "minute", "hour", "million_characters":
		return true
	default:
		return false
	}
}

func validBillingRate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= MaxBillingRateMultiplier && math.Abs(v*10000-math.Round(v*10000)) < 1e-6
}

func (p BillingRatePolicy) Validate() error {
	if len(p.ModelMultipliers) > 256 {
		return fmt.Errorf("billing rate policy supports at most 256 model rules")
	}
	for platform, factor := range p.PlatformMultipliers {
		if !IsConcretePlatform(platform) {
			return fmt.Errorf("unsupported billing rate platform %q", platform)
		}
		if !validBillingRate(factor) {
			return fmt.Errorf("platform multiplier must be finite, between 0 and 999999.9999, with at most four decimal places")
		}
	}
	seen := make(map[string]bool, len(p.ModelMultipliers))
	for _, rule := range p.ModelMultipliers {
		if !IsConcretePlatform(rule.Platform) {
			return fmt.Errorf("unsupported billing rate platform %q", rule.Platform)
		}
		model := strings.TrimSpace(rule.Model)
		if model == "" || model != rule.Model || len(model) > 256 || strings.ContainsFunc(model, unicode.IsSpace) || strings.Contains(strings.TrimSuffix(model, "*"), "*") {
			return fmt.Errorf("billing model must be an exact ID or a trailing-star prefix")
		}
		if !ValidBillingRateUnit(rule.BillingUnit) {
			return fmt.Errorf("unsupported billing rate unit %q", rule.BillingUnit)
		}
		if !validBillingRate(rule.Multiplier) {
			return fmt.Errorf("model multiplier must be finite, between 0 and 999999.9999, with at most four decimal places")
		}
		key := rule.Platform + "\x00" + strings.ToLower(model) + "\x00" + rule.BillingUnit
		if seen[key] {
			return fmt.Errorf("duplicate billing model rate rule")
		}
		seen[key] = true
	}
	return nil
}

// Resolve is independent of slice order: exact > longest prefix, then a
// matching unit-specific rule > the all-units rule at that specificity.
func (p BillingRatePolicy) Resolve(platform, model, unit string) BillingRateMatch {
	match := BillingRateMatch{Multiplier: 1, PlatformMultiplier: 1, ModelMultiplier: 1, Source: "default"}
	if value, ok := p.PlatformMultipliers[platform]; ok && validBillingRate(value) {
		match.PlatformMultiplier, match.Source = value, "platform"
	}
	model = strings.ToLower(strings.TrimSpace(model))
	bestSpecificity, bestUnit := -1, -1
	for _, rule := range p.ModelMultipliers {
		if rule.Platform != platform || !validBillingRate(rule.Multiplier) || (rule.BillingUnit != "" && rule.BillingUnit != unit) {
			continue
		}
		pattern := strings.ToLower(rule.Model)
		specificity := -1
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(model, prefix) {
				specificity = len(prefix)
			}
		} else if pattern == model && model != "" {
			specificity = len(pattern) + 257
		}
		unitSpecificity := 0
		if rule.BillingUnit != "" {
			unitSpecificity = 1
		}
		if specificity < 0 || specificity < bestSpecificity || (specificity == bestSpecificity && unitSpecificity <= bestUnit) {
			continue
		}
		bestSpecificity, bestUnit = specificity, unitSpecificity
		match.ModelMultiplier, match.Source, match.Pattern = rule.Multiplier, "model", rule.Model
	}
	match.Multiplier = match.PlatformMultiplier * match.ModelMultiplier
	return match
}
