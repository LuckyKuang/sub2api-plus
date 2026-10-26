//go:build unit

package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGrokOAuthClientExchangeAndRefreshUseFormFields(t *testing.T) {
	identity := outboundidentity.Identity{
		Preset: "grok", UserAgent: xai.CLIUserAgent(xai.CLIClientVersion), Originator: xai.CLIClientIdentifier, Version: xai.CLIClientVersion,
		Headers: map[string]string{
			"User-Agent":               xai.CLIUserAgent(xai.CLIClientVersion),
			"x-grok-client-identifier": xai.CLIClientIdentifier,
			"x-grok-client-version":    xai.CLIClientVersion,
			"x-grok-client-mode":       xai.CLIClientMode,
		},
	}
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, identity.UserAgent, r.Header.Get("User-Agent"))
		require.Equal(t, identity.Originator, r.Header.Get("x-grok-client-identifier"))
		require.Equal(t, identity.Version, r.Header.Get("x-grok-client-version"))
		require.Empty(t, r.Header.Get("x-grok-client-mode"), "an explicitly overridden loopback token endpoint is not an official CLI control destination")
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client-id", r.Form.Get("client_id"))

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			require.Equal(t, "auth-code", r.Form.Get("code"))
			require.Equal(t, "http://127.0.0.1:56121/callback", r.Form.Get("redirect_uri"))
			require.Equal(t, "verifier", r.Form.Get("code_verifier"))
			require.Empty(t, r.Form.Get("code_challenge"))
			require.Empty(t, r.Form.Get("code_challenge_method"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "exchange-access",
				"refresh_token": "exchange-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "openid api:access",
			})
		case "refresh_token":
			require.Equal(t, "refresh-token", r.Form.Get("refresh_token"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refresh-access",
				"refresh_token": "refresh-rotated",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
		default:
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	// Tests inject a loopback token endpoint; allowlist requires unsafe override.
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	t.Setenv(xai.EnvTokenURL, server.URL)

	client := NewGrokOAuthClient()
	exchanged, err := client.ExchangeCode(ctx, "auth-code", "verifier", "http://127.0.0.1:56121/callback", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "exchange-access", exchanged.AccessToken)
	require.Equal(t, "exchange-refresh", exchanged.RefreshToken)
	require.Equal(t, int64(3600), exchanged.ExpiresIn)
	require.Equal(t, "openid api:access", exchanged.Scope)

	refreshed, err := client.RefreshToken(ctx, "refresh-token", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "refresh-access", refreshed.AccessToken)
	require.Equal(t, "refresh-rotated", refreshed.RefreshToken)
	require.Equal(t, int64(7200), refreshed.ExpiresIn)
}

func TestGrokOAuthControlIdentitySnapshotSurvivesSameOriginRedirect(t *testing.T) {
	var resolutions atomic.Int32
	ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity {
		version := "1.0.45"
		if resolutions.Add(1) > 1 {
			version = "9.9.9"
		}
		return outboundidentity.Identity{
			Preset: "grok", UserAgent: "grok-shell/" + version + " (linux; x86_64)", Originator: "grok-shell", Version: version, Source: "global",
			Headers: map[string]string{"x-grok-client-identifier": "grok-shell", "x-grok-client-version": version, "x-grok-client-mode": "headless"},
		}
	})
	assertIdentity := func(r *http.Request) {
		require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", r.Header.Get("User-Agent"))
		require.Equal(t, "grok-shell", r.Header.Get("X-Grok-Client-Identifier"))
		require.Equal(t, "1.0.45", r.Header.Get("X-Grok-Client-Version"))
		require.Empty(t, r.Header.Get("X-Grok-Client-Mode"), "the loopback token origin is not an official control host")
	}
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assertIdentity(r)
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "fixture-refresh", r.Form.Get("refresh_token"))
		if r.URL.Path == "/token" {
			http.Redirect(w, r, "/token/continued", http.StatusTemporaryRedirect)
			return
		}
		require.Equal(t, "/token/continued", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "redirected-access", "refresh_token": "rotated-refresh"})
	}))
	defer origin.Close()
	client := &grokOAuthClient{tokenURL: origin.URL + "/token"}
	result, err := client.RefreshToken(ctx, "fixture-refresh", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "redirected-access", result.AccessToken)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, resolutions.Load(), "all control attempts must render the first selected snapshot")
}

