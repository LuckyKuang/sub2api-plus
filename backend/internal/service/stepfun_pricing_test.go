//go:build unit

package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Independent oracle: StepFun international official pricing, USD / 1M tokens,
// reviewed 2026-10-08. Cache misses include cache creation, with no surcharge.
var stepFunOfficialPrices = []struct {
	model                 string
	input, cached, output float64
}{
	{"step-5-preview", 1, .05, 2.70},
	{"step-3.7-flash", .20, .04, 1.15},
	{"step-3.5-flash", .10, .02, .30},
	{"step-3.5-flash-2603", .10, .02, .30},
	{"step-1o-turbo-vision", .36, .07, 1.15},
}

func TestStepFunPricingOfficialRatesAndReferenceCoverage(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		t.Run(fmt.Sprintf("catalog=%t", catalog), func(t *testing.T) {
			billing := newEmptyCatalogBillingService(t)
			refs := NewChannelPricingReferenceService(billing.pricingService, billing)
			if catalog {
				refs = newBundledCatalogReferenceService(t)
				billing = refs.billing
			}
			listed, err := refs.List(t.Context(), PlatformStepFun)
			require.NoError(t, err)
			byModel := referencesByModel(t, listed)
			for _, price := range stepFunOfficialPrices {
				t.Run(price.model, func(t *testing.T) {
					card := requireModelPricing(t, billing, price.model)
					require.InDelta(t, price.input/1e6, card.InputPricePerToken, 1e-15)
					require.InDelta(t, price.input/1e6, card.CacheCreationPricePerToken, 1e-15, "cache creation uses ordinary input rate, without a surcharge")
					require.InDelta(t, price.cached/1e6, card.CacheReadPricePerToken, 1e-15)
					require.InDelta(t, price.output/1e6, card.OutputPricePerToken, 1e-15)
					cost, err := billing.CalculateCost(price.model, UsageTokens{InputTokens: 1_000_000, CacheReadTokens: 1_000_000, OutputTokens: 1_000_000}, 1)
					require.NoError(t, err)
					require.InDelta(t, price.input+price.cached+price.output, cost.TotalCost, 1e-12)
					ref, ok := byModel[price.model]
					require.True(t, ok, "priced chat models must be visible in model sync")
					require.Equal(t, ChannelPricingStatusPriced, ref.Status)
					require.Equal(t, ref, mustResolve(t, refs, PlatformStepFun, price.model))
					require.InDelta(t, price.input/1e6, *ref.Pricing.InputPrice, 1e-15)
					require.InDelta(t, price.cached/1e6, *ref.Pricing.CacheReadPrice, 1e-15)
					require.InDelta(t, price.output/1e6, *ref.Pricing.OutputPrice, 1e-15)
					if catalog {
						require.Equal(t, ChannelPricingSourceReleaseCatalog, ref.Source)
					} else {
						require.Equal(t, ChannelPricingSourceBuiltinFallback, ref.Source)
					}
				})
			}
			require.Contains(t, byModel, "step-router-v1", "unpriced supported models must stay visible")
			for _, model := range []string{"step-router-v1", "step-unknown", "step-3.7-flash-20261008", "step-3.7-flash-preview", "step-3.5-flash-2604", "step-opus", "step-tts-2"} {
				ref := mustResolve(t, refs, PlatformStepFun, model)
				require.Equal(t, ChannelPricingStatusManualRequired, ref.Status, model)
				require.Nil(t, ref.Pricing, model)
				_, err := billing.GetModelPricing(model)
				require.ErrorIs(t, err, ErrModelPricingUnavailable, "must not borrow another model's price: %s", model)
			}
		})
	}
}

