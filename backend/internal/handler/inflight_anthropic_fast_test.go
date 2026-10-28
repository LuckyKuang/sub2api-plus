//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type anthropicFastInflightRecorder struct {
	calls int
	req   service.InflightEstimateRequest
}

func (r *anthropicFastInflightRecorder) EstimateInflightReservation(_ context.Context, _ *service.APIKey, req service.InflightEstimateRequest) (float64, bool) {
	r.calls++
	r.req = req
	return 0, true
}

// The Messages beta declaration can become OpenAI priority after conversion.
// HTTP admission must retain the exact declaration for pricing, while a
// foreign protocol or substring cannot opt into that billing dimension.
func TestInflightMessagesCapturesExactFastBetaForReservation(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, header string
		want                   bool
	}{
		{"messages exact", service.ContentModerationProtocolAnthropicMessages, claude.BetaFastMode, true},
		{"messages list whitespace", service.ContentModerationProtocolAnthropicMessages, "other-beta, " + claude.BetaFastMode + " ,future-beta", true},
		{"messages substring", service.ContentModerationProtocolAnthropicMessages, claude.BetaFastMode + "-future", false},
		{"messages empty", service.ContentModerationProtocolAnthropicMessages, "", false},
		{"chat foreign beta", service.ContentModerationProtocolOpenAIChat, claude.BetaFastMode, false},
		{"responses foreign beta", service.ContentModerationProtocolOpenAIResponses, claude.BetaFastMode, false},
		{"gemini foreign beta", service.ContentModerationProtocolGemini, claude.BetaFastMode, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			c := newInflightTestGinContext()
			c.Request.Header.Set("anthropic-beta", tc.header)
			estimator := &anthropicFastInflightRecorder{}
			key := &service.APIKey{User: &service.User{ID: 1}}
			req := tokenInflightEstimate(tc.protocol, "retail-model", []byte(`{}`))
			done, err := reserveInflightBalance(c, billing, estimator, key, nil, req)
			require.NoError(t, err)
			done()
			require.Equal(t, 1, estimator.calls)
			require.Equal(t, tc.want, estimator.req.AnthropicFast)
			require.Empty(t, estimator.req.ServiceTier, "the native beta is metadata until a supported conversion selects OpenAI priority")
		})
	}
}
