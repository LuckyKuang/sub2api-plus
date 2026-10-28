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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// An account mapping is a forwarding instruction. A known Luna model does not
// require its own custom card to reach the configured Sol account, while an
// accepted public card, explicit zero and requested-model billing remain retail
// contracts. All expected amounts below come from 1000 input + 1000 output
// tokens, independently of the production estimators and billing calculators.
func TestInflightAccountMappedLunaPricingCoversRealForwardAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6-luna", "gpt-5.6-luna"} {
		for _, protocol := range []string{"responses", "chat via responses", "raw chat"} {
			for _, tc := range []struct {
				name, publicSource, billingSource string
				free, partial, sellingFactors     bool
				want                              float64
			}{
				{name: "no public custom card", want: .012},
				{name: "group public card", publicSource: PricingSourceGroup, want: .010},
				{name: "channel public card", publicSource: PricingSourceChannel, want: .010},
				{name: "group explicit free", publicSource: PricingSourceGroup, free: true, want: 0},
				{name: "channel explicit free", publicSource: PricingSourceChannel, free: true, want: 0},
				{name: "partial public card inherits same model", publicSource: PricingSourceGroup, partial: true, want: .0035},
				{name: "explicit requested source", billingSource: BillingModelSourceRequested, want: .0006},
				{name: "explicit upstream source", publicSource: PricingSourceGroup, billingSource: BillingModelSourceUpstream, want: .012},
				{name: "platform and public model factors", sellingFactors: true, want: .072},
			} {
				t.Run(model+"/"+protocol+"/"+tc.name, func(t *testing.T) {
					want := tc.want
					if model == "gpt-5.6-luna" {
						if tc.partial {
							want = .0042 // $3/M explicit input + $1.2/M same-name output.
						} else if tc.billingSource == BillingModelSourceRequested {
							want = .0014 // $0.2/M input + $1.2/M output.
						}
					}
					group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, Hydrated: true}
					prices := []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: BillingModeToken,
						InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(10e-6),
						CacheWritePrice: billingFloat(2.5e-6), CacheReadPrice: billingFloat(.2e-6)}}
					if tc.publicSource != "" {
						card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeToken,
							InputPrice: billingFloat(3e-6), OutputPrice: billingFloat(7e-6)}
						if tc.free {
							card.InputPrice, card.OutputPrice = billingFloat(0), billingFloat(0)
						}
						if tc.partial {
							card.OutputPrice = nil
						}
						if tc.publicSource == PricingSourceGroup {
							group.ModelPricing = []ChannelModelPricing{card}
						} else {
							prices = append(prices, card)
						}
					}
					if tc.sellingFactors {
						group.BillingRatePolicy = domain.BillingRatePolicy{
							PlatformMultipliers: map[string]float64{PlatformOpenAI: 2},
							ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: model, Multiplier: 3}},
						}
					}
					billingSource := tc.billingSource
					if billingSource == "" {
						billingSource = BillingModelSourceChannelMapped
					}
					cache := populateChannelCache([]Channel{{ID: 901, GroupIDs: []int64{group.ID}, Status: StatusActive,
						BillingModelSource: billingSource, RestrictModels: false, ModelPricing: prices}}, map[int64]string{group.ID: PlatformOpenAI})
					channels := &ChannelService{frozenCache: cache}
					channels.cache.Store(cache)
					logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					svc.cfg = rawChatCompletionsTestConfig()
					svc.cfg.Default.RateMultiplier = 1
					svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000}
					svc.channelService, svc.resolver = channels, NewModelPricingResolver(channels, svc.billingService)
					account := structuredInputRetryTestAccount()
					account.Status, account.Schedulable = StatusActive, true
					account.Credentials["model_mapping"] = map[string]any{model: "gpt-6-sol"}
					if protocol == "raw chat" {
						account.Extra["openai_responses_supported"] = false
					}
					svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightSnapshotCacheStub{byBucket: map[string][]Account{
						inflightBucketKey(group.ID, PlatformOpenAI): {*account},
					}}, nil, nil, nil, svc.cfg)
					key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &User{ID: 22, Balance: 100}}
					ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), model)
					require.NoError(t, ValidateBillingPricingSnapshot(ctx))
					mapping := channels.ResolveChannelMapping(ctx, group.ID, model)
					require.False(t, mapping.Mapped, "the channel must not accidentally substitute for the account mapping under test")
					require.False(t, channels.IsModelRestricted(ctx, group.ID, model), "a missing custom card cannot restrict this channel")
					path, contentProtocol := "/v1/responses", ContentModerationProtocolOpenAIResponses
					body := []byte(fmt.Sprintf(`{"model":%q,"input":%q,"max_output_tokens":1000,"stream":false}`, model, strings.Repeat("a", 4000)))
					response := `{"id":"account-mapped-pricing","object":"response","model":"gpt-6-sol","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":1000,"total_tokens":2000}}`
					contentType := "application/json"
					if protocol != "responses" {
						path, contentProtocol = "/v1/chat/completions", ContentModerationProtocolOpenAIChat
						body = []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}],"max_tokens":1000,"stream":false}`, model, strings.Repeat("a", 4000)))
						if protocol == "raw chat" {
							response = `{"id":"account-mapped-pricing","object":"chat.completion","model":"gpt-6-sol","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":1000,"total_tokens":2000}}`
						} else {
							contentType = "text/event-stream"
							response = `data: {"type":"response.completed","response":{"id":"account-mapped-pricing","model":"gpt-6-sol","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1000,"output_tokens":1000,"total_tokens":2000}}}` + "\n\n"
						}
					}
					req := (InflightEstimateRequest{Protocol: contentProtocol, Model: model, Kind: InflightEstimateToken,
						BodyBytes: len(body), MaxTokens: 1000}).WithRequestPricingMetadata(body)
					held, priced := svc.EstimateInflightReservation(ctx, key, req)
					require.True(t, priced, "a known requested model remains priceable without a custom public card")
					if tc.publicSource != "" && tc.billingSource != BillingModelSourceUpstream {
						require.InDelta(t, want, held, 1e-12, "accepted public card or zero is authoritative before and after mapping")
					}
					// The preselection estimate has no final credential owner. Once
					// selected, that owner's actual mapping must be covered before
					// sending; the selected estimate cannot depend on other accounts.
					svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightAccountMappingForbiddenCache{t: t}, nil, nil, nil, svc.cfg)
					selected, selectedPriced := svc.EstimateInflightReservationForAccount(ctx, key, req, account)
					require.True(t, selectedPriced)
					require.InDelta(t, want, selected, 1e-12, "the selected owner must reserve the independently specified settlement without replacing the retail contract")
					generic := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, channelService: channels,
						resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot}
					genericSelected, genericPriced := generic.EstimateInflightReservationForAccount(ctx, key, req, account)
					require.True(t, genericPriced)
					require.InDelta(t, want, genericSelected, 1e-12, "both gateways share the selected account's accepted retail contract")
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
						Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}}
					svc.httpUpstream = upstream
					c := adaptiveProtocolTestContext(path, body)
					c.Request = c.Request.WithContext(ctx)
					var result *OpenAIForwardResult
					var err error
					if protocol == "responses" {
						result, err = svc.Forward(ctx, c, account, body)
					} else {
						result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requests, 1)
					wantEndpoint := "/v1/responses"
					if protocol == "raw chat" {
						wantEndpoint = "/v1/chat/completions"
					}
					require.Equal(t, wantEndpoint, upstream.lastReq.URL.Path)
					require.Equal(t, "gpt-6-sol", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, "gpt-6-sol", result.BillingModel)
					require.Equal(t, "gpt-6-sol", result.UpstreamModel)
					require.Equal(t, 1000, result.Usage.InputTokens)
					require.Equal(t, 1000, result.Usage.OutputTokens)
					require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account,
						ChannelUsageFields: ChannelUsageFields{ChannelID: 901, OriginalModel: model, ChannelMappedModel: model, BillingModelSource: billingSource}}))
					require.Equal(t, 1, ledger.calls)
					require.NotNil(t, ledger.lastCmd)
					require.InDelta(t, want, ledger.lastCmd.BalanceCost, 1e-12, "settlement must follow the independently specified retail contract")
					require.NotNil(t, logs.lastLog)
					require.Equal(t, model, logs.lastLog.RequestedModel)
					require.Equal(t, model, logs.lastLog.BillingRateSnapshot["model"], "model selling factors belong to the public model")
					require.NotNil(t, logs.lastLog.UpstreamModel)
					require.Equal(t, "gpt-6-sol", *logs.lastLog.UpstreamModel)
				})
			}
		}
	}
}

