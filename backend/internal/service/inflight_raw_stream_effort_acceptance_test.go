//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Streaming must settle the final effort sent to the provider, just like a
// buffered response. Requested xhigh is cheaper on this public card than the
// normalized GLM max or Grok high: $2 x platform 2 x model 3 x effort 4 = $48.
func TestInflightRawChatStreamEffortCoversActualWireAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, platform, upstreamModel, requested, forwarded string
		wantCost                                            float64
	}{
		{"GLM normalization", PlatformOpenAI, "glm-5.2", "xhigh", "max", 48},
		{"Grok normalization", PlatformGrok, "grok-4.5", "xhigh", "high", 48},
		{"ordinary effort preserved", PlatformOpenAI, "gpt-5.5", "medium", "medium", 6},
	} {
		for _, free := range []bool{false, true} {
			name, price, want := tc.name+"/paid", 2.0, tc.wantCost
			if free {
				name, price, want = tc.name+"/explicit zero", 0, 0
			}
			t.Run(name, func(t *testing.T) {
				const publicModel = "retail-stream-model"
				group := &Group{ID: 77, Platform: tc.platform, RateMultiplier: 1,
					ModelPricing: []ChannelModelPricing{{Platform: tc.platform, Models: []string{publicModel},
						BillingMode: BillingModePerRequest, PerRequestPrice: &price,
						ReasoningEffortMultipliers: map[string]float64{"medium": 0.5, "xhigh": 0.5, "high": 4, "max": 4}}},
					BillingRatePolicy: domain.BillingRatePolicy{
						PlatformMultipliers: map[string]float64{tc.platform: 2},
						ModelMultipliers:    []domain.BillingModelRate{{Platform: tc.platform, Model: publicModel, Multiplier: 3}},
					}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, User: user, Group: group, GroupID: &group.ID}
				account := rawChatCompletionsTestAccount()
				account.Platform = tc.platform
				account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: false}
				account.Credentials["model_mapping"] = map[string]any{publicModel: tc.upstreamModel}
				cache := &inflightSnapshotCacheStub{byBucket: map[string][]Account{
					inflightBucketKey(group.ID, tc.platform): {*account},
				}}
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q,"max_tokens":1,"stream":true}`, publicModel, tc.requested))
				responseBody := fmt.Sprintf("data: {\"id\":\"raw-stream-effort\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n", tc.upstreamModel)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
				logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				svc.cfg, svc.httpUpstream = rawChatCompletionsTestConfig(), upstream
				svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1}
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), tc.platform), key, time.Now(), publicModel)
				req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIChat, Model: publicModel,
					Kind: InflightEstimateToken, BodyBytes: len(body), MaxTokens: 1}).WithRequestPricingMetadata(body)
				hold, priced := svc.EstimateInflightReservation(ctx, key, req)
				c := adaptiveProtocolTestContext("/v1/chat/completions", body)
				c.Request = c.Request.WithContext(ctx)
				result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.Stream)
				require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
				require.Equal(t, tc.upstreamModel, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, tc.forwarded, gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
				require.Equal(t, tc.forwarded, optionalStringValue(result.ReasoningEffort))
				require.Equal(t, 1, result.Usage.InputTokens)
				require.Equal(t, 1, result.Usage.OutputTokens)
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user,
					Account: account, OriginalModel: publicModel}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, want, ledger.lastCmd.BalanceCost, 1e-12)
				require.Equal(t, publicModel, logs.lastLog.BillingRateSnapshot["model"])
				require.True(t, priced)
				if free {
					require.Zero(t, hold, "streaming cannot replace the explicit free public card")
				} else {
					require.GreaterOrEqual(t, hold, want, "admission must cover settlement of the actual streaming effort")
				}
			})
		}
	}
}
