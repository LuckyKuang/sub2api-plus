//go:build unit

package zcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestRuntimeHeadersUseFixedUbuntuDefaults(t *testing.T) {
	t.Setenv("LANG", "fr_FR.UTF-8")
	t.Setenv("TZ", "Europe/Paris")
	require.Equal(t, map[string]string{
		"X-Platform": "linux-x64", "X-Os-Category": "linux", "X-Os-Version": "6.8.0-31-generic",
		"X-Client-Language": "en-US", "X-Client-Timezone": "UTC",
	}, RuntimeHeaders())
	first := RuntimeHeaders()
	first["X-Platform"] = "mutated"
	require.NotEqual(t, first, RuntimeHeaders())
	require.Equal(t, "#31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024", controlRuntimeHeaders()["X-Os-Version"])
}

func TestAuxiliaryClientsSendIdentityWithoutTransportWiring(t *testing.T) {
	for _, configured := range []bool{false, true} {
		expected := DefaultIdentity()
		ctx := context.Background()
		if configured {
			expected.UserAgent, expected.Version, expected.Source = "ZCode/4.1.0", "4.1.0", "account"
			expected.Headers["User-Agent"] = expected.UserAgent
			expected.Headers[HeaderAppVersion] = expected.Version
			expected.Headers["X-Client-Timezone"] = "Asia/Shanghai"
			ctx = outboundidentity.WithIdentity(ctx, expected)
		}
		seen := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen++
			require.Empty(t, r.Header.Get("X-ZCode-Agent"))
			require.Empty(t, r.Header.Get("X-Device-Mid"))
			for name, value := range ControlIdentity(expected).Headers {
				if r.Header.Get(name) != value {
					t.Errorf("%s: %s = %q, want %q", r.URL.Path, name, r.Header.Get(name), value)
				}
			}
			if r.Header.Get("Originator") != "" || r.Header.Get("Version") != "" {
				t.Error("foreign identity headers")
			}
			// Stop after the request; this test exercises the real wire on error paths too.
			w.WriteHeader(http.StatusUnauthorized)
		}))
		client := server.Client()
		_, err := NewHandshakeClient(client, server.URL).Init(ctx, ProviderBigModel, "test-poll")
		require.Error(t, err)
		_, err = NewCredentialClient(client, server.URL, server.URL).ResolveIndividualCodingPlanKey(ctx, ProviderBigModel, "test-token")
		require.Error(t, err)
		_, err = NewOffPeakClient(client, server.URL).Availability(ctx, OffPeakAuth{JWT: "test-token"})
		require.Error(t, err)
		require.Equal(t, 3, seen)
		server.Close()
	}
}

func TestCredentialIdentityIsCapturedOnceAcrossBusinessCalls(t *testing.T) {
	resolutions := 0
	ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity {
		resolutions++
		got := DefaultIdentity()
		if resolutions > 1 {
			got.UserAgent = "ZCode/99.0.0"
		}
		return got
	})
	seen := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if r.UserAgent() != DefaultIdentity().UserAgent {
			t.Errorf("identity changed: %q", r.UserAgent())
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/getCustomerInfo"):
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{"organizationId": "org-1", "organizationName": "默认机构", "projects": []any{map[string]any{"projectId": "proj-1", "projectName": "默认项目"}}}}})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			writeEnvelope(t, w, []any{map[string]any{"apiKey": "ak-1", "name": PersonalAPIKeyName}})
		case strings.HasSuffix(r.URL.Path, "/copy/ak-1"):
			writeEnvelope(t, w, map[string]any{"secretKey": "sk-1"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	got, err := NewCredentialClient(server.Client(), server.URL, server.URL).ResolveIndividualCodingPlanKey(ctx, ProviderBigModel, "test-token")
	require.NoError(t, err)
	require.Equal(t, "ak-1.sk-1", got.APIKey)
	require.Equal(t, 3, seen)
	require.Equal(t, 1, resolutions)
}

func TestZCodeSDKWireProfilesMatchOfficialRunnerHeaderLayering(t *testing.T) {
	identity := DefaultIdentity()
	for _, tc := range []struct{ protocol, suffix string }{
		{"anthropic", "ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"},
		{"chat_completions", "ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22"},
		{"responses", "ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"},
	} {
		projected := identity.ForProtocol(tc.protocol)
		require.Equal(t, "ZCode/3.14.3 "+tc.suffix, projected.UserAgent)
		require.Equal(t, "3.14.3", projected.Headers[HeaderAppVersion])
		require.Equal(t, projected.UserAgent, projected.ForProtocol(tc.protocol).UserAgent)
	}
	require.Equal(t, "ZCode/3.14.3", identity.UserAgent)
}
