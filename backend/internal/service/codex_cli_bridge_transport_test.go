//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func enabledCodexCLIBridgeTestSettings() *SettingService {
	return &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: "true"}}}
}

type codexCLIBridgeTransportStub struct {
	status codexcli.Status
	bridge func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error)
}

func (s codexCLIBridgeTransportStub) Status(context.Context) codexcli.Status { return s.status }
func (s codexCLIBridgeTransportStub) Bridge(ctx context.Context, status codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
	return s.bridge(ctx, status, req, forward)
}

// Requirements: global opt-in and an eligible credential owner's account opt-in
// are both necessary. An unrelated account must not depend on bridge storage;
// an opted-in account must never bypass its setting when storage is unavailable.
func TestCodexCLIBridgeRequiresBothGlobalAndEligibleAccountOptIn(t *testing.T) {
	ctx := context.Background()
	for _, global := range []string{"", "false", "true"} {
		for _, accountEnabled := range []bool{false, true} {
			account := enabledCodexCLIExecutionAccount(101, "access", "workspace")
			account.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = accountEnabled
			svc := &OpenAIGatewayService{settingService: &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: global}}}}
			enabled, err := svc.codexCLIBridgeEnabled(ctx, account)
			require.NoError(t, err)
			require.Equal(t, global == "true" && accountEnabled, enabled)
		}
	}
	broken := &SettingService{settingRepo: &codexCLIBridgeSettingRepo{readErr: errors.New("private-password")}}
	for _, kind := range []string{"nil", "disabled", "setup-token", "other-provider", "shadow", "pat"} {
		t.Run(kind, func(t *testing.T) {
			account := enabledCodexCLIExecutionAccount(101, "access", "workspace")
			switch kind {
			case "nil":
				account = nil
			case "disabled":
				account.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = false
			case "apikey":
				account.Type = AccountTypeAPIKey
			case "setup-token":
				account.Type = AccountTypeSetupToken
			case "other-provider":
				account.Platform = PlatformAnthropic
			case "shadow":
				parent := int64(200)
				account.ParentAccountID = &parent
			case "pat":
				account.Credentials["auth_mode"] = "personalAccessToken"
			}
			enabled, err := (&OpenAIGatewayService{settingService: broken}).codexCLIBridgeEnabled(ctx, account)
			require.NoError(t, err)
			require.False(t, enabled)
		})
	}
	enabled, err := (&OpenAIGatewayService{settingService: broken}).codexCLIBridgeEnabled(ctx, enabledCodexCLIExecutionAccount(101, "access", "workspace"))
	require.False(t, enabled)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private")
}

func codexCLIBridgeReadyFixture() codexcli.Status {
	return codexcli.Status{Installed: true, InstalledVersion: "0.165.0", TargetVersion: "0.165.0", VersionMatch: true, SourceVerified: true, ExecutionAvailable: true}
}