// Only the selected owner can determine this estimate. A failover from an
// expensive owner to a cheaper one, and back, must use each owner's current
// mapping even when they share the same public model and frozen price snapshot.
func TestInflightAccountMappedEstimateFollowsSelectedOwnerWithoutPoolAccess(t *testing.T) {
	const publicModel = "gpt-6-luna"
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, Hydrated: true}
	key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &User{ID: 22, Balance: 100}}
	channels := newChannelServiceWithPricings(group.ID, []ChannelModelPricing{{Platform: PlatformOpenAI,
		Models: []string{"gpt-6-sol"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(10e-6)}})
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(&openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{},
		&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.cfg = rawChatCompletionsTestConfig()
	svc.cfg.Default.RateMultiplier = 1
	svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000}
	svc.channelService, svc.resolver = channels, NewModelPricingResolver(channels, svc.billingService)
	svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightAccountMappingForbiddenCache{t: t}, nil, nil, nil, svc.cfg)
	ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), publicModel)
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	body := []byte(fmt.Sprintf(`{"model":%q,"input":%q,"max_output_tokens":1000}`, publicModel, strings.Repeat("a", 4000)))
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: publicModel,
		Kind: InflightEstimateToken, BodyBytes: len(body), MaxTokens: 1000}).WithRequestPricingMetadata(body)
	expensive, cheap, unchanged := structuredInputRetryTestAccount(), structuredInputRetryTestAccount(), structuredInputRetryTestAccount()
	expensive.ID, cheap.ID, unchanged.ID = 1, 2, 3
	for _, account := range []*Account{expensive, cheap, unchanged} {
		account.Status, account.Schedulable = StatusActive, true
	}
	expensive.Credentials["model_mapping"] = map[string]any{publicModel: "gpt-6-sol"}
	cheap.Credentials["model_mapping"] = map[string]any{publicModel: "gpt-5.6-luna"}
	generic := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, channelService: channels,
		resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot}
	for _, gateway := range []struct {
		name     string
		estimate func(context.Context, *APIKey, InflightEstimateRequest, *Account) (float64, bool)
	}{{"openai", svc.EstimateInflightReservationForAccount}, {"generic", generic.EstimateInflightReservationForAccount}} {
		t.Run(gateway.name, func(t *testing.T) {
			for _, owner := range []struct {
				account *Account
				want    float64
			}{{expensive, .012}, {cheap, .0014}, {unchanged, .0006}, {expensive, .012}} {
				amount, priced := gateway.estimate(ctx, key, req, owner.account)
				require.True(t, priced)
				require.InDelta(t, owner.want, amount, 1e-12, "selected owner %d defines the final mapped token prices", owner.account.ID)
				require.Equal(t, publicModel, req.Model, "owner changes cannot replace the public request identity")
			}
		})
	}
}

type inflightAccountMappingForbiddenCache struct {
	SchedulerCache
	t *testing.T
}

func (cache *inflightAccountMappingForbiddenCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	cache.t.Fatal("selected-account estimates must not read or wait for another account pool")
	return nil, false, nil
}
