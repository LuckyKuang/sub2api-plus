//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The admitted public card is also the settlement contract when a compatible
// supplier translates effort scales. Low/medium/xhigh are not necessarily the
// final forwarded level: GLM and Anthropic conversions change these values.
func TestInflightConvertedEffortCoversActualWireAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, protocol, upstreamModel, requested, forwarded string
		nativeAnthropic                                     bool
		mappedPolicy                                        bool
		poolUnavailable                                     string
		flatResponses                                       bool
	}{
		{"native responses flat effort does not enter typed converter", ContentModerationProtocolOpenAIResponses, "k3", "xhigh", "", true, false, "", true},
		{"direct responses flat effort remains supported", ContentModerationProtocolOpenAIResponses, "gpt-5.5", "xhigh", "xhigh", false, false, "", true},
		{"snapshot failure recovers GLM chat xhigh", ContentModerationProtocolOpenAIChat, "glm-5.2", "xhigh", "max", false, false, "failed", false},
		{"snapshot missing recovers GLM chat xhigh", ContentModerationProtocolOpenAIChat, "glm-5.2", "xhigh", "max", false, false, "missing", false},
		{"Grok messages omitted defaults medium", ContentModerationProtocolAnthropicMessages, "grok-4.5", "", "medium", false, false, "", false},
		{"mapped model policy promotes low", ContentModerationProtocolAnthropicMessages, "gpt-5.5", "low", "high", false, true, "", false},
		{"GLM chat medium to high", ContentModerationProtocolOpenAIChat, "glm-5.2", "medium", "high", false, false, "", false},
		{"GLM chat xhigh to max", ContentModerationProtocolOpenAIChat, "glm-5.2", "xhigh", "max", false, false, "", false},
		{"Grok chat xhigh to high", ContentModerationProtocolOpenAIChat, "grok-4.5", "xhigh", "high", false, false, "", false},
		{"GLM messages medium to high", ContentModerationProtocolAnthropicMessages, "glm-5.2", "medium", "high", false, false, "", false},
		{"GLM messages xhigh to max", ContentModerationProtocolAnthropicMessages, "glm-5.2", "xhigh", "max", false, false, "", false},
		{"GLM messages omitted defaults high", ContentModerationProtocolAnthropicMessages, "glm-5.2", "", "high", false, false, "", false},
		{"OpenAI messages omitted defaults medium", ContentModerationProtocolAnthropicMessages, "gpt-5.5", "", "medium", false, false, "", false},
		{"Anthropic chat xhigh to max", ContentModerationProtocolOpenAIChat, "k3", "xhigh", "max", true, false, "", false},
		{"Anthropic responses xhigh to max", ContentModerationProtocolOpenAIResponses, "k3", "xhigh", "max", true, false, "", false},
		{"native Anthropic messages preserves xhigh", ContentModerationProtocolAnthropicMessages, "k3", "xhigh", "xhigh", true, false, "", false},
		{"native GLM53 messages medium to high", ContentModerationProtocolAnthropicMessages, "glm-5.3", "medium", "high", true, false, "", false},
		{"native GLM53 messages xhigh to max", ContentModerationProtocolAnthropicMessages, "glm-5.3", "xhigh", "max", true, false, "", false},
		{"native Claude55 responses omitted defaults high", ContentModerationProtocolOpenAIResponses, "claude-sonnet-5-5", "", "high", true, false, "", false},
		{"GLM53 preserves low", ContentModerationProtocolOpenAIChat, "glm-5.3", "low", "low", false, false, "", false},
		{"unrelated chat preserves medium", ContentModerationProtocolOpenAIChat, "gpt-5.5", "medium", "medium", false, false, "", false},
	} {
		for _, free := range []bool{false, true} {
			name := tc.name + "/paid"
			price, want := 2.0, 48.0
			if tc.forwarded == "" {
				want = 12 // The converter omitted the incompatible field; unconfigured effort is factor 1.
			} else if tc.forwarded == tc.requested {
				want = 6 // $2 x platform 2 x public model 3 x the unchanged 0.5 effort.
			}
			if free {
				name, price, want = tc.name+"/explicit zero", 0, 0
			}
			t.Run(name, func(t *testing.T) {
				const publicModel = "retail-converted-model"
				platform := PlatformOpenAI
				account := forceChatMessagesFallbackAccount()
				if strings.HasPrefix(tc.upstreamModel, "grok-") {
					platform, account.Platform = PlatformGrok, PlatformGrok
				}
				if tc.nativeAnthropic {
					account = nativeAnthropicTestAccount()
					platform = account.Platform
				}
				if tc.flatResponses && !tc.nativeAnthropic {
					account.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses), "openai_passthrough": true}
				}
				account.Credentials["model_mapping"] = map[string]any{publicModel: tc.upstreamModel}
				effortMultipliers := map[string]float64{"low": 0.5, "medium": 0.5, "xhigh": 0.5, "high": 4, "max": 4}
				if tc.requested != tc.forwarded && tc.forwarded != "" {
					effortMultipliers[tc.forwarded] = 4
				}
				if tc.flatResponses {
					effortMultipliers["max"] = 0.25
				}
				group := &Group{ID: 77, Platform: platform, RateMultiplier: 1,
					ModelPricing: []ChannelModelPricing{{Platform: platform, Models: []string{publicModel},
						BillingMode: BillingModePerRequest, PerRequestPrice: &price,
						ReasoningEffortMultipliers: effortMultipliers}},
					BillingRatePolicy: domain.BillingRatePolicy{
						PlatformMultipliers: map[string]float64{platform: 2},
						ModelMultipliers:    []domain.BillingModelRate{{Platform: platform, Model: publicModel, Multiplier: 3}},
					}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
				cache := &inflightSnapshotCacheStub{byBucket: map[string][]Account{
					inflightBucketKey(group.ID, platform): {*account},
				}}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"converted-effort","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, tc.upstreamModel)))}
				if tc.nativeAnthropic {
					response = nativeAnthropicStreamResponse()
					if tc.protocol == ContentModerationProtocolAnthropicMessages {
						response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
							Body: io.NopCloser(strings.NewReader(`{"id":"native-message-effort","type":"message","role":"assistant","model":"k3","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))}
					}
				}
				if tc.flatResponses && !tc.nativeAnthropic {
					response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"direct-flat-effort","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}
				}
				upstream := &httpUpstreamRecorder{resp: response}
				logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				svc.cfg, svc.httpUpstream = rawChatCompletionsTestConfig(), upstream
				svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1}
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				var snapshotCache SchedulerCache = cache
				unavailable := &inflightUnavailableSnapshotCache{inflightSnapshotCacheStub: cache}
				if tc.poolUnavailable == "failed" {
					unavailable.unavailable.Store(true)
					snapshotCache = unavailable
				}
				svc.schedulerSnapshot = NewSchedulerSnapshotService(snapshotCache, nil, nil, nil, svc.cfg)
				if tc.poolUnavailable == "missing" {
					svc.schedulerSnapshot = nil
				}
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key, time.Now(), publicModel)
				if tc.mappedPolicy {
					ctx = WithOpenAIReasoningEffortPolicy(ctx, "", []ReasoningEffortMapping{{From: "low", To: "high", MatchType: "exact", Model: tc.upstreamModel}}, "")
				}
				path := "/v1/chat/completions"
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":1,"reasoning_effort":%q,"stream":false}`, publicModel, tc.requested))
				if tc.protocol == ContentModerationProtocolOpenAIResponses {
					path = "/v1/responses"
					body = []byte(fmt.Sprintf(`{"model":%q,"input":"hello","max_output_tokens":1,"reasoning":{"effort":%q},"stream":false}`, publicModel, tc.requested))
				} else if tc.protocol == ContentModerationProtocolAnthropicMessages {
					path = "/v1/messages"
					body = []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":1,"output_config":{"effort":%q},"stream":false}`, publicModel, tc.requested))
				}
				if tc.flatResponses {
					body = []byte(fmt.Sprintf(`{"model":%q,"input":"hello","max_output_tokens":1,"reasoning_effort":%q,"stream":false}`, publicModel, tc.requested))
				}
				req := (InflightEstimateRequest{Protocol: tc.protocol, Model: publicModel,
					Kind: InflightEstimateToken, BodyBytes: len(body), MaxTokens: 1}).WithRequestPricingMetadata(body)
				hold, priced := svc.EstimateInflightReservation(ctx, key, req)
				require.NoError(t, ctx.Err(), "a snapshot lookup timeout must not cancel the parent request")
				unavailable.unavailable.Store(false)
				if tc.poolUnavailable != "" {
					svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
					recovered, _, err := svc.schedulerSnapshot.ListSchedulableAccounts(ctx, key.GroupID, platform, false)
					require.NoError(t, err)
					require.Len(t, recovered, 1, "the scheduler can recover after admission metadata was unavailable")
					account = &recovered[0]
				}
				c := adaptiveProtocolTestContext(path, body)
				c.Request = c.Request.WithContext(ctx)
				var result *OpenAIForwardResult
				var err error
				switch tc.protocol {
				case ContentModerationProtocolAnthropicMessages:
					result, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
				case ContentModerationProtocolOpenAIResponses:
					result, err = svc.Forward(ctx, c, account, body)
				default:
					result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				effortPath := "reasoning_effort"
				if tc.nativeAnthropic {
					effortPath = "output_config.effort"
				}
				require.Equal(t, tc.forwarded, gjson.GetBytes(upstream.lastBody, effortPath).String())
				require.Equal(t, tc.forwarded, optionalStringValue(result.ReasoningEffort))
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user,
					Account: account, OriginalModel: publicModel}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, want, ledger.lastCmd.BalanceCost, 1e-12)
				require.Equal(t, publicModel, logs.lastLog.BillingRateSnapshot["model"])
				require.True(t, priced)
				if free || tc.forwarded == tc.requested {
					require.InDelta(t, want, hold, 1e-12, "unrelated routes and explicit zero must retain their own public quote")
				} else {
					require.GreaterOrEqual(t, hold, want, "admission must cover the actual translated effort and its independently expected $%.2f charge", want)
				}
			})
		}
	}
}

// A failed admission read does not guarantee that account selection will fail:
// the dependency can recover before a later retry selects its mapped owner.
type inflightUnavailableSnapshotCache struct {
	*inflightSnapshotCacheStub
	unavailable atomic.Bool
}

func (s *inflightUnavailableSnapshotCache) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	if s.unavailable.Load() {
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	return s.inflightSnapshotCacheStub.GetSnapshot(ctx, bucket)
}
