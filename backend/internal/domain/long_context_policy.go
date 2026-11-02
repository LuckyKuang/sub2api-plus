package domain

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Prices are USD per million tokens. Nil inherits; a pointer to zero is free.
// Threshold zero disables pricing, independently of provider cache behavior.
type LongContextModelRule struct {
	Platform     string   `json:"platform"`
	Model        string   `json:"model"`
	Threshold    int      `json:"threshold"`
	Mode         string   `json:"mode,omitempty"` // prices, multipliers, or inherit
	Input        *float64 `json:"input,omitempty"`
	Output       *float64 `json:"output,omitempty"`
	CacheWrite   *float64 `json:"cache_write,omitempty"`
	CacheWrite1h *float64 `json:"cache_write_1h,omitempty"`
	CacheRead    *float64 `json:"cache_read,omitempty"`
}

func (r LongContextModelRule) Clone() LongContextModelRule {
	copy := r
	for _, pair := range []struct {
		dst **float64
		src *float64
	}{{&copy.Input, r.Input}, {&copy.Output, r.Output}, {&copy.CacheWrite, r.CacheWrite}, {&copy.CacheWrite1h, r.CacheWrite1h}, {&copy.CacheRead, r.CacheRead}} {
		if pair.src != nil {
			value := *pair.src
			*pair.dst = &value
		}
	}
	return copy
}

func (p BillingRatePolicy) ValidateLongContext() error {
	if len(p.LongContextModels) > 256 {
		return fmt.Errorf("at most 256 long-context model rules are allowed")
	}
	for platform, threshold := range p.LongContextPlatforms {
		if !IsConcretePlatform(platform) || threshold < 0 || threshold > 1000000000 {
			return fmt.Errorf("invalid long-context platform or threshold")
		}
	}
	seen := map[string]bool{}
	for _, rule := range p.LongContextModels {
		model := strings.TrimSpace(rule.Model)
		key := rule.Platform + "\x00" + strings.ToLower(model)
		if !IsConcretePlatform(rule.Platform) || model == "" || model != rule.Model || len(model) > 256 || (strings.Contains(model, "*") || strings.ContainsFunc(model, unicode.IsSpace)) || seen[key] || rule.Threshold < 0 || rule.Threshold > 1000000000 {
			return fmt.Errorf("invalid or duplicate long-context model rule")
		}
		seen[key] = true
		if rule.Mode != "" && rule.Mode != "inherit" && rule.Mode != "prices" && rule.Mode != "multipliers" {
			return fmt.Errorf("invalid long-context price mode")
		}
		for _, value := range []*float64{rule.Input, rule.Output, rule.CacheWrite, rule.CacheWrite1h, rule.CacheRead} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > MaxBillingRateMultiplier) {
				return fmt.Errorf("invalid long-context price or multiplier")
			}
		}
		if (rule.Mode == "prices" || rule.Mode == "multipliers") && (rule.Input == nil || rule.Output == nil) {
			return fmt.Errorf("custom long-context prices require input and output values")
		}
	}
	return nil
}

func (p BillingRatePolicy) LongContextRule(platform, model string) (*LongContextModelRule, bool) {
	for _, rule := range p.LongContextModels {
		if rule.Platform == platform && strings.EqualFold(rule.Model, model) {
			copy := rule.Clone()
			return &copy, true
		}
	}
	if threshold, ok := p.LongContextPlatforms[platform]; ok {
		return &LongContextModelRule{Platform: platform, Model: model, Threshold: threshold}, true
	}
	return nil, false
}
