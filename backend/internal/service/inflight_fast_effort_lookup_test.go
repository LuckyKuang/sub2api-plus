//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fast and translated effort are independently billable dimensions of one
// admission. Both must see one account-pool generation and share its timeout;
// neither a success nor a failed read may trigger another pool read per tier.
func TestInflightFastAndEffortShareOneMetadataLookupPerEstimate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, gateway := range []string{"openai", "generic"} {
		for _, failure := range []bool{false, true} {
			name := gateway + "/success"
			if failure {
				name = gateway + "/timeout"
			}
			t.Run(name, func(t *testing.T) {
				svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
				account := rawChatCompletionsTestAccount()
				account.Status, account.Schedulable = StatusActive, true
				account.Extra = map[string]any{"openai_responses_supported": false}
				svc.cfg.Security = rawChatCompletionsTestConfig().Security
				key.Group.ModelPricing[0].ReasoningEffortMultipliers = map[string]float64{"xhigh": 4}
				svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
					Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority,
				}}}).settingService
				cache := &inflightFastEffortLookupCache{account: account}
				cache.unavailable.Store(failure)
				svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.WithValue(context.Background(), ctxkey.Group, key.Group), PlatformOpenAI), key, time.Now(), "gpt-5.6-sol")
				body := []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"xhigh","max_tokens":1,"stream":false}`)
				req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIChat, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata(body)
				estimate := svc.EstimateInflightReservation
				if gateway == "generic" {
					other := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot}
					estimate = other.EstimateInflightReservation
				}
				started := time.Now()
				amount, priced := estimate(ctx, key, req)
				elapsed := time.Since(started)
				require.True(t, priced)
				require.InDelta(t, 24, amount, 1e-12, "$2 output × Fast3 × actual xhigh4 must remain reserved")
				require.Equal(t, int64(1), cache.reads.Load(), "Fast and effort must reuse successful and failed reads")
				require.Less(t, elapsed, 3*time.Second, "one admission has one shared two-second metadata budget")
				require.NoError(t, ctx.Err(), "metadata expiry must not cancel the parent request")
				// A new admission gets fresh metadata. A failed read must not
				// poison a later independent request after dependency recovery.
				cache.unavailable.Store(false)
				amount, priced = estimate(ctx, key, req)
				require.True(t, priced)
				require.InDelta(t, 24, amount, 1e-12)
				require.Equal(t, int64(2), cache.reads.Load(), "memo lifetime is exactly one Estimate call")
				// Validate the independent $24 contract against actual forwarding
				// and settlement, including the final explicit effort on the wire.
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": []string{"application/json"}},
					Body:   io.NopCloser(strings.NewReader(`{"id":"fast-effort-lookup","model":"gpt-5.6-sol","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
				}}
				svc.httpUpstream = upstream
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
				result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "xhigh", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
				require.Equal(t, "priority", gjson.GetBytes(upstream.lastBody, "service_tier").String())
				require.Equal(t, "xhigh", optionalStringValue(result.ReasoningEffort))
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, 24, ledger.lastCmd.BalanceCost, 1e-12)
				require.Equal(t, int64(2), cache.reads.Load(), "forwarding and settlement reuse captured rules without more admission lookups")
			})
		}
	}
}

func TestInflightOrdinaryAndGroupOnlyFastAvoidMetadataReads(t *testing.T) {
	for _, gateway := range []string{"openai", "generic"} {
		for _, protocol := range []string{ContentModerationProtocolOpenAIChat, ContentModerationProtocolOpenAIResponses, ContentModerationProtocolAnthropicMessages} {
			for _, force := range []bool{false, true} {
				name := gateway + "/" + protocol + "/ordinary"
				if force {
					name = gateway + "/" + protocol + "/group Fast"
				}
				t.Run(name, func(t *testing.T) {
					svc, key, _, account := newInflightFastPolicySnapshotFixture(t)
					key.Group.ForceOpenAIFast = force
					cache := &inflightFastEffortLookupCache{account: account}
					svc.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, svc.cfg)
					ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-5.6-sol")
					body := []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hello"}],"max_tokens":1}`)
					if protocol == ContentModerationProtocolOpenAIResponses {
						body = []byte(`{"model":"gpt-5.6-sol","input":"hello","max_output_tokens":1}`)
					}
					// Real ingress always captures metadata, including absent effort.
					// The public card has no effort factors, so that capture must not
					// turn ordinary or group-only Fast admission into a pool lookup.
					req := (InflightEstimateRequest{Protocol: protocol, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata(body)
					estimate := svc.EstimateInflightReservation
					if gateway == "generic" {
						other := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot}
						estimate = other.EstimateInflightReservation
					}
					amount, priced := estimate(ctx, key, req)
					require.True(t, priced)
					want := 2.0
					if force {
						want = 6
					}
					require.InDelta(t, want, amount, 1e-12)
					require.Zero(t, cache.reads.Load(), "ordinary and group-only Fast do not depend on account metadata")
				})
			}
		}
	}
}

type inflightFastEffortLookupCache struct {
	SchedulerCache
	account     *Account
	reads       atomic.Int64
	unavailable atomic.Bool
}

func (cache *inflightFastEffortLookupCache) GetSnapshot(ctx context.Context, _ SchedulerBucket) ([]*Account, bool, error) {
	cache.reads.Add(1)
	if cache.unavailable.Load() {
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	return []*Account{cache.account}, true, nil
}
