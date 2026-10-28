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

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Messages can reach OpenAI through Responses conversion. The public endpoint
// does not carry service_tier, but trusted group/global policy may add priority
// after admission. One output token costs $2 Standard and $6 priority.
func TestInflightOpenAIFastMessagesConversionCoversWireSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		force    bool
		free     bool
		beta     bool
		settings *OpenAIFastPolicySettings
		wantTier string
		wantCost float64
	}{
		{name: "ordinary conversion", wantCost: 2},
		{name: "beta header", beta: true, wantTier: "priority", wantCost: 6},
		{name: "group force", force: true, wantTier: "priority", wantCost: 6},
		{name: "global missing force", settings: &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority}}}, wantTier: "priority", wantCost: 6},
		{name: "free forced priority", force: true, free: true, wantTier: "priority", wantCost: 2},
		{name: "global filter overrides group force", force: true, settings: &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter}}}, wantCost: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
			key.Group.ForceOpenAIFast, key.Group.FreeOpenAIFast = tc.force, tc.free
			svc.settingService = newOpenAIGatewayServiceWithSettings(t, tc.settings).settingService
			account := rawChatCompletionsTestAccount()
			account.Status, account.Schedulable = StatusActive, true
			account.Extra = map[string]any{"openai_responses_supported": true}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
			svc.cfg.Security = rawChatCompletionsTestConfig().Security
			upstream := &httpUpstreamRecorder{resp: fastConversionResponse("gpt-5.6-sol")}
			svc.httpUpstream = upstream
			body := []byte(`{"model":"gpt-5.6-sol","max_tokens":1,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
			base := context.WithValue(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), ctxkey.Group, key.Group)
			ctx := svc.WithBillingPricingSnapshot(base, key, time.Now(), "gpt-5.6-sol")
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolAnthropicMessages, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata(body)
			req.AnthropicFast = tc.beta
			hold, priced := svc.EstimateInflightReservation(ctx, key, req)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
			if tc.beta {
				c.Request.Header.Set("Anthropic-Beta", claude.BetaFastMode)
			}
			result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "/v1/responses", upstream.lastReq.URL.Path)
			require.Equal(t, tc.wantTier, gjson.GetBytes(upstream.lastBody, "service_tier").String())
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account, Result: result}))
			require.NotNil(t, ledger.lastCmd)
			require.InDelta(t, tc.wantCost, ledger.lastCmd.BalanceCost, 1e-12)
			require.True(t, priced)
			require.InDelta(t, tc.wantCost, hold, 1e-12, "Messages admission must cover the policy actually sent and settled")
		})
	}
}

// Account aliases can be normalized again by Codex before a Fast-policy model
// whitelist is evaluated. The hold must use that actual upstream model too.
func TestInflightOpenAIFastWhitelistUsesNormalizedOAuthWireModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
	svc.cfg.Security = rawChatCompletionsTestConfig().Security
	svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		Scope: BetaPolicyScopeOAuth, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority,
		ModelWhitelist: []string{"gpt-5.3-codex"}, FallbackAction: BetaPolicyActionPass,
	}}}).settingService
	account := rawChatCompletionsTestAccount()
	account.Type, account.Status, account.Schedulable = AccountTypeOAuth, StatusActive, true
	account.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account", "model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.3-codex-high"}}
	svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
	upstream := &httpUpstreamRecorder{resp: fastConversionResponse("gpt-5.3-codex")}
	svc.httpUpstream = upstream
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello","max_output_tokens":1,"stream":false}`)
	ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-5.6-sol")
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata(body)
	hold, priced := svc.EstimateInflightReservation(ctx, key, req)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	result, err := svc.Forward(ctx, c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-5.3-codex", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "priority", gjson.GetBytes(upstream.lastBody, "service_tier").String())
	require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account, Result: result,
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-5.6-sol", BillingModelSource: BillingModelSourceRequested}}))
	require.NotNil(t, ledger.lastCmd)
	require.InDelta(t, 6, ledger.lastCmd.BalanceCost, 1e-12)
	require.True(t, priced)
	require.InDelta(t, 6, hold, 1e-12, "normalized whitelist may add priority only after account selection")
}

func fastConversionResponse(model string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fast-conversion\",\"status\":\"completed\",\"model\":\"" + model + "\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n"))}
}