func TestStepFunPricingCatalogAndChannelOverrides(t *testing.T) {
	billing := newEmptyCatalogBillingService(t)
	var err error
	billing.pricingService.pricingData, err = billing.pricingService.parsePricingData([]byte(`{"step-5-preview":{"litellm_provider":"stepfun","mode":"chat","input_cost_per_token":0.000002,"output_cost_per_token":0.000003,"cache_read_input_token_cost":0}}`))
	require.NoError(t, err)
	card := requireModelPricing(t, billing, "step-5-preview")
	require.InDelta(t, 2e-6, card.InputPricePerToken, 1e-15)
	require.Zero(t, card.CacheReadPricePerToken, "explicit zero must not be replaced with fallback")
	for _, model := range []string{"step-5-preview", "step-router-v1"} {
		resolved := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceChannel, BasePricing: &ModelPricing{InputPricePerToken: 4e-6, OutputPricePerToken: 5e-6, CacheReadPricePerToken: .5e-6}}
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: t.Context(), Model: model, Tokens: UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}, RateMultiplier: 1, Resolver: &ModelPricingResolver{}, Resolved: resolved})
		require.NoError(t, err)
		require.InDelta(t, 9.5, cost.TotalCost, 1e-12, "saved channel price must override defaults, including unpriced router")
	}
}

// Official prompt-cache example: 591 prompt tokens include 512 cached tokens;
// 79 misses + 512 hits + 120 output at Step 5 USD rates cost $0.0004286.
func TestStepFunOfficialCachedUsageAcrossProtocols(t *testing.T) {
	for _, kind := range []string{"oauth", "apikey"} {
		for _, stream := range []bool{false, true} {
			for _, protocol := range []string{"chat", "responses", "messages"} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", kind, protocol, stream), func(t *testing.T) {
					body := `{"id":"chat-step","object":"chat.completion","model":"step-5-preview","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":591,"cached_tokens":512,"completion_tokens":120,"total_tokens":711}}`
					contentType := "application/json"
					if stream {
						contentType = "text/event-stream"
						body = "data: " + `{"id":"chat-step","object":"chat.completion.chunk","model":"step-5-preview","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: " + `{"id":"chat-step","object":"chat.completion.chunk","model":"step-5-preview","choices":[],"usage":{"prompt_tokens":591,"cached_tokens":512,"completion_tokens":120,"total_tokens":711}}` + "\n\ndata: [DONE]\n\n"
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
					a := stepFunTestAccount(kind, "global")
					request := []byte(fmt.Sprintf(`{"model":"step-5-preview","input":"hello","messages":[{"role":"user","content":"hello"}],"max_tokens":256,"stream":%t}`, stream))
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "responses":
						result, err = svc.Forward(ctx, c, a, request)
					case "messages":
						result, err = svc.ForwardAsAnthropic(ctx, c, a, request, "", "")
					default:
						result, err = svc.ForwardAsChatCompletions(ctx, c, a, request, "", "")
					}
					require.NoError(t, err)
					require.Equal(t, 591, result.Usage.InputTokens)
					require.Equal(t, 512, result.Usage.CacheReadInputTokens)
					require.Equal(t, 120, result.Usage.OutputTokens)
					require.Zero(t, result.Usage.CacheCreationInputTokens)
					require.Contains(t, recorder.Body.String(), "512", "client usage must retain cache hits")
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					billingGateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					billingGateway.cfg.Default.RateMultiplier = 1
					require.NoError(t, billingGateway.RecordUsage(ctx, &OpenAIRecordUsageInput{
						Result: result, Account: a, User: &User{ID: 7}, APIKey: &APIKey{ID: 8, Group: &Group{Platform: PlatformStepFun, RateMultiplier: 1}},
					}))
					require.Equal(t, 1, billingRepo.calls)
					require.InDelta(t, .0004286, billingRepo.lastCmd.BalanceCost, 1e-12, "actual deduction must charge 79 misses, 512 hits, 120 output")
					require.Equal(t, 79, usageRepo.lastLog.InputTokens)
					require.Equal(t, 512, usageRepo.lastLog.CacheReadTokens)
					require.InDelta(t, .0004286, usageRepo.lastLog.TotalCost, 1e-12)
					requireStepFunWire(t, upstream.lastReq, true)
				})
			}
		}
	}
}