// The bridge is a transport boundary, not a second request compiler. The
// existing prepared request, opaque continuation/tool fields, trusted identity,
// workspace, credential owner and proxy must reach the established sender.
func TestCodexCLIBridgePreservesPreparedWireAndAccountFailoverOwnership(t *testing.T) {
	const body = `{"model":"gpt-6-astra","input":[{"type":"future_item","opaque":{"keep":true}},{"type":"function_call_output","call_id":"call-one","output":"tool output"}],"previous_response_id":"resp-existing","stream":true,"tools":[{"type":"function","name":"test","parameters":{"type":"object"}}]}`
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	settings := enabledCodexCLIBridgeTestSettings()
	upstreamCalls, bridgeCalls := 0, 0
	var expectedOwner int64
	var expectedProxy, expectedUA, expectedWorkspace, expectedAccess string
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, ownerID int64, _ int) (*http.Response, error) {
		upstreamCalls++
		bytes, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(bytes))
		require.Equal(t, expectedOwner, ownerID)
		require.Equal(t, expectedProxy, proxy)
		require.Equal(t, expectedUA, req.Header.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
		require.Equal(t, "0.165.0", req.Header.Get("Version"))
		require.Equal(t, expectedWorkspace, req.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "Bearer "+expectedAccess, req.Header.Get("Authorization"))
		require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n"))}, nil
	}}
	runtime := codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, status codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		bridgeCalls++
		require.True(t, status.SourceVerified)
		require.Equal(t, expectedUA, req.Header.Get("User-Agent"))
		return forward(ctx, req)
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings, httpUpstream: upstream, codexCLIBridgeRuntime: runtime}
	for _, fixture := range []struct {
		id                                int64
		access, workspace, terminal, host string
	}{
		{101, "owner-one-access", "workspace-one", "screen-256color", "proxy-one.invalid"},
		{202, "owner-two-access", "workspace-two", "xterm-256color", "proxy-two.invalid"},
	} {
		account := enabledCodexCLIExecutionAccount(fixture.id, fixture.access, fixture.workspace)
		account.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) " + fixture.terminal
		proxyID := fixture.id + 1000
		account.ProxyID = &proxyID
		account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: fixture.host, Port: 8080, Status: StatusActive}
		expectedOwner, expectedProxy = fixture.id, "http://"+fixture.host+":8080"
		expectedUA, expectedWorkspace, expectedAccess = account.Credentials["user_agent"].(string), fixture.workspace, fixture.access
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+fixture.access)
		req.Header.Set("Chatgpt-Account-Id", fixture.workspace)
		req.Header.Set("User-Agent", "untrusted-inbound/9")
		// Real Responses builders render Codex identity before the common sender;
		// preparation deliberately leaves the native Codex resolver authoritative.
		svc.applyOpenAIOutboundIdentity(req.Context(), account, req.Header, true)
		resp, err := svc.doOpenAIUpstream(req, "http://forged-proxy.invalid:1", account)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	require.Equal(t, 2, bridgeCalls)
	require.Equal(t, 2, upstreamCalls, "each newly selected account gets exactly one credential-owning send")
}

