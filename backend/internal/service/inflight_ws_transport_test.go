//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// A configured passthrough mode does not establish the actual transport. Forced
// HTTP and large, self-contained first turns still apply account mappings;
// opaque continuations must keep their original passthrough connection.
func TestResponsesWSInflightTransportUsesActualIngressRoute(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*OpenAIGatewayService, *Account)
		payload     string
		forceHTTP   bool
		passthrough bool
	}{
		{name: "small passthrough frame", passthrough: true},
		{name: "global force HTTP", forceHTTP: true, mutate: func(s *OpenAIGatewayService, _ *Account) { s.cfg.Gateway.OpenAIWS.ForceHTTP = true }},
		{name: "WS disabled", forceHTTP: true, mutate: func(s *OpenAIGatewayService, _ *Account) { s.cfg.Gateway.OpenAIWS.Enabled = false }},
		{name: "API key WS disabled", forceHTTP: true, mutate: func(s *OpenAIGatewayService, _ *Account) { s.cfg.Gateway.OpenAIWS.APIKeyEnabled = false }},
		{name: "OAuth WS disabled", forceHTTP: true, mutate: func(s *OpenAIGatewayService, a *Account) {
			a.Type = AccountTypeOAuth
			s.cfg.Gateway.OpenAIWS.OAuthEnabled = false
		}},
		{name: "account force HTTP", forceHTTP: true, mutate: func(_ *OpenAIGatewayService, a *Account) { a.Extra["openai_ws_force_http"] = true }},
		{name: "runtime transport disabled", forceHTTP: true, mutate: func(s *OpenAIGatewayService, _ *Account) {
			s.settingService.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: false})
		}},
		{name: "Grok always bridges", forceHTTP: true, mutate: func(_ *OpenAIGatewayService, a *Account) { a.Platform = PlatformGrok }},
		{name: "OAuth plugin route bridges", forceHTTP: true, mutate: func(s *OpenAIGatewayService, a *Account) {
			a.Type = AccountTypeOAuth
			s.pluginManager = &PluginManager{}
			s.pluginManager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100})
		}},
		{name: "large full-history first frame bridges", payload: `{"type":"response.create","model":"gpt-6-luna","input":"` + strings.Repeat("x", 256) + `"}`},
		{name: "large opaque continuation keeps passthrough", passthrough: true, payload: `{"type":"response.create","model":"gpt-6-luna","previous_response_id":"resp_prior","input":"` + strings.Repeat("x", 256) + `"}`},
		{name: "large empty continuation bridges", payload: `{"type":"response.create","model":"gpt-6-luna","previous_response_id":"","input":"` + strings.Repeat("x", 256) + `"}`},
		{name: "HTTP bridge mode maps", mutate: func(_ *OpenAIGatewayService, a *Account) {
			a.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
		}},
		{name: "pool mode maps", mutate: func(_ *OpenAIGatewayService, a *Account) {
			a.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
		}},
		{name: "router disabled maps", mutate: func(s *OpenAIGatewayService, _ *Account) { s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = false }},
		{name: "large frame with bridge disabled stays passthrough", passthrough: true,
			payload: `{"type":"response.create","model":"gpt-6-luna","input":"` + strings.Repeat("x", 256) + `"}`,
			mutate:  func(s *OpenAIGatewayService, _ *Account) { s.cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, account := newResponsesWSInflightTransportTestService()
			if tc.mutate != nil {
				tc.mutate(s, account)
			}
			payload := tc.payload
			if payload == "" {
				payload = `{"type":"response.create","model":"gpt-6-luna","input":"hi"}`
			}
			ctx := s.WithResponsesWebSocketInflightTransport(context.Background(), account, []byte(payload))
			require.Equal(t, tc.forceHTTP, s.responsesWSForceHTTPBridge(ctx, account), "the proxy must reuse the captured raw force condition")
			require.Equal(t, tc.passthrough, responsesWSInflightPassthrough(ctx, account), "only a real passthrough route may skip account model mapping")
			require.Equal(t, tc.forceHTTP, s.responsesWSForceHTTPBridge(context.Background(), account), "direct proxy calls preserve their existing raw routing decision")
		})
	}
}

func newResponsesWSInflightTransportTestService() (*OpenAIGatewayService, *Account) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = true
	cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes = 128
	settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	settings.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true})
	account := &Account{ID: 1701, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}}
	return &OpenAIGatewayService{cfg: cfg, settingService: settings}, account
}

func TestResponsesWSInflightTransportIsFrozenPerAttemptAndOwner(t *testing.T) {
	s, account := newResponsesWSInflightTransportTestService()
	payload := []byte(`{"type":"response.create","model":"gpt-6-luna","input":"hi"}`)
	ctx := s.WithResponsesWebSocketInflightTransport(context.Background(), account, payload)
	require.True(t, responsesWSInflightPassthrough(ctx, account))
	require.False(t, s.responsesWSForceHTTPBridge(ctx, account))

	// A setting change between reservation and proxy execution cannot change
	// this attempt's route, but every subsequent capture must see that change.
	s.settingService.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: false})
	require.False(t, s.responsesWSForceHTTPBridge(ctx, account))
	require.True(t, responsesWSInflightPassthrough(ctx, account))
	retry := s.WithResponsesWebSocketInflightTransport(ctx, account, payload)
	require.True(t, s.responsesWSForceHTTPBridge(retry, account))
	require.False(t, responsesWSInflightPassthrough(retry, account), "same-owner retries must replace the inherited decision")
	require.True(t, responsesWSInflightPassthrough(ctx, account), "a retry must not mutate another attempt's snapshot")

	other := *account
	other.ID++
	require.True(t, s.responsesWSForceHTTPBridge(ctx, &other), "a new owner must read current force rules")
	require.False(t, responsesWSInflightPassthrough(ctx, &other), "a different owner cannot inherit a mapping exemption")
	s.settingService.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true})
	require.False(t, s.responsesWSForceHTTPBridge(retry, &other), "another owner's stale force-HTTP flag cannot change this account's route")
	otherCtx := s.WithResponsesWebSocketInflightTransport(retry, &other, payload)
	require.True(t, responsesWSInflightPassthrough(otherCtx, &other))
	require.False(t, responsesWSInflightPassthrough(otherCtx, account))

	// A retry's newly supplied frame must also replace a same-owner size-based
	// decision; retaining a reference to an old frame cannot determine routing.
	large := []byte(`{"type":"response.create","model":"gpt-6-luna","input":"` + strings.Repeat("x", 256) + `"}`)
	bridged := s.WithResponsesWebSocketInflightTransport(otherCtx, &other, large)
	require.False(t, responsesWSInflightPassthrough(bridged, &other))
	large[0] = '!'
	require.False(t, responsesWSInflightPassthrough(bridged, &other), "the decision must not retain or reread frame contents")
	smallRetry := s.WithResponsesWebSocketInflightTransport(bridged, &other, payload)
	require.True(t, responsesWSInflightPassthrough(smallRetry, &other))
}

func TestResponsesWSInflightTransportWithoutAuthorityMapsConservatively(t *testing.T) {
	_, account := newResponsesWSInflightTransportTestService()
	require.False(t, responsesWSInflightPassthrough(context.Background(), account))
	require.False(t, responsesWSInflightPassthrough(nil, account))
	require.False(t, responsesWSInflightPassthrough(context.Background(), nil))
}
