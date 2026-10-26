//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokAuxiliaryTokenCache struct {
	GeminiTokenCache
	reads  int
	onRead func()
}

func (c *grokAuxiliaryTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	if c.onRead != nil {
		c.onRead()
	}
	return "private-grok-token", nil
}

type grokAuxiliaryUpstream struct {
	requests []*http.Request
	proxies  []string
	err      error
}

func (u *grokAuxiliaryUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	u.proxies = append(u.proxies, proxy)
	if u.err != nil {
		return nil, u.err
	}
	body := `{"data":[{"url":"https://images.example.test/output.png"}],"request_id":"task-1","status":"completed","text":"hello"}`
	if strings.HasSuffix(req.URL.Path, "/settings") {
		body = `{"accept_request_encodings":["zstd"]}`
	}
	return &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (u *grokAuxiliaryUpstream) DoWithTLS(req *http.Request, proxy string, owner int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, owner, concurrency)
}

func grokStrictProxyAccount(accountType, mode string) *Account {
	proxyID := int64(7931)
	account := &Account{ID: 7932, Platform: PlatformGrok, Type: accountType, Status: StatusActive, Schedulable: true,
		Concurrency: 2, Credentials: map[string]any{
			"api_key": "private-grok-token", "access_token": "private-grok-token", "refresh_token": "private-grok-refresh",
			"expires_at":               time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok", UserAgent: "grok-shell/1.0.46 (linux; x86_64)"},
		}, ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "owner.proxy.example", Port: 8080,
			Username: "private-proxy-user", Password: "private-proxy-password", Status: StatusActive}}
	setOpenAIStrictPreflightProxyMode(account, mode)
	return account
}

func grokAuxiliaryInbound(method, path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, path, nil)
	return c
}

func grokAuxiliaryAssertSafeError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	for _, secret := range []string{"private-grok-token", "private-grok-refresh", "private-proxy-user", "private-proxy-password"} {
		require.NotContains(t, err.Error(), secret)
	}
}