func TestCodexCLIBridgeUnavailableRuntimeNeverCallsNativeTransport(t *testing.T) {
	for _, field := range []string{"execution", "installed", "source", "version"} {
		t.Run(field, func(t *testing.T) {
			status := codexCLIBridgeReadyFixture()
			switch field {
			case "execution":
				status.ExecutionAvailable = false
			case "installed":
				status.Installed = false
			case "source":
				status.SourceVerified = false
			case "version":
				status.VersionMatch = false
			}
			status.Message = "private-access-token /private/path"
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: enabledCodexCLIBridgeTestSettings(), httpUpstream: upstream, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: status, bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
				t.Fatal("unverified runtime launched")
				return nil, nil
			}}}
			req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"hello"}`))
			require.NoError(t, err)
			resp, err := svc.doOpenAIUpstreamPrepared(req, "", enabledCodexCLIExecutionAccount(101, "access", "workspace"))
			require.Nil(t, resp)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.Empty(t, upstream.requests)
		})
	}
}

func TestCodexCLIBridgeErrorIsTerminalWithoutNativeFallbackOrAccountReplay(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	bridgeCalls := 0
	svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: enabledCodexCLIBridgeTestSettings(), httpUpstream: upstream, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		bridgeCalls++
		return nil, &codexcli.TaskError{Code: "bridge_process_failed"}
	}}}
	req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	resp, err := svc.doOpenAIUpstreamPrepared(req, "", enabledCodexCLIExecutionAccount(101, "access", "workspace"))
	require.Nil(t, resp)
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.Equal(t, 1, bridgeCalls)
	require.Empty(t, upstream.requests)
}

func TestCodexCLIBridgeUsesAcquiredCredentialOwnerInsteadOfSelectedShadow(t *testing.T) {
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	owner := enabledCodexCLIExecutionAccount(101, "acquired-access", "acquired-workspace")
	owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color"
	parent := owner.ID
	selected := &Account{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent, Concurrency: 7}
	recordRequestCredentialOwner(ctx, owner, "acquired-access")
	ctx = withRequestCredentialOwner(ctx, selected, "acquired-access")
	// This isolated fixture has no repository; its acquired owner determines
	// routing. Production live-permission tests separately cover account edits
	// without replacing the acquired credentials or identity.
	owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = false
	owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Windows 11; x86_64) terminal"
	bridgeCalls, upstreamCalls := 0, 0
	svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: enabledCodexCLIBridgeTestSettings(), httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, resourceID int64, concurrency int) (*http.Response, error) {
		upstreamCalls++
		// Existing acquired-owner contract: HTTPUpstream resources are charged
		// to the selected logical row, while wire declarations use its owner.
		require.EqualValues(t, 102, resourceID)
		require.Equal(t, 7, concurrency)
		require.Equal(t, "Bearer acquired-access", req.Header.Get("Authorization"))
		require.Equal(t, "acquired-workspace", req.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", req.Header.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
		require.Equal(t, "0.165.0", req.Header.Get("Version"))
		return openAIIdentityContractSuccessResponse(), nil
	}}, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		bridgeCalls++
		return forward(ctx, req)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer acquired-access")
	req.Header.Set("Chatgpt-Account-Id", "acquired-workspace")
	req.Header.Set("User-Agent", "forged-inbound/999")
	svc.applyOpenAIOutboundIdentity(req.Context(), selected, req.Header, true)
	resp, err := svc.doOpenAIUpstream(req, "", selected)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, 1, bridgeCalls)
	require.Equal(t, 1, upstreamCalls)
}

func TestCodexCLIBridgeGlobalDisableKeepsOrdinaryTransport(t *testing.T) {
	repo := &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: "false"}}
	upstream := &httpUpstreamRecorder{resp: openAIIdentityContractSuccessResponse()}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: &SettingService{settingRepo: repo}, httpUpstream: upstream, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		t.Fatal("disabled bridge intercepted an ordinary request")
		return nil, nil
	}}}
	req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	resp, err := svc.doOpenAIUpstreamPrepared(req, "", enabledCodexCLIExecutionAccount(101, "access", "workspace"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Len(t, upstream.requests, 1)
}

func TestCodexCLIBridgeSwitchPreservesConversationOwnerAndDisablesDirectWS(t *testing.T) {
	ctx := context.Background()
	repo := &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: "false"}}
	settings := &SettingService{settingRepo: repo}
	store := newFakeOpenAISessionRouteStore()
	route, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:7", SessionDigest: "same-session", OwnerAccountID: 101})
	require.NoError(t, err)
	svc := &OpenAIGatewayService{settingService: settings, sessionRouteStore: store}
	account := enabledCodexCLIExecutionAccount(101, "access", "workspace")
	require.True(t, svc.codexUpstreamWSAllowed(ctx, account))
	for _, global := range []string{"true", "false", "true"} {
		repo.values[SettingKeyCodexCLIBridgeEnabled] = global
		require.Equal(t, global != "true", svc.codexUpstreamWSAllowed(ctx, account))
		current, err := store.Get(ctx, "group:7", "same-session")
		require.NoError(t, err)
		require.Equal(t, route.OwnerAccountID, current.OwnerAccountID)
		require.Equal(t, route.Generation, current.Generation)
		require.Empty(t, current.LeaseID)
	}
	// An eligible account's setting outage must not authorize a bypass via WS.
	svc.settingService = &SettingService{settingRepo: &codexCLIBridgeSettingRepo{readErr: errors.New("private-password")}}
	require.False(t, svc.codexUpstreamWSAllowed(ctx, account))
}

func TestCodexCLITaskGlobalBridgeGatePrecedesCredentialsFilesAndProcess(t *testing.T) {
	for _, test := range []struct {
		name    string
		setting *SettingService
		want    string
	}{
		{"disabled", &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}, "cli_bridge_disabled"},
		{"unavailable", &SettingService{settingRepo: &codexCLIBridgeSettingRepo{readErr: errors.New("private-password")}}, "cli_bridge_setting_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := &countTokensOwnerCache{}
			svc := &OpenAIGatewayService{settingService: test.setting, openAITokenProvider: NewOpenAITokenProvider(nil, cache, nil)}
			c, recorder := newCodexCLIExecutionContext()
			runtime := codexCLIExecutionRuntime{available: true, run: func(context.Context, codexcli.TaskRequest) (codexcli.TaskResult, error) {
				t.Fatal("global bridge gate bypassed")
				return codexcli.TaskResult{}, nil
			}}
			result, err := svc.forwardCodexCLITaskWithRuntime(context.Background(), c, enabledCodexCLIExecutionAccount(101, "access", "workspace"), []byte(`{"model":"gpt-6-astra","input":"task"}`), runtime)
			require.Nil(t, result)
			require.EqualError(t, err, test.want)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "private")
			require.Zero(t, cache.reads)
			require.Zero(t, cache.writes)
		})
	}
}
