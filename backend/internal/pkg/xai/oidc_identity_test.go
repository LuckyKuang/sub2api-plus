//go:build unit

package xai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type oidcMetadataIdentityClient func(*http.Request) (*http.Response, error)

func (c oidcMetadataIdentityClient) Do(req *http.Request) (*http.Response, error) { return c(req) }

// The native mode declaration is host-scoped, while the login's selected
// User-Agent, identifier and version survive all discovery and JWKS sends.
func TestGrokOIDCMetadataRendersFrozenIdentityForDestination(t *testing.T) {
	identity := outboundidentity.Identity{Preset: "grok", Source: "account", AccountID: 42,
		UserAgent: "grok-shell/1.0.45 (linux; x86_64)", Originator: "grok-shell", Version: "1.0.45",
		Headers: map[string]string{"x-grok-client-identifier": "grok-shell", "x-grok-client-version": "1.0.45", "x-grok-client-mode": "headless"}}
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	for _, tc := range []struct{ destination, mode string }{
		{"https://auth.x.ai/.well-known/openid-configuration", "headless"},
		{"https://accounts.x.ai/jwks", "headless"},
		{"https://api.x.ai/jwks", ""},
		{"http://127.0.0.1:8080/jwks", ""},
		{"https://auth.x.ai.attacker.invalid/jwks", ""},
	} {
		t.Run(tc.destination, func(t *testing.T) {
			client := oidcMetadataIdentityClient(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, identity.UserAgent, req.UserAgent())
				require.Equal(t, "grok-shell", req.Header.Get("x-grok-client-identifier"))
				require.Equal(t, "1.0.45", req.Header.Get("x-grok-client-version"))
				require.Equal(t, tc.mode, req.Header.Get("x-grok-client-mode"))
				require.Empty(t, req.Header.Get("Authorization"))
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
			})
			var result map[string]any
			require.NoError(t, oidcFetchJSON(ctx, client, tc.destination, &result))
			require.Equal(t, true, result["ok"])
			frozen, ok := outboundidentity.FromContext(ctx)
			require.True(t, ok)
			require.Equal(t, "headless", frozen.Headers["x-grok-client-mode"], "rendering cannot mutate the captured login identity")
			require.Equal(t, identity.Source, frozen.Source)
			require.Equal(t, identity.AccountID, frozen.AccountID)
		})
	}
}
