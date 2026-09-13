package outboundidentity

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentitySnapshotRemovesUntrustedDeclarationsAndPreservesProtocol(t *testing.T) {
	i := Identity{Preset: "grok", UserAgent: "xai-grok-workspace/1.2.3", Originator: "grok-shell", Version: "1.2.3", Headers: map[string]string{"x-grok-client-identifier": "grok-shell", "x-grok-client-version": "1.2.3"}}
	ctx := WithIdentity(context.Background(), i)
	i.Headers["x-grok-client-version"] = "untrusted"
	copy, _ := FromContext(ctx)
	copy.Headers["x-grok-client-version"] = "also-untrusted"
	req, err := http.NewRequestWithContext(ctx, "POST", "https://example.com/v1/messages", nil)
	require.NoError(t, err)
	req.Header = http.Header{"user-agent": {"caller"}, "User-Agent": {"override"}, "originator": {"caller"}, "Version": {"999"}, "X-Stainless-Os": {"inbound"}, "X-Goog-Api-Client": {"inbound"}, "Authorization": {"Bearer credential"}, "Anthropic-Version": {"2023-06-01"}, "X-Stainless-Retry-Count": {"2"}, "X-Session-Id": {"session"}}
	ApplyContext(req)
	require.Equal(t, "xai-grok-workspace/1.2.3", req.Header.Get("User-Agent"))
	require.Equal(t, "1.2.3", req.Header.Get("X-Grok-Client-Version"))
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
