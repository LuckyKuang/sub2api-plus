//go:build unit || !integration

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// Capture the final send path, including account probes, after hostile inbound
// declarations and generic overrides have been replaced by the owning snapshot.
func TestDomesticOAuthAndAPIKeyOutboundWireMatrix(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, probe := range []bool{false, true} {
				t.Run(platform+"/"+kind+map[bool]string{true: "/probe", false: "/forward"}[probe], func(t *testing.T) {
					account := &Account{ID: 14, Platform: platform, Type: kind, Credentials: map[string]any{"api_key": "test-key", "oauth_provider": "bigmodel"}}
					if kind == AccountTypeOAuth && platform != PlatformZhipu {
						account.Credentials = cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: "cn"}, &cnoauth.Grant{AccessToken: "grant", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
					}
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}}
					oauth := NewCNOAuthService(nil, nil, nil)
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, account.GetAnthropicProtocolBaseURL()+"/v1/messages", nil)
					require.NoError(t, err)
					for _, key := range []string{"User-Agent", "X-Msh-Version", "X-ZCode-Agent", "X-Stainless-Package-Version", "X-Client-Version", "X-Device-Mid"} {
						req.Header.Set(key, "untrusted")
					}
					if probe {
						svc := &AccountTestService{httpUpstream: upstream, cnOAuthService: oauth}
						_, err = svc.doOpenAIAccountTestUpstream(req, "", account, false)
					} else {
						svc := &OpenAIGatewayService{httpUpstream: upstream, cnOAuthService: oauth}
						_, err = svc.doOpenAIUpstream(req, "", account)
					}
					require.NoError(t, err)
					expected := builtInOutboundIdentity(nativeAccountOutboundPreset(platform, kind)).ForProtocol("anthropic")
					require.Equal(t, expected.UserAgent, upstream.lastReq.UserAgent())
					for name, value := range expected.Headers {
						require.Equal(t, value, upstream.lastReq.Header.Get(name), name)
					}
					require.Empty(t, upstream.lastReq.Header.Get("X-Client-Version"))
					require.Empty(t, upstream.lastReq.Header.Get("X-Device-Mid"))
					if platform == PlatformMiniMax {
						require.Equal(t, "main", upstream.lastReq.Header.Get("X-Mavis-Agent-Id"))
						require.NotEmpty(t, upstream.lastReq.Header.Get("X-Mavis-Session-Id"))
					}
				})
			}
		}
	}
}

func TestMiniMaxIdentitySDKPinAndSessionRetryFailover(t *testing.T) {
	_, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: minimax.APIKeyPreset, Version: "0.99.0"})
	require.Error(t, err)
	_, err = buildOutboundIdentity(OutboundIdentitySelection{Preset: minimax.APIKeyPreset, UserAgent: "Anthropic/JS 0.91.1 injected"})
	require.Error(t, err)
	_, err = buildOutboundIdentity(OutboundIdentitySelection{Preset: minimax.APIKeyPreset, UserAgent: "Anthropic/JS 0.91.1 injected", Version: "0.91.1"})
	require.Error(t, err, "a version override must not sanitize an invalid candidate")
	config := emptyOutboundIdentitySettings()
	config.Defaults["minimax:apikey"] = "codex"
	_, ctx := outboundIdentityTestSettings(t, config)
	account := &Account{ID: 14, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: minimax.APIKeyPreset}}}
	ctx = WithOutboundIdentityScope(ctx, nil)
	var first string
	for n := 0; n < 3; n++ {
		if n == 2 {
			copy := *account
			copy.ID = 15
			account = &copy
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.minimaxi.com/anthropic/v1/messages", nil)
		require.NoError(t, err)
		prepareAccountOutboundRequest(req, account)
		identity, ok := outboundidentity.FromContext(req.Context())
		require.True(t, ok)
		require.Equal(t, "account", identity.Source)
		require.Equal(t, "Anthropic/JS 0.91.1", req.UserAgent())
		session := req.Header.Get("X-Mavis-Session-Id")
		switch n {
		case 0:
			first = session
		case 1:
			require.Equal(t, first, session)
		default:
			require.NotEqual(t, first, session)
		}
	}
}

func TestDomesticControlPlaneSettingsPreview(t *testing.T) {
	svc, _ := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	view := svc.GetOutboundIdentityView(context.Background())
	require.Len(t, view.ControlPlane, 3)
	for _, identity := range view.ControlPlane {
		switch identity.Preset {
		case "deepseek":
			require.Equal(t, identity.Version, identity.Headers["X-Client-Version"])
		case "minimax":
			require.Len(t, identity.Headers, 1)
		case "zcode":
			require.NotContains(t, identity.Headers, "X-ZCode-Agent")
			require.NotEmpty(t, identity.Headers["X-Os-Version"])
		default:
			t.Fatal(identity.Preset)
		}
	}
}

func TestDomesticIdentityRejectsUnofficialProductAndSDKFingerprint(t *testing.T) {
	for _, preset := range []string{"deepseek", "kimi", "zcode", "minimax_apikey"} {
		identity := builtInOutboundIdentity(preset)
		_, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: preset, UserAgent: identity.UserAgent + " unofficial/9.9.9"})
		require.Error(t, err, preset)
	}
	config := emptyOutboundIdentitySettings()
	config.Defaults["minimax:apikey"] = "codex"
	_, ctx := outboundIdentityTestSettings(t, config)
	account := &Account{ID: 5, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax_apikey", Version: "0.99.0"}}}
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "codex", identity.Preset, "invalid SDK candidate falls through atomically to the configured mapping")
	require.NotContains(t, identity.Headers, "X-Stainless-Package-Version")
}