func TestInflightOpenAIFastDetachedRoutingModelKeepsWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
	svc.cfg.Security = rawChatCompletionsTestConfig().Security
	svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		Scope: BetaPolicyScopeOAuth, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority,
		ModelWhitelist: []string{"gpt-5.3-codex"}, FallbackAction: BetaPolicyActionPass,
	}}}).settingService
	account := rawChatCompletionsTestAccount()
	account.Type, account.Status, account.Schedulable = AccountTypeOAuth, StatusActive, true
	account.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account",
		"model_mapping": map[string]any{"route-alias": "gpt-5.3-codex-high"}}
	svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
	upstream := &httpUpstreamRecorder{resp: fastConversionResponse("gpt-5.3-codex")}
	svc.httpUpstream = upstream
	base := context.WithValue(context.Background(), ctxkey.Group, key.Group)
	routed := WithCompositeRouteDecision(base, CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, PublicModel: "gpt-5.6-sol", UpstreamModel: "route-alias"})
	routed = svc.WithBillingPricingSnapshot(routed, key, time.Now(), "gpt-5.6-sol")
	ctx := CopyBillingPricingSnapshot(routed, base)
	_, routeStillInContext := ResolvedUpstreamModelFromContext(ctx)
	require.False(t, routeStillInContext, "detached settlement copies only the accepted snapshot")
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata([]byte(`{"model":"gpt-5.6-sol","input":"hello"}`))
	hold, priced := svc.EstimateInflightReservation(ctx, key, req)
	body := []byte(`{"model":"route-alias","input":"hello","max_output_tokens":1,"stream":false}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	result, err := svc.Forward(ctx, c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-5.3-codex", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "priority", gjson.GetBytes(upstream.lastBody, "service_tier").String())
	require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account, Result: result,
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-5.6-sol", BillingModelSource: BillingModelSourceRequested}}))
	require.NotNil(t, ledger.lastCmd)
	require.InDelta(t, 6, ledger.lastCmd.BalanceCost, 1e-12)
	require.True(t, priced)
	require.InDelta(t, 6, hold, 1e-12)
}

func TestInflightOpenAIFastMessagesChatConversionHonorsPolicyAndHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		force, beta bool
		settings    *OpenAIFastPolicySettings
		wantTier    string
		wantCost    float64
		blocked     bool
	}{
		{name: "group force", force: true, wantTier: "priority", wantCost: 6},
		{name: "global missing force", settings: &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority}}}, wantTier: "priority", wantCost: 6},
		{name: "beta header", beta: true, wantTier: "priority", wantCost: 6},
		{name: "filter beta header", beta: true, settings: &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter}}}, wantCost: 2},
		{name: "block forced priority", force: true, settings: &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionBlock}}}, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
			svc.cfg.Security = rawChatCompletionsTestConfig().Security
			key.Group.ForceOpenAIFast = tc.force
			svc.settingService = newOpenAIGatewayServiceWithSettings(t, tc.settings).settingService
			account := rawChatCompletionsTestAccount()
			account.Status, account.Schedulable = StatusActive, true
			account.Extra = map[string]any{"openai_responses_supported": false}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/json"}},
				Body:   io.NopCloser(strings.NewReader(`{"id":"fast-messages-chat","model":"gpt-5.6-sol","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
			}}
			svc.httpUpstream = upstream
			body := []byte(`{"model":"gpt-5.6-sol","max_tokens":1,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
			ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-5.6-sol")
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolAnthropicMessages, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1, AnthropicFast: tc.beta}).WithRequestPricingMetadata(body)
			hold, priced := svc.EstimateInflightReservation(ctx, key, req)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
			if tc.beta {
				c.Request.Header.Set("Anthropic-Beta", "fixture-other,"+claude.BetaFastMode)
			}
			result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			if tc.blocked {
				var denied *OpenAIFastBlockedError
				require.ErrorAs(t, err, &denied)
				require.Nil(t, result)
				require.Nil(t, upstream.lastReq, "blocked policy must run before upstream writes")
				require.Nil(t, ledger.lastCmd, "blocked policy cannot charge")
				require.True(t, priced)
				require.Zero(t, hold, "known blocked pool cannot produce customer work")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.Equal(t, tc.wantTier, gjson.GetBytes(upstream.lastBody, "service_tier").String())
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account, Result: result}))
			require.NotNil(t, ledger.lastCmd)
			require.InDelta(t, tc.wantCost, ledger.lastCmd.BalanceCost, 1e-12)
			require.True(t, priced)
			require.InDelta(t, tc.wantCost, hold, 1e-12, "Chat conversion admission follows the actual configured policy")
		})
	}
}

func TestInflightOpenAIFastDoesNotChangeNativeAnthropic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
	svc.cfg.Security = rawChatCompletionsTestConfig().Security
	key.Group.Platform, key.Group.ForceOpenAIFast, key.Group.FreeOpenAIFast = PlatformComposite, true, true
	key.Group.ModelPricing[0].Platform = PlatformKimi
	svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{Scope: BetaPolicyScopeAll, ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority}}}).settingService
	account := nativeAnthropicTestAccount()
	account.Status, account.Schedulable = StatusActive, true
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"id":"native-fast-isolation","type":"message","role":"assistant","model":"gpt-5.6-sol","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
	}}
	svc.httpUpstream = upstream
	body := []byte(`{"model":"gpt-5.6-sol","max_tokens":1,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	base := WithResolvedTargetPlatform(context.WithValue(context.Background(), ctxkey.Group, key.Group), PlatformKimi)
	ctx := svc.WithBillingPricingSnapshot(base, key, time.Now(), "gpt-5.6-sol")
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolAnthropicMessages, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1, AnthropicFast: true}).WithRequestPricingMetadata(body)
	hold, priced := svc.EstimateInflightReservation(ctx, key, req)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Anthropic-Beta", claude.BetaFastMode)
	result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "/anthropic/v1/messages", upstream.lastReq.URL.Path)
	require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
	require.Nil(t, result.ServiceTier)
	require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account, Result: result}))
	require.NotNil(t, ledger.lastCmd)
	require.InDelta(t, 2, ledger.lastCmd.BalanceCost, 1e-12)
	require.True(t, priced)
	require.InDelta(t, 2, hold, 1e-12)
}