// Voice and media bypass the ordinary Chat forwarding helper. Each independently
// callable entrance must reject a broken configured route before credential
// acquisition or transport; only an explicitly unbound owner may go direct.
func TestGrokAuxiliaryHTTPRejectsBrokenProxyBeforeCredentialOrSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := []struct {
		name, method, endpoint string
		media                  GrokMediaEndpoint
	}{
		{name: "tts", method: http.MethodPost, endpoint: "tts"},
		{name: "stt", method: http.MethodPost, endpoint: "stt"},
		{name: "custom-voice-create", method: http.MethodPost, endpoint: "custom-voices"},
		{name: "custom-voice-list", method: http.MethodGet, endpoint: "custom-voices"},
		{name: "custom-voice-get", method: http.MethodGet, endpoint: "custom-voices/voice-1"},
		{name: "custom-voice-audio", method: http.MethodGet, endpoint: "custom-voices/voice-1/audio"},
		{name: "custom-voice-delete", method: http.MethodDelete, endpoint: "custom-voices/voice-1"},
		{name: "image-create", media: GrokMediaEndpointImagesGenerations},
		{name: "image-edit", media: GrokMediaEndpointImagesEdits},
		{name: "video-create", media: GrokMediaEndpointVideosGenerations},
		{name: "video-edit", media: GrokMediaEndpointVideosEdits},
		{name: "video-extend", media: GrokMediaEndpointVideosExtensions},
		{name: "video-status", media: GrokMediaEndpointVideoStatus},
		{name: "video-content", media: GrokMediaEndpointVideoContent},
	}
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, path := range paths {
			for _, mode := range openAIStrictPreflightProxyModes {
				t.Run(accountType+"/"+path.name+"/"+mode, func(t *testing.T) {
					account := grokStrictProxyAccount(accountType, mode)
					before, err := json.Marshal(account.Credentials)
					require.NoError(t, err)
					cache := &grokAuxiliaryTokenCache{}
					upstream := &grokAuxiliaryUpstream{}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream,
						grokTokenProvider: NewGrokTokenProvider(nil, cache)}
					body := []byte(`{"model":"grok-imagine-image","prompt":"a tree","image":{"url":"https://images.example.test/input.png"},"video":{"url":"https://videos.example.test/input.mp4"},"text":"hello"}`)
					var result *OpenAIForwardResult
					if path.media != "" {
						result, err = svc.ForwardGrokMedia(context.Background(), grokAuxiliaryInbound(path.media.httpMethod(), "/v1/videos/task-1"), account, path.media, "task-1", body, "application/json")
					} else {
						result, err = svc.ForwardGrokVoice(context.Background(), grokAuxiliaryInbound(path.method, "/v1/"+path.endpoint), account, path.endpoint, body, "application/json")
					}
					if mode != "valid-bound" && mode != "explicitly-unbound" {
						require.Zero(t, cache.reads, "validate the saved binding before asking for an OAuth credential")
						require.Empty(t, upstream.requests, "a broken route cannot reach a direct or replacement transport")
						require.Nil(t, result)
						grokAuxiliaryAssertSafeError(t, err)
						after, marshalErr := json.Marshal(account.Credentials)
						require.NoError(t, marshalErr)
						require.JSONEq(t, string(before), string(after))
						return
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					wantCalls := 1
					if path.media == GrokMediaEndpointVideoContent {
						wantCalls = 2 // status and content retain the same acquired route.
					}
					require.Len(t, upstream.requests, wantCalls)
					for i, req := range upstream.requests {
						require.Equal(t, "Bearer private-grok-token", req.Header.Get("Authorization"))
						wantUA := "grok-shell/1.0.46 (linux; x86_64)"
						if path.media != "" {
							wantUA = "xai-grok-build/1.0.46"
						}
						require.Equal(t, wantUA, req.UserAgent())
						require.Equal(t, "1.0.46", req.Header.Get("X-Grok-Client-Version"))
						require.Equal(t, "grok-shell", req.Header.Get("X-Grok-Client-Identifier"))
						if mode == "valid-bound" {
							require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", upstream.proxies[i])
						} else {
							require.Empty(t, upstream.proxies[i], "a stale hydrated proxy object cannot override explicit unbinding")
						}
					}
				})
			}
		}
	}
}

func TestGrokRealtimeRejectsBrokenProxyBeforeDial(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, operation := range []string{"open", "probe"} {
			for _, mode := range openAIStrictPreflightProxyModes {
				t.Run(accountType+"/"+operation+"/"+mode, func(t *testing.T) {
					account := grokStrictProxyAccount(accountType, mode)
					connection := newLiveTestFrameConn()
					t.Cleanup(func() { _ = connection.Close() })
					dialer := &liveStrictProxyDialer{conn: connection}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, openaiWSPassthroughDialer: dialer}
					var err error
					if operation == "open" {
						var upstream *GrokRealtimeUpstream
						upstream, err = svc.OpenGrokRealtime(context.Background(), account, "private-grok-token", "grok-voice-latest")
						if upstream != nil {
							t.Cleanup(func() { _ = upstream.Close() })
						}
					} else {
						err = svc.ProbeGrokRealtime(context.Background(), account, "private-grok-token", "grok-voice-latest")
					}
					if mode != "valid-bound" && mode != "explicitly-unbound" {
						require.Zero(t, dialer.calls)
						grokAuxiliaryAssertSafeError(t, err)
						return
					}
					require.NoError(t, err)
					require.Equal(t, 1, dialer.calls)
					require.Equal(t, "wss://api.x.ai/v1/realtime?model=grok-voice-latest", dialer.url)
					require.Equal(t, "Bearer private-grok-token", dialer.headers.Get("Authorization"))
					if mode == "valid-bound" {
						require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", dialer.proxy)
					} else {
						require.Empty(t, dialer.proxy)
					}
				})
			}
		}
	}
}

