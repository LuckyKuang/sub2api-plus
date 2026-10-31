//go:build unit

package handler

import (
	"fmt"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real WS frames must retain channel/request pricing when the completed event
// reports Luna. Compiled Sol/6.1 Sol cost: 2 input × $2/M + 1 output × $10/M.
func TestOpenAIResponsesWebSocket_ReportedLunaPreservesSelectedSolPrice(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, tc := range []struct{ name, requested, mapped, source string }{
			{"channel mapped Sol", "gpt-6-luna", "gpt-6-sol", service.BillingModelSourceChannelMapped},
			{"requested 6.1 Sol", "gpt-6.1-sol", "gpt-6.1-sol", service.BillingModelSourceRequested},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: fmt.Sprintf(`{"type":"response.create","model":%q,"stream":false}`, tc.requested),
					ingressMode:  mode, channelMapping: map[string]string{tc.requested: tc.mapped}, billingModelSource: tc.source, reportedModel: "gpt-6-luna",
				})
				require.Len(t, got.upstreamPayloads, 1)
				require.Equal(t, tc.mapped, gjson.GetBytes(got.upstreamPayloads[0], "model").String(), "the channel mapping must reach the actual upstream frame")
				require.Len(t, got.logs, 1)
				log := got.logs[0]
				require.InDelta(t, 14e-6, log.TotalCost, 1e-12)
				require.InDelta(t, 14e-6, log.ActualCost, 1e-12)
				require.Equal(t, tc.requested, log.RequestedModel)
				require.NotNil(t, log.UpstreamResponseModel)
				require.Equal(t, "gpt-6-luna", *log.UpstreamResponseModel, "reported models must remain visible for diagnosis")
			})
		}
	}
}
