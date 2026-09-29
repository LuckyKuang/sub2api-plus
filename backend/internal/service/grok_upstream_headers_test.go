//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

func TestApplyDefaultGrokUpstreamHeadersUsesCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "claude-code/1.2.3")
	req.Header.Set("x-grok-client-version", "none")

	applyDefaultGrokUpstreamHeaders(req)

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
	require.Equal(t, xai.CLIClientMode, req.Header.Get("x-grok-client-mode"))
}

func TestApplyDefaultGrokUpstreamHeadersHonorsCLIVersionOverride(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "1.0.42")

	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "codex_cli_rs/0.144.0")

	applyDefaultGrokUpstreamHeaders(req)

	require.Equal(t, "1.0.42", req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIUserAgent("1.0.42"), req.Header.Get("User-Agent"))
	require.Equal(t, "grok-shell", req.Header.Get("x-grok-client-identifier"))
}

func TestApplyDefaultGrokUpstreamHeadersPreservesSelectedSnapshot(t *testing.T) {
	identity := outboundidentity.Identity{
		Preset: "grok", UserAgent: "grok-shell/3.9.1 (linux; x86_64)", Originator: "grok-shell", Version: "3.9.1",
		Headers: map[string]string{
			"User-Agent":               "grok-shell/3.9.1 (linux; x86_64)",
			"x-grok-client-identifier": "grok-shell",
			"x-grok-client-version":    "3.9.1",
			"x-grok-client-mode":       "headless",
		},
	}
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "untrusted-inbound/1.0")
	req.Header.Set("Originator", "untrusted")
	req.Header.Set("Version", "0.0.0")

	applyDefaultGrokUpstreamHeaders(req)

	require.Equal(t, identity.UserAgent, req.UserAgent())
	require.Equal(t, identity.Version, req.Header.Get("x-grok-client-version"))
	require.Equal(t, identity.Originator, req.Header.Get("x-grok-client-identifier"))
	require.Equal(t, xai.CLIClientMode, req.Header.Get("x-grok-client-mode"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
}

func TestResolveGrokUpstreamUserAgentNeverPassthrough(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.0.0 (Mac OS; arm64)")

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), resolveGrokUpstreamUserAgent(c))
	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), resolveGrokUpstreamUserAgent(nil))
}

func TestApplyGrokRuntimeHeadersKeepsCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "claude-code/9.9.9")
	req.Header.Set("Version", "9.9.9")
	req.Header.Set("X-App", "foreign")

	applyGrokRuntimeHeaders(req, openAITLSFingerprintRuntime{
		UpstreamUserAgent:  "codex_cli_rs/0.144.0",
		UpstreamOriginator: "codex_cli_rs",
	})

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
	require.Empty(t, req.Header.Get("X-App"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIClientMode, req.Header.Get("x-grok-client-mode"))
}

func TestApplyGrokTLSProfileHeadersAlwaysUsesCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodPost, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "grok-native/1.0")

	// HEAD Profile is TLS-only; Originator/UserAgent HTTP fields are not present.
	applyGrokTLSProfileHeaders(req, &tlsfingerprint.Profile{Name: "chrome"})

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
}
