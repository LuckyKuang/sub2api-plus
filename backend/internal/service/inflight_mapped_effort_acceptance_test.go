//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

// A public retail card remains the price owner after an account model mapping.
// Its hold must cover the actual mapped model's effort interpretation: this
// request forwards max to DeepSeek, so $2 x platform 2 x model 3 x max 4 is $48.
// The public alias's legacy max -> xhigh normalization cannot lower the hold.
func TestInflightMappedMaxEffortCoversPublicCardSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeToken} {
		for _, free := range []bool{false, true} {
			name := string(mode) + "/paid"
			price, expected := 2.0, 48.0
			if free {
				name, price, expected = string(mode)+"/explicit_zero", 0, 0
			}
			t.Run(name, func(t *testing.T) {
				const publicModel = "retail-model"
				const upstreamModel = "deepseek-v4-flash"
				body := []byte(`{"model":"retail-model","messages":[{"role":"user","content":"hello"}],"max_tokens":1,"reasoning_effort":"max","stream":false}`)
				card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{publicModel}, BillingMode: mode,
					ReasoningEffortMultipliers: map[string]float64{"max": 4, "xhigh": 0.5}}
				if mode == BillingModeToken {
					card.InputPrice, card.OutputPrice = billingFloat(0), &price
				} else {
					card.PerRequestPrice = &price
				}
				group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{card},
					BillingRatePolicy: domain.BillingRatePolicy{
						PlatformMultipliers: map[string]float64{PlatformOpenAI: 2},
						ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: publicModel, Multiplier: 3}},
					}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
				account := rawChatCompletionsTestAccount()
				account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: false}
				account.Credentials["model_mapping"] = map[string]any{publicModel: upstreamModel}
				cache := &inflightSnapshotCacheStub{byBucket: map[string][]Account{
					inflightBucketKey(group.ID, PlatformOpenAI): {*account},
				}}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"mapped-effort"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"mapped-effort","object":"chat.completion","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
				}}
				logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				svc.cfg = rawChatCompletionsTestConfig()
				svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1}
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
				svc.httpUpstream = upstream
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), publicModel)
				req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIChat, Model: publicModel,
					Kind: InflightEstimateToken, BodyBytes: len(body), MaxTokens: 1}).WithRequestPricingMetadata(body)
				hold, priced := svc.EstimateInflightReservation(ctx, key, req)

				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, upstreamModel, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, "max", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
				require.NotNil(t, result.ReasoningEffort)
				require.Equal(t, "max", *result.ReasoningEffort)
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user,
					Account: account, OriginalModel: publicModel}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, expected, ledger.lastCmd.BalanceCost, 1e-12)
				require.Equal(t, publicModel, logs.lastLog.BillingRateSnapshot["model"], "account mapping must not replace the retail model's factors")
				require.True(t, priced)
				if free {
					require.Zero(t, hold, "normalization candidates must not replace an explicit free retail card with the mapped SKU's paid catalog")
				} else {
					require.GreaterOrEqual(t, hold, 48.0, "preflight must cover the mapped max effort charged by actual forwarding and settlement")
				}
			})
		}
	}
}

func TestInflightMappedThinkingAndSuffixEffortsCoverActualWireSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, protocol, model, wireModel, body, response string
		oauth                                            bool
	}{
		{
			name: "mapped thinking fallback", protocol: ContentModerationProtocolOpenAIChat,
			model: "kimi-k2.5", wireModel: "kimi-k2.5",
			body:     `{"model":"retail-model","messages":[{"role":"user","content":"hello"}],"max_tokens":1,"thinking":{"type":"enabled"},"stream":false}`,
			response: `{"id":"mapped-thinking","object":"chat.completion","model":"kimi-k2.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		},
		{
			name: "mapped OAuth suffix", protocol: ContentModerationProtocolOpenAIResponses, oauth: true,
			model: "gpt-5.3-codex-high", wireModel: "gpt-5.3-codex",
			body:     `{"model":"retail-model","instructions":"suffix fixture","input":"hello","max_output_tokens":1,"stream":false}`,
			response: `{"id":"mapped-suffix","usage":{"input_tokens":1,"output_tokens":1}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const publicModel = "retail-model"
			body := []byte(tc.body)
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{publicModel},
					BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2),
					ReasoningEffortMultipliers: map[string]float64{"high": 4}}},
				BillingRatePolicy: domain.BillingRatePolicy{
					PlatformMultipliers: map[string]float64{PlatformOpenAI: 2},
					ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: publicModel, Multiplier: 3}},
				}}
			user := &User{ID: 22}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: false}
			account.Credentials["model_mapping"] = map[string]any{publicModel: tc.model}
			if tc.oauth {
				account.Type, account.Extra = AccountTypeOAuth, nil
				account.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account",
					"model_mapping": map[string]any{publicModel: tc.model}}
			}
			cache := &inflightSnapshotCacheStub{byBucket: map[string][]Account{
				inflightBucketKey(group.ID, PlatformOpenAI): {*account},
			}}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"mapped-implicit-effort"}},
				Body:   io.NopCloser(strings.NewReader(tc.response)),
			}}
			logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.cfg, svc.httpUpstream = rawChatCompletionsTestConfig(), upstream
			svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1}
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), publicModel)
			req := (InflightEstimateRequest{Protocol: tc.protocol, Model: publicModel,
				Kind: InflightEstimateToken, BodyBytes: len(body), MaxTokens: 1}).WithRequestPricingMetadata(body)
			require.Empty(t, req.ReasoningEffort, "the public alias alone does not declare a billing effort")
			hold, priced := svc.EstimateInflightReservation(ctx, key, req)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			path := "/v1/chat/completions"
			if tc.oauth {
				path = "/v1/responses"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			var result *OpenAIForwardResult
			var err error
			if tc.oauth {
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				result, err = svc.Forward(ctx, c, account, body)
			} else {
				result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wireModel, gjson.GetBytes(upstream.lastBody, "model").String())
			if !tc.oauth {
				require.Equal(t, "enabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
			}
			require.NotNil(t, result.ReasoningEffort)
			require.Equal(t, "high", *result.ReasoningEffort, "actual mapped forwarding determines this model's effort")
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user,
				Account: account, OriginalModel: publicModel}))
			require.NotNil(t, ledger.lastCmd)
			require.InDelta(t, 48.0, ledger.lastCmd.BalanceCost, 1e-12, "$2 x platform 2 x public model 3 x actual high effort 4")
			require.Equal(t, publicModel, logs.lastLog.BillingRateSnapshot["model"])
			require.True(t, priced)
			require.GreaterOrEqual(t, hold, 48.0)
		})
	}
}
