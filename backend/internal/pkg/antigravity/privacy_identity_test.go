package antigravity

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type privacyIdentityTransport func(*http.Request) (*http.Response, error)

func (f privacyIdentityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPrivacyIdentityFallbackOnBothEndpoints(t *testing.T) {
	for _, env := range []string{"", "invalid", "1.20.8"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv(AntigravityUserAgentVersionEnv, env)
			ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity {
				return outboundidentity.Identity{}
			})
			expected := DefaultIdentity()
			if env == "1.20.8" {
				require.Equal(t, "environment", expected.Source)
				require.Equal(t, "antigravity/1.20.8 windows/amd64", expected.UserAgent)
			} else {
				require.Equal(t, "compiled_default", expected.Source)
				require.Equal(t, "antigravity/2.9.1 windows/amd64", expected.UserAgent)
			}
			var paths []string
			client := &Client{httpClient: &http.Client{Transport: privacyIdentityTransport(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				require.Equal(t, expected.UserAgent, req.UserAgent())
				require.Equal(t, "gl-node/22.21.1", req.Header.Get("X-Goog-Api-Client"))
				require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
				identity, ok := outboundidentity.FromContext(req.Context())
				require.True(t, ok)
				require.Equal(t, expected.Source, identity.Source)
				before := req.Header.Clone()
				outboundidentity.ApplyContext(req)
				require.Equal(t, before, req.Header)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
			})}}
			_, err := client.SetUserSettings(ctx, "test-token")
			require.NoError(t, err)
			_, err = client.FetchUserInfo(ctx, "test-token", "test-project")
			require.NoError(t, err)
			require.Equal(t, []string{"/v1internal:setUserSettings", "/v1internal:fetchUserInfo"}, paths)
		})
	}
}

func TestPrivacyIdentityIsEndpointLocalAndFiltersOverrides(t *testing.T) {
	identity := DefaultIdentity()
	identity.AccountID, identity.Source = 72, "account"
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, privacyBaseURL+"/v1internal:setUserSettings", nil)
	require.NoError(t, err)
	req.Header = http.Header{
		"user-agent": {"untrusted"}, "User-Agent": {"untrusted"},
		"x-goog-api-client": {"untrusted"}, "X-Goog-Api-Client": {"untrusted"},
		"X-GOOG-API-CLIENT": {"untrusted"}, "Originator": {"foreign"}, "Version": {"999"},
		"X-Stainless-Os": {"foreign"}, "Authorization": {"Bearer test-token"},
	}
	applyPrivacyIdentity(req)
	want := http.Header{"User-Agent": {identity.UserAgent}, "X-Goog-Api-Client": {"gl-node/22.21.1"}, "Authorization": {"Bearer test-token"}}
	require.Equal(t, want, req.Header)
	outboundidentity.ApplyContext(req)
	require.Equal(t, want, req.Header)
	snapshot, ok := outboundidentity.FromContext(req.Context())
	require.True(t, ok)
	require.Equal(t, identity.AccountID, snapshot.AccountID)
	require.Equal(t, identity.Source, snapshot.Source)
	require.Equal(t, identity.Version, snapshot.Version)
	parent, _ := outboundidentity.FromContext(ctx)
	require.Equal(t, identity, parent)
	inference, err := NewAPIRequest(ctx, "generateContent", "test-token", []byte(`{}`))
	require.NoError(t, err)
	require.Empty(t, inference.Header.Get("X-Goog-Api-Client"))

	// A compatible selected family still owns its declarations. Privacy's SDK
	// pin cannot be attached to an unrelated identity by the protocol adapter.
	foreign := outboundidentity.Identity{Preset: "grok", UserAgent: "xai-grok-workspace/1.2.3", Headers: map[string]string{"X-Grok-Client-Version": "1.2.3"}}
	req = req.WithContext(outboundidentity.WithIdentity(ctx, foreign))
	applyPrivacyIdentity(req)
	require.Equal(t, foreign.UserAgent, req.UserAgent())
	require.Empty(t, req.Header.Get("X-Goog-Api-Client"))
	require.Equal(t, "1.2.3", req.Header.Get("X-Grok-Client-Version"))
}
