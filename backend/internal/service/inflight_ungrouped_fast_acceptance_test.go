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

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// An ungrouped key pays the catalog's $30/M output price for GPT-5.5,
// or $75/M when the global policy forwards priority. Group absence cannot
// suppress that policy during either ingress or selected-owner admission.
func TestInflightUngroupedFastPolicyCoversWireAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, gateway := range []string{"openai default", "openai resolved", "generic resolved"} {
		for _, test := range []struct {
			name       string
			tier       string
			rule       OpenAIFastPolicyRule
			wantTier   string
			wantCharge float64
		}{
			{name: "global missing force", rule: OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll}, wantTier: "priority", wantCharge: 75e-6},
			{name: "global explicit filter", tier: "priority", rule: OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter, Scope: BetaPolicyScopeAll}, wantCharge: 30e-6},
			{name: "matching user force", rule: OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll, UserIDs: []int64{42}}, wantTier: "priority", wantCharge: 75e-6},
			{name: "other user rule passes", rule: OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll, UserIDs: []int64{99}}, wantCharge: 30e-6},
		} {
			t.Run(gateway+"/"+test.name, func(t *testing.T) {
				svc, key, ledger, _ := newInflightFastPolicySnapshotFixture(t)
				key.Group, key.GroupID = nil, nil
				account := rawChatCompletionsTestAccount()
				account.Status, account.Schedulable = StatusActive, true
				account.Extra = map[string]any{"openai_responses_supported": false}
				svc.cfg.Security = rawChatCompletionsTestConfig().Security
				svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{test.rule}}).settingService
				svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
				ctx := context.WithValue(context.Background(), ctxkey.UserID, key.UserID)
				if gateway != "openai default" {
					ctx = WithResolvedTargetPlatform(ctx, PlatformOpenAI)
				}
				other := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot, settingService: svc.settingService}
				estimate := svc.EstimateInflightReservation
				estimateSelected := svc.EstimateInflightReservationForAccount
				if gateway == "generic resolved" {
					ctx = other.WithBillingPricingSnapshot(ctx, key, time.Now(), "gpt-5.5")
					estimate, estimateSelected = other.EstimateInflightReservation, other.EstimateInflightReservationForAccount
				} else {
					ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), "gpt-5.5")
				}
				require.Nil(t, BillingPricingSnapshotFromContext(ctx), "ungrouped pricing has no group snapshot")
				body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"max_tokens":1}`)
				if test.tier != "" {
					body = []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"max_tokens":1,"service_tier":"` + test.tier + `"}`)
				}
				req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIChat, Model: "gpt-5.5", MaxTokens: 1}).WithRequestPricingMetadata(body)
				hold, priced := estimate(ctx, key, req)
				require.True(t, priced)
				require.InDelta(t, test.wantCharge, hold, 1e-12)
				selectedHold, selectedPriced := estimateSelected(ctx, key, req, account)
				require.True(t, selectedPriced)
				require.InDelta(t, test.wantCharge, selectedHold, 1e-12)
				// An administrator may change the rules after admission. Forwarding
				// must retain this request's accepted policy without a group snapshot.
				svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{}).settingService

				// Capture the actual outbound request and settle its returned usage;
				// expected dollar amounts above are independent of both paths.
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": []string{"application/json"}},
					Body:   io.NopCloser(strings.NewReader(`{"id":"ungrouped-fast","model":"gpt-5.5","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":1,"total_tokens":1}}`)),
				}}
				svc.httpUpstream = upstream
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
				result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, test.wantTier, gjson.GetBytes(upstream.lastBody, "service_tier").String())
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, test.wantCharge, ledger.lastCmd.BalanceCost, 1e-12)
			})
		}
	}
}

func TestInflightUngroupedFastPolicyRespectsGatewayPlatformDefaults(t *testing.T) {
	for _, test := range []struct {
		name         string
		generic      bool
		resolved     string
		force        string
		hasForce     bool
		group        string
		wantPlatform string
		wantCharge   float64
	}{
		{name: "OpenAI service default", wantPlatform: PlatformOpenAI, wantCharge: 75e-6},
		{name: "empty force falls through to OpenAI default", hasForce: true, wantPlatform: PlatformOpenAI, wantCharge: 75e-6},
		{name: "generic service has no OpenAI default", generic: true, wantCharge: 30e-6},
		{name: "resolved Grok remains Grok", resolved: PlatformGrok, wantPlatform: PlatformGrok, wantCharge: 30e-6},
		{name: "forced Grok remains Grok", hasForce: true, force: PlatformGrok, wantPlatform: PlatformGrok, wantCharge: 30e-6},
		{name: "force outranks resolved target", resolved: PlatformOpenAI, hasForce: true, force: PlatformGrok, wantPlatform: PlatformGrok, wantCharge: 30e-6},
		{name: "empty force falls through to resolved Grok", resolved: PlatformGrok, hasForce: true, wantPlatform: PlatformGrok, wantCharge: 30e-6},
		{name: "Grok group remains Grok", group: PlatformGrok, wantPlatform: PlatformGrok, wantCharge: 30e-6},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, key, _, account := newInflightFastPolicySnapshotFixture(t)
			key.Group, key.GroupID = nil, nil
			if test.group != "" {
				key.Group = &Group{ID: 77, Platform: test.group, RateMultiplier: 1, Hydrated: true}
				key.GroupID = &key.Group.ID
			}
			svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
				ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll,
			}}}).settingService
			ctx := context.WithValue(context.Background(), ctxkey.UserID, key.UserID)
			if test.resolved != "" {
				ctx = WithResolvedTargetPlatform(ctx, test.resolved)
			}
			if test.hasForce {
				ctx = context.WithValue(ctx, ctxkey.ForcePlatform, test.force)
			}
			other := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot, settingService: svc.settingService}
			estimate := svc.EstimateInflightReservation
			estimateSelected := svc.EstimateInflightReservationForAccount
			capture := svc.WithBillingPricingSnapshot
			if test.generic {
				estimate, estimateSelected = other.EstimateInflightReservation, other.EstimateInflightReservationForAccount
				capture = other.WithBillingPricingSnapshot
			}
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.5", MaxTokens: 1}).WithRequestPricingMetadata([]byte(`{"model":"gpt-5.5","input":"hello","max_output_tokens":1}`))
			// Exercise direct service calls with no pricing capture or injected
			// target. Each wrapper must honor the same default and precedence.
			hold, priced := estimate(ctx, key, req)
			require.True(t, priced)
			require.InDelta(t, test.wantCharge, hold, 1e-12)
			selected, selectedPriced := estimateSelected(ctx, key, req, account)
			require.True(t, selectedPriced)
			require.InDelta(t, test.wantCharge, selected, 1e-12)
			captured := capture(ctx, key, time.Now(), req.Model)
			require.Equal(t, test.wantPlatform, requestBillingPlatform(captured, key.Group, ""))
			require.Equal(t, test.wantPlatform == PlatformOpenAI, openAIFastPolicySettingsFromContext(captured) != nil)
			hold, priced = estimate(captured, key, req)
			require.True(t, priced)
			require.InDelta(t, test.wantCharge, hold, 1e-12)
		})
	}
}
