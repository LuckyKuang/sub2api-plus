//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Account probes obey the same two opt-ins and acquired credential snapshot as
// inference sends: no generic proxy argument, selected shadow or CLI defaults
// may replace that owner's token, identity, route or workspace.
func TestCodexCLIBridgeAccountProbePreservesAcquiredOwnerAndSingleSend(t *testing.T) {
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	owner := enabledCodexCLIExecutionAccount(301, "probe-owner-access", "probe-owner-workspace")
	owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color"
	proxyID := int64(701)
	owner.ProxyID = &proxyID
	owner.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "probe-owner-proxy.invalid", Port: 8080, Status: StatusActive}
	parent := owner.ID
	selected := &Account{ID: 302, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent, Concurrency: 9}
	recordRequestCredentialOwner(ctx, owner, "probe-owner-access")
	ctx = withRequestCredentialOwner(ctx, selected, "probe-owner-access")
	owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Windows 11; x86_64) wrong-terminal"
	owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = false
	owner.Proxy.Host = "later-row-mutation.invalid"
	const payload = `{"model":"gpt-6-astra","input":[{"role":"user","content":"probe"}],"stream":true}`
	bridgeCalls, upstreamCalls := 0, 0
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, settingService: enabledCodexCLIBridgeTestSettings(), codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, status codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		bridgeCalls++
		require.True(t, status.SourceVerified)
		require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", req.Header.Get("User-Agent"))
		return forward(ctx, req)
	}}}
	probe := &AccountTestService{openaiGatewayService: gateway, settingService: gateway.settingService, httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, resourceID int64, concurrency int) (*http.Response, error) {
		upstreamCalls++
		// Existing acquired-owner contract: selected logical row owns transport
		// resources/concurrency; acquired owner owns credentials and identity.
		require.EqualValues(t, 302, resourceID)
		require.Equal(t, 9, concurrency)
		require.Equal(t, "http://probe-owner-proxy.invalid:8080", proxy)
		require.Equal(t, "Bearer probe-owner-access", req.Header.Get("Authorization"))
		require.Equal(t, "probe-owner-workspace", req.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", req.Header.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
		require.Equal(t, "0.165.0", req.Header.Get("Version"))
		require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", req.URL.String())
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, payload, string(body))
		return openAIIdentityContractSuccessResponse(), nil
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer probe-owner-access")
	req.Header.Set("Chatgpt-Account-Id", "probe-owner-workspace")
	req.Header.Set("User-Agent", "forged-inbound/999")
	// The real probe request builder applies the native Codex identity before
	// entering this shared sender; exercise that boundary without test presets.
	probe.applyOpenAIOutboundIdentity(req.Context(), selected, req.Header, true)
	resp, err := probe.doOpenAIAccountTestUpstream(req, "http://forged-proxy.invalid:1", selected, false)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, 1, bridgeCalls)
	require.Equal(t, 1, upstreamCalls)
}

func TestCodexCLIBridgeAccountProbeGlobalDisabledUsesNativeTransport(t *testing.T) {
	settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: "false"}}}
	upstream := &httpUpstreamRecorder{resp: openAIIdentityContractSuccessResponse()}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		t.Fatal("disabled bridge intercepted account probe")
		return nil, nil
	}}}
	probe := &AccountTestService{openaiGatewayService: gateway, settingService: settings, httpUpstream: upstream}
	req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"probe"}`))
	require.NoError(t, err)
	resp, err := probe.doOpenAIAccountTestUpstreamPrepared(req, "", enabledCodexCLIExecutionAccount(301, "access", "workspace"), false)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Len(t, upstream.requests, 1)
}

func TestCodexCLIBridgeAccountProbeUnavailableMakesNoProviderWrites(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	status := codexCLIBridgeReadyFixture()
	status.ExecutionAvailable = false
	status.Message = "private-token /private/executable"
	gateway := &OpenAIGatewayService{settingService: enabledCodexCLIBridgeTestSettings(), codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: status, bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		t.Fatal("unavailable CLI launched")
		return nil, nil
	}}}
	probe := &AccountTestService{openaiGatewayService: gateway, httpUpstream: upstream}
	req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"probe"}`))
	require.NoError(t, err)
	resp, err := probe.doOpenAIAccountTestUpstreamPrepared(req, "", enabledCodexCLIExecutionAccount(301, "access", "workspace"), false)
	require.Nil(t, resp)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private")
	require.Empty(t, upstream.requests)
}

// Bridge faults must produce a static terminal 503. They are neither provider
// health signals nor permission to unschedule/replay a credential-owning account.
func TestCodexCLIBridgeTransportErrorsAreTerminalStaticAndDoNotUnschedule(t *testing.T) {
	for _, fixture := range []struct {
		name string
		err  error
	}{
		{"before_provider", &codexcli.BridgeError{Code: "bridge_startup_timeout", RequestSent: false}},
		{"after_provider", &codexcli.BridgeError{Code: "bridge_response_read_failed", RequestSent: true}},
		{"runtime_unavailable", &codexcli.TaskError{Code: "bridge_runtime_unavailable"}},
		{"setting_unavailable", ErrCodexCLIBridgeSettingUnavailable},
	} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", fixture.name, passthrough), func(t *testing.T) {
				repo := &openaiTransportAccountRepoStub{}
				svc := &OpenAIGatewayService{accountRepo: repo}
				account := enabledCodexCLIExecutionAccount(301, "access", "workspace")
				c, rec := newOpenAITransportErrTestContext()
				input := fmt.Errorf("private-token /private/path username/password authentication failed: %w", fixture.err)
				got := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account, input, passthrough)
				require.ErrorIs(t, got, fixture.err)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(got, &failover))
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				require.JSONEq(t, `{"error":{"type":"codex_cli_error","code":"cli_bridge_unavailable","message":"Codex CLI bridge unavailable"}}`, rec.Body.String())
				require.NotContains(t, rec.Body.String(), "private")
				require.Empty(t, repo.tempUnschedCalls)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				_, hasTransportEvent := c.Get(OpsUpstreamErrorsKey)
				require.False(t, hasTransportEvent, "raw diagnostic-bearing transport events must not be emitted for CLI faults")
				require.Nil(t, GetOpsRoutingDiagnostics(c))
			})
		}
	}
}
