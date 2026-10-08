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
	"github.com/google/uuid"
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
					// Expected values come from the official-client contract below,
					// never from the resolver under test.
					expected := officialDomesticAnthropicHeaders(platform, kind)
					for name, value := range expected {
						require.Equal(t, value, upstream.lastReq.Header.Get(name), name)
					}
					for name := range upstream.lastReq.Header {
						if outboundidentity.IsIdentityHeader(name) && !strings.EqualFold(name, "X-Msh-Device-Id") {
							_, present := expected[http.CanonicalHeaderKey(name)]
							require.True(t, present, "unexpected identity header: %s", name)
						}
					}
					if platform == PlatformKimi {
						_, err := uuid.Parse(upstream.lastReq.Header.Get("X-Msh-Device-Id"))
						require.NoError(t, err, "official Kimi identity requires a persisted UUID")
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
	config.Profiles["minimax_apikey"] = OutboundIdentitySelection{Preset: "minimax_apikey"}
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
	config.Profiles["minimax_apikey"] = OutboundIdentitySelection{Preset: "minimax_apikey"}
	_, ctx := outboundIdentityTestSettings(t, config)
	account := &Account{ID: 5, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax_apikey", Version: "0.99.0"}}}
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "minimax_apikey", identity.Preset, "invalid SDK candidate falls through atomically to the native global identity")
	require.Equal(t, "global", identity.Source)
	require.Equal(t, "0.91.1", identity.Headers["X-Stainless-Package-Version"])
}

// Independent oracle: official dsh attribution; Kimi oauth/identity.ts and
// Anthropic SDK 0.95.2; MiniMax model-resolver-helpers.ts and Anthropic SDK 0.91.1;
// ZCode source headers + runner AI SDK 6.0.193. The fixed Ubuntu host is the
// product requirement, not the OS that happens to run these tests.
func officialDomesticAnthropicHeaders(platform, kind string) map[string]string {
	headers := map[string]string{}
	switch platform {
	case PlatformDeepseek:
		headers["User-Agent"] = "deepseek-harness/0.2.0-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)"
	case PlatformKimi:
		headers = map[string]string{"User-Agent": "kimi-code-cli/2.1.1", "X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": "2.1.1", "X-Msh-Device-Name": "ubuntu", "X-Msh-Device-Model": "Linux 6.8.0-31-generic x64", "X-Msh-Os-Version": "6.8.0-31-generic"}
	case PlatformMiniMax:
		headers["User-Agent"] = "MiniMaxAgent"
		if kind == AccountTypeAPIKey {
			headers["User-Agent"] = "Anthropic/JS 0.91.1"
		}
	case PlatformZhipu:
		headers = map[string]string{"User-Agent": "ZCode/3.14.3 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22", "X-ZCode-App-Version": "3.14.3", "X-ZCode-Agent": "glm", "HTTP-Referer": "https://zcode.z.ai", "X-Title": "Z Code@electron", "X-Release-Channel": "production", "X-Platform": "linux-x64", "X-Os-Category": "linux", "X-Os-Version": "6.8.0-31-generic", "X-Client-Language": "en-US", "X-Client-Timezone": "UTC"}
	}
	if platform == PlatformMiniMax || platform == PlatformKimi {
		for key, value := range map[string]string{"X-Stainless-Lang": "js", "X-Stainless-OS": "Linux", "X-Stainless-Arch": "x64", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0"} {
			headers[key] = value
		}
		headers["X-Stainless-Package-Version"] = "0.91.1"
		if platform == PlatformKimi {
			headers["X-Stainless-Package-Version"] = "0.95.2"
		}
	}
	normalized := map[string]string{}
	for key, value := range headers {
		normalized[http.CanonicalHeaderKey(key)] = value
	}
	return normalized
}
