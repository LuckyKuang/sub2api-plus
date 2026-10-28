package service

import (
	"errors"
	"fmt"
)

// A configured price for one dimension cannot establish another dimension's
// price. In particular, a partial card on an unknown public alias must never
// turn a missing output/cache price into an implicit zero or another SKU's card.
type missingTokenPriceError struct{ dimension string }

func (err *missingTokenPriceError) Error() string {
	return fmt.Sprintf("%s: missing %s token price", ErrModelPricingUnavailable, err.dimension)
}

func (*missingTokenPriceError) Unwrap() error { return ErrModelPricingUnavailable }

func isMissingTokenPriceError(err error) bool {
	var missing *missingTokenPriceError
	return errors.As(err, &missing)
}

// Evidence comes directly from the same-name catalog and explicit pointer
// presence in frozen Group/Channel cards and intervals. These inputs already
// survive durable export/restore; no process-local validity flag is involved.
func (s *BillingService) validateUsedTokenPriceEvidence(resolved *ResolvedPricing, model string, tokens UsageTokens, pricingContext int, pricing *ModelPricing) error {
	if resolved == nil || resolved.channelPricing == nil {
		return nil
	}
	if base, err := s.GetModelPricing(model); err == nil && base != nil {
		return nil
	}
	card := resolved.channelPricing
	input, output := card.InputPrice != nil, card.OutputPrice != nil
	cacheWrite, cacheWrite1h, cacheRead := card.CacheWritePrice != nil, card.CacheWritePrice != nil || card.CacheWrite1hPrice != nil, card.CacheReadPrice != nil
	if interval := FindMatchingInterval(resolved.Intervals, pricingContext); interval != nil {
		input = input || interval.InputPrice != nil
		output = output || interval.OutputPrice != nil
		cacheWrite = cacheWrite || interval.CacheWritePrice != nil
		cacheWrite1h = cacheWrite1h || interval.CacheWritePrice != nil || interval.CacheWrite1hPrice != nil
		cacheRead = cacheRead || interval.CacheReadPrice != nil
	}
	imageInputTokens := min(max(tokens.ImageInputTokens, 0), max(tokens.InputTokens, 0))
	textInputTokens := max(tokens.InputTokens-imageInputTokens, 0)
	if textInputTokens > 0 && !input {
		return &missingTokenPriceError{dimension: "input"}
	}
	// Image input zero retains the established fallback to the text input price.
	if imageInputTokens > 0 && !(card.ImageInputPrice != nil && pricing.ImageInputPricePerToken != 0) && !input {
		return &missingTokenPriceError{dimension: "image_input"}
	}
	if tokens.OutputTokens-tokens.ImageOutputTokens > 0 && !output {
		return &missingTokenPriceError{dimension: "output"}
	}
	// Explicit image output zero is valid. Omitted image output uses text output.
	if tokens.ImageOutputTokens > 0 && card.ImageOutputPrice == nil && !output {
		return &missingTokenPriceError{dimension: "image_output"}
	}
	if tokens.CacheReadTokens > 0 && !cacheRead {
		return &missingTokenPriceError{dimension: "cache_read"}
	}
	write5m, write1h := normalizeCacheCreationBreakdown(tokens)
	if write5m == 0 && write1h == 0 && tokens.CacheCreationTokens > 0 {
		write5m = tokens.CacheCreationTokens
	}
	if write5m > 0 && !cacheWrite {
		return &missingTokenPriceError{dimension: "cache_write"}
	}
	if write1h > 0 && !cacheWrite1h {
		return &missingTokenPriceError{dimension: "cache_write_1h"}
	}
	return nil
}
