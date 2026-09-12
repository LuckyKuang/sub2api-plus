package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamTrustedIdentitySurvivesGrokHostAndFallback(t *testing.T) {
	for _, withTLS := range []bool{false, true} {
		for _, preset := range []string{"", "codex", "grok", "claude"} {
			t.Run(fmt.Sprintf("tls=%t/preset=%s", withTLS, preset), func(t *testing.T) {
				svc, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
				require.True(t, ok)
				const accountID int64 = 4084
				profile := service.HTTPUpstreamProfileOpenAI
				isolation, proxyKey := svc.getIsolationMode(), directProxyKey
				mode := svc.resolveProtocolMode(profile, proxyKey, nil)
				settings := svc.applyProfilePoolSettings(svc.resolvePoolSettings(isolation, 1), profile)
				cacheKey, poolKey := buildCacheKey(isolation, proxyKey, accountID, mode), buildPoolKey(settings, mode)
				if withTLS {
					mode = upstreamProtocolModeDefault
					cacheKey = "tls:" + buildCacheKey(isolation, proxyKey, accountID, mode)
					poolKey = buildPoolKey(settings, mode) + ":tls"
				}
				var captured []*http.Request
				svc.clients[cacheKey] = &upstreamClientEntry{
					client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						captured = append(captured, req.Clone(req.Context()))
						status, body := http.StatusOK, `{"id":"response-ok"}`
						if len(captured) == 1 {
							status, body = http.StatusForbidden, `{"error":"Access denied"}`
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
					})},
					proxyKey: proxyKey, poolKey: poolKey, protocolMode: mode,
				}
				account := &service.Account{ID: accountID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{
					"base_url": "https://cli-chat-proxy.grok.com/v1", "api_key": "test-key",
				}}
				if preset != "" {
					account.Credentials["outbound_identity"] = map[string]any{"preset": preset}
				}
				ctx := service.WithAccountOutboundIdentity(context.Background(), account)
				ctx = service.WithHTTPUpstreamProfile(ctx, profile)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", strings.NewReader(`{"input":"hello"}`))
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer test-key")
				// Native Codex is finalized by its service; its empty generic context
				// must not allow a transport destination to choose another client.
				req.Header.Set("User-Agent", "codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) terminal")
				service.ApplyAccountOutboundIdentity(ctx, account, req)
				want := identityHeadersForTransportTest(req.Header)
				var resp *http.Response
				if withTLS {
					resp, err = svc.DoWithTLS(req, "", accountID, 1, &tlsfingerprint.Profile{Name: "test"})
				} else {
					resp, err = svc.Do(req, "", accountID, 1)
				}
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.NoError(t, resp.Body.Close())
				require.Len(t, captured, 2)
				require.Equal(t, grokCLIProxyHost, captured[0].URL.Hostname())
				require.Equal(t, grokOfficialAPIHost, captured[1].URL.Hostname())
				require.Equal(t, "xai-grok-cli", captured[0].Header.Get("X-XAI-Token-Auth"))
				require.Empty(t, captured[1].Header.Get("X-XAI-Token-Auth"))
				for _, sent := range captured {
					require.Equal(t, want, identityHeadersForTransportTest(sent.Header))
					require.Equal(t, "Bearer test-key", sent.Header.Get("Authorization"))
				}
			})
		}
	}
}

func identityHeadersForTransportTest(headers http.Header) http.Header {
	identity := http.Header{}
	for name, values := range headers {
		if outboundidentity.IsIdentityHeader(name) {
			identity[name] = append([]string(nil), values...)
		}
	}
	return identity
}

func TestClaudeOAuthRefreshUsesSelectedIdentity(t *testing.T) {
	identity := outboundidentity.Identity{Preset: "claude", UserAgent: "claude-cli/2.9.1 (external, cli)", Originator: "claude-cli", Version: "2.9.1", Headers: map[string]string{"X-App": "cli", "X-Stainless-OS": "Linux"}}
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	var captured http.Header
	rt := newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed","expires_in":3600}`))
	}), nil)
	client, ok := NewClaudeOAuthClient().(*claudeOAuthService)
	require.True(t, ok)
	client.tokenURL = "https://in-process/oauth/token"
	client.clientFactory = func(string) (*req.Client, error) { return newTestReqClient(rt), nil }
	token, err := client.RefreshToken(ctx, "refresh", "")
	require.NoError(t, err)
	require.Equal(t, "refreshed", token.AccessToken)
	require.Equal(t, identity.UserAgent, captured.Get("User-Agent"))
	require.Equal(t, "cli", captured.Get("X-App"))
	require.Equal(t, "Linux", captured.Get("X-Stainless-OS"))
}

func TestGrokFallbackRetainsTrustedIdentityWithoutProxyAuthenticationHints(t *testing.T) {
	i := outboundidentity.Identity{Preset: "grok", UserAgent: "xai-grok-workspace/3.9.1", Originator: "grok-shell", Version: "3.9.1", Headers: map[string]string{"x-grok-client-version": "3.9.1", "x-grok-client-identifier": "grok-shell"}}
	req, err := http.NewRequestWithContext(outboundidentity.WithIdentity(context.Background(), i), http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	i.Apply(req.Header)
	fallback, err := newGrokOfficialAPIFallbackRequest(req)
	require.NoError(t, err)
	defer func() { _ = fallback.Body.Close() }()
	require.Equal(t, "api.x.ai", fallback.URL.Host)
	require.Equal(t, "Bearer test-token", fallback.Header.Get("Authorization"))
	require.Empty(t, fallback.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, i.UserAgent, fallback.Header.Get("User-Agent"))
	require.Equal(t, i.Version, fallback.Header.Get("X-Grok-Client-Version"))
}
