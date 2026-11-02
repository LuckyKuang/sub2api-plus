//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// Exercise the production builder, policy and dispatcher: the response default
// must not erase a sent priority, and inbound Codex UA is never outbound proof.
func TestOutboundMetadataFinalFastRequestAcrossNativeAndCLIBridge(t *testing.T) {
	for _, bridge := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "bridge"}[bridge], func(t *testing.T) {
			owner := codexBridgeAPIKeyFixture()
			owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = bridge
			status := codexCLIBridgeReadyFixture()
			status.InstalledVersion = "0.165.0"
			sends := 0
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}, settingService: enabledCodexCLIBridgeTestSettings(), cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector()}
			svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				sends++
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"service_tier":"priority"`)
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", req.Header.Get("User-Agent"))
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_fast","model":"gpt-6.1-sol","service_tier":"default","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`))}, nil
			}}
			svc.codexCLIBridgeRuntime = codexCLIBridgeTransportStub{status: status, bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, send func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
				return send(ctx, req)
			}}
			body := []byte(`{"model":"gpt-6.1-sol","input":"hello","service_tier":"fast","stream":false}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request.Header.Set("User-Agent", "codex-tui/0.166.0 forged-inbound")
			result, err := svc.Forward(context.Background(), c, owner, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, sends)
			require.Equal(t, "priority", optionalStringValue(result.ServiceTier))
			require.Equal(t, "default", result.ResponseServiceTier)
			require.NotNil(t, result.Outbound)
			expected := OutboundNative
			if bridge {
				expected = OutboundCodexCLIBridge
			}
			require.Equal(t, expected, result.Outbound.Transport)
			require.Equal(t, "gateway_send", result.Outbound.Evidence)
			require.True(t, result.Outbound.TierKnown)
			require.Equal(t, "priority", result.Outbound.ServiceTier)
			require.NotContains(t, result.Outbound.UserAgent, "forged-inbound")
			if bridge {
				require.Equal(t, "0.165.0", result.Outbound.CLIVersion)
			} else {
				require.Empty(t, result.Outbound.CLIVersion)
			}
			resolution := ApplyOpenAIServiceTierBillingResolution(owner, result)
			require.Equal(t, "default", resolution.Billing, "public API default remains a lower bill")
		})
	}
}

func TestOutboundMetadataCannotClaimAnUnexecutedBridge(t *testing.T) {
	owner := codexBridgeAPIKeyFixture()
	svc := &OpenAIGatewayService{settingService: enabledCodexCLIBridgeTestSettings(), codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		return nil, errors.New("failed before relay")
	}}}
	ctx, observation := beginOutboundObservation(WithOutboundIdentityScope(t.Context(), nil))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://compatible.example/v1/responses", strings.NewReader(`{"service_tier":"priority"}`))
	require.NoError(t, err)
	_, err = svc.doOpenAIUpstream(req, "", owner)
	require.Error(t, err)
	require.Nil(t, observation.snapshot())
}

func TestOutboundMetadataAttemptAndTurnIsolation(t *testing.T) {
	parent := context.Background()
	headers := http.Header{"User-Agent": {"codex_cli_rs/0.165.0"}, "Originator": {"codex_cli_rs"}, "Version": {"0.165.0"}, "Authorization": {"secret"}, "Cookie": {"private"}}
	first, one := beginOutboundObservation(parent)
	second, two := beginOutboundObservation(parent)
	observeOutboundWS(first, headers, []byte(`{"type":"response.create","service_tier":"priority"}`))
	observeOutboundWS(second, headers, []byte(`{"type":"response.create","generate":false,"service_tier":"priority"}`))
	require.Nil(t, two.snapshot(), "prewarm cannot prove a generation")
	observeOutboundWS(second, headers, []byte(`{"type":"response.create"}`))
	require.Equal(t, "priority", one.snapshot().ServiceTier)
	require.True(t, two.snapshot().TierKnown)
	require.Empty(t, two.snapshot().ServiceTier)
	snapshot := one.snapshot()
	snapshot.ServiceTier = "changed"
	require.Equal(t, "priority", one.snapshot().ServiceTier, "results are immutable copies")
}

func TestOutboundMetadataWebSocketSuccessfulGenerationWrites(t *testing.T) {
	// This exercises the actual concrete dialer/write path rather than deriving
	// expected identity from the production resolver or connection settings.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	headers := http.Header{"User-Agent": {"codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) tmux"}, "Originator": {"codex_cli_rs"}, "Version": {"0.164.0"}}
	conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), headers, "")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	headers.Set("User-Agent", "changed-after-handshake")
	first, one := beginOutboundObservation(ctx)
	require.NoError(t, conn.WriteJSON(first, json.RawMessage(`{"type":"response.create","service_tier":"priority"}`)))
	require.Equal(t, "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) tmux", one.snapshot().UserAgent)
	require.Equal(t, "0.164.0", one.snapshot().ClientVersion)
	require.Equal(t, "priority", one.snapshot().ServiceTier)
	next, two := beginOutboundObservation(ctx)
	require.NoError(t, conn.WriteJSON(next, json.RawMessage(`{"type":"response.create","generate":false}`)))
	require.Nil(t, two.snapshot())
	require.NoError(t, conn.WriteJSON(next, json.RawMessage(`{"type":"response.create"}`)))
	require.True(t, two.snapshot().TierKnown)
	require.Empty(t, two.snapshot().ServiceTier)
	require.Equal(t, "priority", one.snapshot().ServiceTier)
}

func TestOutboundMetadataDoesNotClearAdapterOrMediaTier(t *testing.T) {
	for _, test := range []struct{ platform, path, body string }{
		{PlatformAnthropic, "/v1/messages", `{"speed":"fast"}`},
		{PlatformOpenAI, "/v1/messages", `{"speed":"fast"}`},
		{PlatformOpenAI, "/v1/images/generations", `{"model":"gpt-image-2"}`},
	} {
		ctx, observation := beginOutboundObservation(t.Context())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.example"+test.path, strings.NewReader(test.body))
		require.NoError(t, err)
		observeOutboundHTTP(req, &Account{Platform: test.platform})
		require.NotNil(t, observation.snapshot())
		require.False(t, observation.snapshot().TierKnown, "other protocol fields must not be treated as a missing OpenAI service_tier")
	}
}