func TestGrokOAuthTokenRedirectCannotTrustAnotherOriginOrLeakGrant(t *testing.T) {
	// This unsigned Team claim would bypass ID-token validation if a response
	// from the redirect target were incorrectly treated as the trusted exchange.
	forgedTeamToken := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"principal_type":"Team","principal_id":"attacker-team"}`)) + ".untrusted-signature"
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, grant := range []string{"authorization_code", "refresh_token"} {
			t.Run(strconv.Itoa(status)+"/"+grant, func(t *testing.T) {
				var targetCalls atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					targetCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": forgedTeamToken, "token_type": "Bearer"})
				}))
				defer target.Close()
				var originCalls atomic.Int32
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					originCalls.Add(1)
					require.NoError(t, r.ParseForm())
					require.Equal(t, grant, r.Form.Get("grant_type"))
					if grant == "authorization_code" {
						require.Equal(t, "private-code", r.Form.Get("code"))
						require.Equal(t, "private-verifier", r.Form.Get("code_verifier"))
					} else {
						require.Equal(t, "private-refresh", r.Form.Get("refresh_token"))
					}
					http.Redirect(w, r, target.URL+"/forged-team", status)
				}))
				defer origin.Close()
				client := &grokOAuthClient{tokenURL: origin.URL + "/token"}
				var tokens *xai.TokenResponse
				var err error
				if grant == "authorization_code" {
					tokens, err = client.ExchangeCode(context.Background(), "private-code", "private-verifier", "http://127.0.0.1/callback", "", "client-id")
				} else {
					tokens, err = client.RefreshToken(context.Background(), "private-refresh", "", "client-id")
				}
				require.Error(t, err)
				require.Nil(t, tokens, "another origin's Team token cannot become a trusted exchange result")
				require.EqualValues(t, 1, originCalls.Load())
				require.Zero(t, targetCalls.Load(), "no request, including the grant body, may reach the redirect target")
			})
		}
	}
}

func TestGrokOAuthTokenSameOriginRedirectLimit(t *testing.T) {
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/token/again", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client := &grokOAuthClient{tokenURL: origin.URL + "/token"}
	tokens, err := client.RefreshToken(context.Background(), "private-refresh", "", "client-id")
	require.Error(t, err)
	require.Nil(t, tokens)
	require.EqualValues(t, 10, calls.Load(), "the same-origin policy must retain the standard redirect limit")
}

func TestGrokOAuthTokenRedirectRejectsHTTPSDowngrade(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client := origin.Client()
	client.CheckRedirect = grokOAuthSameOriginRedirectPolicy
	response, err := client.PostForm(origin.URL, url.Values{"refresh_token": {"private-refresh"}})
	if response != nil {
		_ = response.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, targetCalls.Load(), "HTTPS grants must not be replayed over HTTP")
}

func TestGrokOAuthTokenRedirectOriginIncludesSchemeHostAndPort(t *testing.T) {
	origin, err := http.NewRequest(http.MethodPost, "https://auth.x.ai/token", nil)
	require.NoError(t, err)
	for _, tc := range []struct {
		destination string
		allowed     bool
	}{
		{"https://auth.x.ai/another-path", true},
		{"https://AUTH.X.AI:443/another-path", true},
		{"https://auth.x.ai:444/token", false},
		{"https://other.x.ai/token", false},
		{"http://auth.x.ai:443/token", false},
		{"https://user@auth.x.ai/token", false},
	} {
		t.Run(tc.destination, func(t *testing.T) {
			destination, err := http.NewRequest(http.MethodPost, tc.destination, nil)
			require.NoError(t, err)
			err = grokOAuthSameOriginRedirectPolicy(destination, []*http.Request{origin})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGrokOAuthClientRefreshForbiddenClassifiesOnlyExplicitEntitlement(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantReason string
	}{
		{name: "explicit entitlement", body: `{"error":"access_denied"}`, wantReason: "GROK_OAUTH_ENTITLEMENT_DENIED"},
		{name: "generic forbidden", body: `{"error":"forbidden"}`, wantReason: "GROK_OAUTH_TOKEN_REFRESH_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
			t.Setenv(xai.EnvTokenURL, server.URL)

			client := NewGrokOAuthClient()
			_, err := client.RefreshToken(context.Background(), "refresh-token", "", "client-id")
			require.Error(t, err)
			require.Contains(t, strings.ToUpper(err.Error()), tt.wantReason)
		})
	}
}

func TestGrokOAuthClientStatusErrorRedactsSensitiveResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"access-secret","refresh_token":"refresh-secret","code_verifier":"verifier-secret"}`))
	}))
	defer server.Close()
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	t.Setenv(xai.EnvTokenURL, server.URL)

	client := NewGrokOAuthClient()
	_, err := client.RefreshToken(context.Background(), "refresh-secret", "", "client-id")
	require.Error(t, err)

	errText := err.Error()
	require.Contains(t, errText, "status 400")
	require.Contains(t, errText, `\"refresh_token\":\"***\"`)
	require.NotContains(t, errText, "access-secret")
	require.NotContains(t, errText, "refresh-secret")
	require.NotContains(t, errText, "verifier-secret")
}

func TestGrokOAuthEntitlementDenialRequiresExplicitEvidence(t *testing.T) {
	t.Parallel()

	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"error":"access_denied"}`))
	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"code":"entitlement_denied"}`))
	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"message":"no active Grok subscription"}`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`{"error":"forbidden","message":"request forbidden"}`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`<html>403 Forbidden</html>`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`{"error":"access_denied","message":"You have run out of credits"}`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`{"code":"subscription_required","message":"included free usage exhausted"}`))
}

func TestNewGrokOAuthClient_UnvalidatedTokenURLFallsBackToDefault(t *testing.T) {
	// Without unsafe overrides, a random env TokenURL must not be used (fail-closed).
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "")
	t.Setenv(xai.EnvTokenURL, "https://evil.example/oauth/token")

	client := NewGrokOAuthClient().(*grokOAuthClient)
	require.Equal(t, xai.DefaultTokenURL, client.tokenURL)
}