func TestGrokAuxiliaryHTTPCapturesOwnerBeforeCredentialLookup(t *testing.T) {
	for _, path := range []string{"voice", "image", "video-content"} {
		t.Run(path, func(t *testing.T) {
			account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "1.0.46"}
			cache := &grokAuxiliaryTokenCache{onRead: func() {
				account.Proxy.Host = "replacement.proxy.example"
				account.Proxy.Password = "replacement-password"
				account.Credentials["access_token"] = "replacement-token"
				account.Credentials["base_url"] = "https://replacement-relay.example/v1"
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
			}}
			upstream := &grokAuxiliaryUpstream{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream,
				grokTokenProvider: NewGrokTokenProvider(nil, cache)}
			body := []byte(`{"model":"grok-imagine-image","prompt":"a tree"}`)
			var err error
			switch path {
			case "voice":
				_, err = svc.ForwardGrokVoice(context.Background(), grokAuxiliaryInbound(http.MethodPost, "/v1/tts"), account, "tts", body, "application/json")
			case "image":
				_, err = svc.ForwardGrokMedia(context.Background(), grokAuxiliaryInbound(http.MethodPost, "/v1/images/generations"), account, GrokMediaEndpointImagesGenerations, "", body, "application/json")
			case "video-content":
				_, err = svc.ForwardGrokMedia(context.Background(), grokAuxiliaryInbound(http.MethodGet, "/v1/videos/task-1/content"), account, GrokMediaEndpointVideoContent, "task-1", nil, "")
			}
			require.NoError(t, err)
			require.Equal(t, 1, cache.reads)
			wantCalls := 1
			if path == "video-content" {
				wantCalls = 2
			}
			require.Len(t, upstream.requests, wantCalls)
			for i, req := range upstream.requests {
				require.Equal(t, "api.x.ai", req.URL.Host, "a relink during credential lookup cannot redirect the acquired bearer")
				require.Equal(t, "Bearer private-grok-token", req.Header.Get("Authorization"))
				require.Equal(t, "1.0.46", req.Header.Get("X-Grok-Client-Version"))
				require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", upstream.proxies[i])
			}
			require.Equal(t, "replacement-token", account.GetCredential("access_token"), "the request snapshot must not overwrite the new login")
		})
	}
}

func TestGrokCompressionRejectsBrokenProxyBeforeCapabilityOrCache(t *testing.T) {
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			resetGrokRequestCompressionCapabilityCacheForTest()
			t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)
			account := grokStrictProxyAccount(AccountTypeOAuth, mode)
			account.Credentials["base_url"] = xai.DefaultCLIBaseURL
			upstream := &grokAuxiliaryUpstream{}
			svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
			body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*2)
			request, err := svc.buildGrokResponsesRequest(context.Background(), nil, account, body, "private-grok-token", "", svc.cfg)
			require.NoError(t, err)
			if mode != "valid-bound" && mode != "explicitly-unbound" {
				require.Empty(t, upstream.requests)
				require.Empty(t, request.Header.Get("Content-Encoding"))
				grokRequestCompressionCapabilityCache.mu.Lock()
				entries := len(grokRequestCompressionCapabilityCache.entries)
				grokRequestCompressionCapabilityCache.mu.Unlock()
				require.Zero(t, entries, "a rejected route must not create a capability observation")
				return
			}
			require.Equal(t, "zstd", request.Header.Get("Content-Encoding"))
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "https://cli-chat-proxy.grok.com/v1/settings", upstream.requests[0].URL.String())
			require.Equal(t, "Bearer private-grok-token", upstream.requests[0].Header.Get("Authorization"))
			if mode == "valid-bound" {
				require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", upstream.proxies[0])
			} else {
				require.Empty(t, upstream.proxies[0])
			}
		})
	}
}
