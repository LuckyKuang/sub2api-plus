package service

import (
	"context"
	"strings"
)

// LongContextDefault exposes the same maintained reference tiers used by the
// pricing editor. Zero is an explicit indication that this model has no tier.
type LongContextDefault struct {
	Model     string `json:"model"`
	Threshold int    `json:"threshold"`
	Inclusive bool   `json:"inclusive,omitempty"`
}

func (s *ChannelPricingReferenceService) LongContextDefaults(ctx context.Context) (map[string][]LongContextDefault, error) {
	out := make(map[string][]LongContextDefault)
	for _, platform := range s.SupportedPlatforms() {
		refs, err := s.List(ctx, platform)
		if err != nil {
			return nil, err
		}
		out[platform] = []LongContextDefault{}
		for _, ref := range refs {
			threshold := 0
			inclusive := false
			if ref.Pricing != nil && ref.Pricing.BillingMode == BillingModeToken {
				for _, interval := range ref.Pricing.Intervals {
					if interval.MinTokens > 0 && (threshold == 0 || interval.MinTokens < threshold) {
						threshold = interval.MinTokens
						inclusive = strings.HasPrefix(interval.TierLabel, ">=")
						if inclusive {
							threshold++
						}
					}
				}
			}
			out[platform] = append(out[platform], LongContextDefault{Model: ref.Model, Threshold: threshold, Inclusive: inclusive})
		}
	}
	return out, nil
}
