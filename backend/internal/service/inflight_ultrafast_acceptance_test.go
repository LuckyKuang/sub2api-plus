//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The public selling card charges $2 per output token, $6 for Fast and $12
// for Ultrafast. The actual wire model, after mapping, gates the group override.
// Admission, selected-account reconciliation, forwarding and settlement must
// preserve those independently specified charges for every supported ingress.
func TestInflightUltrafastMatchesMappedForwardingAndSettlement(t *testing.T) {
	const publicModel = "gpt-6.1-sol"
	for _, tc := range []struct {
		name, wire, clientTier, action string
		fast, free                     bool
		wantTier                       string
		want                           float64
	}{
		{name: "ultrafast alone", wire: "gpt-6.1-sol", wantTier: "ultrafast", want: 12},
		{name: "ultrafast wins Fast", wire: "gpt-6-astra", fast: true, wantTier: "ultrafast", want: 12},
		{name: "mapped unsupported keeps Standard", wire: "gpt-6-luna", want: 2},
		{name: "mapped unsupported falls back to Fast", wire: "gpt-6-luna", fast: true, wantTier: "priority", want: 6},
		{name: "unsupported retains client Flex", wire: "gpt-6-luna", clientTier: "flex", wantTier: "flex", want: 1},
		{name: "unsupported retains unknown client tier", wire: "gpt-6-luna", clientTier: "future", wantTier: "future", want: 2},
		{name: "filter group Ultrafast", wire: "gpt-6.1-sol", action: BetaPolicyActionFilter, want: 2},
		{name: "global priority replaces group Ultrafast", wire: "gpt-6.1-sol", action: OpenAIFastPolicyActionForcePriority, wantTier: "priority", want: 6},
		{name: "free Fast does not discount Ultrafast", wire: "gpt-6.1-sol", fast: true, free: true, wantTier: "ultrafast", want: 12},
		{name: "unsupported free Fast", wire: "gpt-6-luna", fast: true, free: true, wantTier: "priority", want: 2},
	} {
		for _, protocol := range []string{ContentModerationProtocolOpenAIResponses, ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages} {
			t.Run(tc.name+"/"+protocol, func(t *testing.T) {
				svc, key, ledger, account := newInflightFastPolicySnapshotFixture(t)
				key.Group.ForceOpenAIUltrafast, key.Group.ForceOpenAIFast, key.Group.FreeOpenAIFast = true, tc.fast, tc.free
				key.Group.ModelPricing[0].Models = []string{publicModel}
				account.Credentials = map[string]any{"model_mapping": map[string]any{publicModel: tc.wire}}
				settings := &OpenAIFastPolicySettings{}
				if tc.action != "" {
					settings.Rules = []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierUltrafast, Action: tc.action, Scope: BetaPolicyScopeAll}}
				}
				svc.settingService = newOpenAIGatewayServiceWithSettings(t, settings).settingService
				base := context.WithValue(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), ctxkey.Group, key.Group)
				ctx := svc.WithBillingPricingSnapshot(base, key, time.Now(), publicModel)
				require.NoError(t, ValidateBillingPricingSnapshot(ctx))
				req := InflightEstimateRequest{Protocol: protocol, Model: publicModel, Kind: InflightEstimateToken, MaxTokens: 1, ServiceTier: tc.clientTier}
				initial, priced := svc.EstimateInflightReservation(ctx, key, req)
				require.True(t, priced)
				require.InDelta(t, tc.want, initial, 1e-12)
				selected, priced := svc.EstimateInflightReservationForAccount(ctx, key, req, account)
				require.True(t, priced)
				require.InDelta(t, tc.want, selected, 1e-12)
				body := []byte(fmt.Sprintf(`{"model":%q,"service_tier":%q}`, publicModel, tc.clientTier))
				out, err := svc.applyOpenAIFastPolicyToBody(ctx, account, tc.wire, body)
				require.NoError(t, err)
				require.Equal(t, tc.wantTier, gjson.GetBytes(out, "service_tier").String())
				err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account,
					Result: &OpenAIForwardResult{RequestID: "ultrafast-admission", Model: publicModel, UpstreamModel: tc.wire,
						ServiceTier: extractOpenAIServiceTierFromBody(out), Usage: OpenAIUsage{OutputTokens: 1}}})
				require.NoError(t, err)
				require.InDelta(t, tc.want, ledger.lastCmd.BalanceCost, 1e-12)
			})
		}
	}
}

func TestInflightUltrafastSelectedAccountDoesNotReadAnotherOwner(t *testing.T) {
	svc, key, _, account := newInflightFastPolicySnapshotFixture(t)
	key.Group.ForceOpenAIUltrafast = true
	key.Group.ModelPricing[0].Models = []string{"gpt-6.1-sol"}
	account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"}}
	other := *account
	other.ID++
	other.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-astra"}}
	svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account, &other}}, nil, nil, nil, svc.cfg)
	ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-6.1-sol")
	req := InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-6.1-sol", MaxTokens: 1}
	amount, priced := svc.EstimateInflightReservation(ctx, key, req)
	require.True(t, priced)
	require.InDelta(t, 12, amount, 1e-12, "preselection retains the highest reachable tier")
	svc.schedulerSnapshot = nil
	amount, priced = svc.EstimateInflightReservationForAccount(ctx, key, req, account)
	require.True(t, priced)
	require.InDelta(t, 2, amount, 1e-12, "selection uses only this owner's unsupported wire model")
	amount, priced = svc.EstimateInflightReservationForAccount(ctx, key, req, &other)
	require.True(t, priced)
	require.InDelta(t, 12, amount, 1e-12)
}

func TestInflightUltrafastWebSocketUsesActualTransportModel(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		forceHTTP  bool
		want       float64
	}{
		{name: "passthrough retains supported client model", mode: OpenAIWSIngressModePassthrough, want: 12},
		{name: "pool maps to unsupported model", mode: OpenAIWSIngressModeCtxPool, want: 2},
		{name: "forced HTTP maps to unsupported model", mode: OpenAIWSIngressModePassthrough, forceHTTP: true, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, _, account := newInflightFastPolicySnapshotFixture(t)
			key.Group.ForceOpenAIUltrafast = true
			key.Group.ModelPricing[0].Models = []string{"gpt-6.1-sol"}
			svc.cfg.Gateway.OpenAIWS.Enabled = true
			svc.cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			svc.cfg.Gateway.OpenAIWS.ForceHTTP = tc.forceHTTP
			account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-luna"}}
			account.Extra = map[string]any{"openai_apikey_responses_websockets_v2_mode": tc.mode}
			base := context.WithValue(context.Background(), ctxkey.Group, key.Group)
			ctx := svc.WithBillingPricingSnapshot(base, key, time.Now(), "gpt-6.1-sol")
			frame := []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hi"}`)
			ctx = svc.WithResponsesWebSocketInflightTransport(ctx, account, frame)
			amount, priced := svc.EstimateInflightReservationForAccount(ctx, key,
				InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, ResponsesWebSocket: true, Model: "gpt-6.1-sol", MaxTokens: 1}, account)
			require.True(t, priced)
			require.InDelta(t, tc.want, amount, 1e-12)
			wire := "gpt-6-luna"
			if tc.mode == OpenAIWSIngressModePassthrough && !tc.forceHTTP {
				wire = "gpt-6.1-sol"
			}
			out, _, err := svc.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, wire, frame)
			require.NoError(t, err)
			if tc.want == 12 {
				require.Equal(t, "ultrafast", gjson.GetBytes(out, "service_tier").String())
			} else {
				require.False(t, gjson.GetBytes(out, "service_tier").Exists())
			}
		})
	}
}
