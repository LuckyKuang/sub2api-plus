//go:build unit || !integration

package outboundidentity

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentitySnapshotRemovesUntrustedDeclarationsAndPreservesProtocol(t *testing.T) {
	i := Identity{Preset: "grok", UserAgent: "grok-shell/1.2.3 (linux; x86_64)", Originator: "grok-shell", Version: "1.2.3", Headers: map[string]string{"x-grok-client-identifier": "grok-shell", "x-grok-client-version": "1.2.3", "x-grok-client-mode": "headless"}}
	ctx := WithIdentity(context.Background(), i)
	i.Headers["x-grok-client-version"] = "untrusted"
	copy, _ := FromContext(ctx)
	copy.Headers["x-grok-client-version"] = "also-untrusted"
	req, err := http.NewRequestWithContext(ctx, "POST", "https://example.com/v1/messages", nil)
	require.NoError(t, err)
	req.Header = http.Header{"user-agent": {"caller"}, "User-Agent": {"override"}, "originator": {"caller"}, "Version": {"999"}, "X-Stainless-Os": {"inbound"}, "X-Goog-Api-Client": {"inbound"}, "Authorization": {"Bearer credential"}, "Anthropic-Version": {"2023-06-01"}, "X-Stainless-Retry-Count": {"2"}, "X-Session-Id": {"session"}}
	ApplyContext(req)
	require.Equal(t, "grok-shell/1.2.3 (linux; x86_64)", req.Header.Get("User-Agent"))
	require.Equal(t, "1.2.3", req.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "grok-shell", req.Header.Get("X-Grok-Client-Identifier"))
	require.Empty(t, req.Header.Get("X-Grok-Client-Mode"), "a compatible supplier is not a CLI control destination")
	for _, key := range []string{"user-agent", "originator", "Version", "X-Stainless-Os", "X-Goog-Api-Client"} {
		require.NotContains(t, req.Header, key)
	}
	require.Equal(t, "Bearer credential", req.Header.Get("Authorization"))
	require.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"))
	require.Equal(t, "2", req.Header.Get("X-Stainless-Retry-Count"))
	require.Equal(t, "session", req.Header.Get("X-Session-ID"))
	before := req.Header.Clone()
	ApplyContext(req)
	require.Equal(t, before, req.Header, "reapplication before transport is idempotent")
}

func TestProtocolIdentitySnapshotIsDeepCopiedAndIdempotent(t *testing.T) {
	identity := Identity{Preset: "fixture", UserAgent: "client/1.0.0", Headers: map[string]string{"User-Agent": "client/1.0.0"}, ControlHeaders: map[string]string{"X-Os-Version": "host"}, Inference: map[string]WireProfile{"anthropic": {UserAgentSuffix: "ai/6.0.193", Headers: map[string]string{"X-Stainless-Package-Version": "0.95.2"}}}}
	ctx := WithIdentity(context.Background(), identity)
	identity.Inference["anthropic"].Headers["X-Stainless-Package-Version"] = "mutated"
	identity.ControlHeaders["X-Os-Version"] = "mutated"
	snapshot, _ := FromContext(ctx)
	snapshot.Inference["anthropic"].Headers["X-Stainless-Package-Version"] = "also-mutated"
	snapshot.ControlHeaders["X-Os-Version"] = "also-mutated"
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.invalid"+path, nil)
		require.NoError(t, err)
		req.Header.Set("X-Client-Version", "caller")
		req.Header.Set("X-Device-Mid", "caller")
		ApplyContext(req)
		ApplyContext(req)
		require.Equal(t, "client/1.0.0 ai/6.0.193", req.UserAgent())
		require.Equal(t, "0.95.2", req.Header.Get("X-Stainless-Package-Version"))
		require.Empty(t, req.Header.Get("X-Client-Version"))
		require.Empty(t, req.Header.Get("X-Device-Mid"))
	}
	snapshot, _ = FromContext(ctx)
	require.Equal(t, "host", snapshot.ControlHeaders["X-Os-Version"])
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.invalid/oauth/token", nil)
	require.NoError(t, err)
	ApplyContext(req)
	require.Equal(t, "client/1.0.0", req.UserAgent())
	require.Empty(t, req.Header.Get("X-Stainless-Package-Version"))
}

func TestGrokClientModeIsLimitedToNativeProxyAndControlDestinations(t *testing.T) {
	identity := Identity{
		Preset: "grok", UserAgent: "grok-shell/1.0.45 (linux; x86_64)", Originator: "grok-shell", Version: "1.0.45",
		Headers: map[string]string{"User-Agent": "grok-shell/1.0.45 (linux; x86_64)", "x-grok-client-identifier": "grok-shell", "x-grok-client-version": "1.0.45", "x-grok-client-mode": "headless"},
	}
	ctx := WithIdentity(context.Background(), identity)
	for _, tc := range []struct {
		url, mode string
	}{
		{"https://cli-chat-proxy.grok.com/v1/responses", "headless"},
		{"https://CLI-CHAT-PROXY.GROK.COM/v1/billing", "headless"},
		{"https://auth.x.ai/oauth2/token", "headless"},
		{"https://accounts.x.ai/", "headless"},
		{"https://api.x.ai/v1/responses", ""},
		{"https://eu-west-1.api.x.ai/v1/responses", ""},
		{"https://supplier.example/v1/responses", ""},
		{"https://cli-chat-proxy.grok.com.supplier.example/v1/responses", ""},
		{"https://auth.x.ai.supplier.example/oauth2/token", ""},
		{"https://supplier.example/v1/billing", ""},
		// Returning to the proxy must render from the unchanged owner snapshot.
		{"wss://cli-chat-proxy.grok.com/v1/realtime", "headless"},
		{"wss://supplier.example/v1/realtime", ""},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, tc.url, nil)
			require.NoError(t, err)
			req.Header.Set("X-Grok-Client-Mode", "untrusted")
			ApplyContext(req)
			ApplyContext(req)
			require.Equal(t, tc.mode, req.Header.Get("X-Grok-Client-Mode"))
			require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", req.UserAgent())
			require.Equal(t, "grok-shell", req.Header.Get("X-Grok-Client-Identifier"))
			require.Equal(t, "1.0.45", req.Header.Get("X-Grok-Client-Version"))
		})
	}
	snapshot, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "headless", snapshot.Headers["x-grok-client-mode"])
}
