//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type grokAuxiliaryDialCapture struct {
	target  string
	headers http.Header
	proxy   string
}

type grokAuxiliaryConn struct{ closed bool }

func (*grokAuxiliaryConn) WriteJSON(context.Context, any) error        { return nil }
func (*grokAuxiliaryConn) ReadMessage(context.Context) ([]byte, error) { return nil, io.EOF }
func (*grokAuxiliaryConn) Ping(context.Context) error                  { return nil }
func (c *grokAuxiliaryConn) Close() error                              { c.closed = true; return nil }

type grokAuxiliaryDialer struct {
	captures    []grokAuxiliaryDialCapture
	connections []*grokAuxiliaryConn
}

func (d *grokAuxiliaryDialer) Dial(_ context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.captures = append(d.captures, grokAuxiliaryDialCapture{target: target, headers: headers.Clone(), proxy: proxy})
	conn := &grokAuxiliaryConn{}
	d.connections = append(d.connections, conn)
	return conn, http.StatusSwitchingProtocols, nil, nil
}

func grokAuxiliaryAccount(kind, base, token string, proxy *Proxy) *Account {
	account := &Account{ID: 90801, Platform: PlatformGrok, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 7,
		Credentials: map[string]any{
			"base_url": base, "api_key": token, "access_token": token,
			// Keep this token outside the refresh window: the test exercises
			// acquisition of an already rotated grant through version detection.
			"refresh_token": "refresh-token", "expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			"_token_version": int64(1), "header_override_enabled": true,
			"header_overrides": map[string]any{"X-Relay-Snapshot": "obsolete"},
		}, Proxy: proxy,
	}
	if proxy != nil {
		account.ProxyID = &proxy.ID
	}
	return account
}

func grokAuxiliaryProxy(t *testing.T, raw string) *Proxy {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Proxy{ID: 72, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive}
}

// An access-token rotation can advance the captured grant without changing its
// route or client declarations. Replacing those declarations requires account
// selection again and must not send through either old or replacement routes.
func TestGrokAuxiliaryHTTPUsesAcquiredOwner(t *testing.T) {
	for _, operation := range []string{"images", "voice", "video-content"} {
		for _, change := range []string{"access-token-rotation", "base-url-replaced", "proxy-replaced", "headers-replaced"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				var replacementProxyCalls atomic.Int64
				replacementProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					replacementProxyCalls.Add(1)
					w.WriteHeader(http.StatusBadGateway)
				}))
				defer replacementProxy.Close()
				captures := make(chan grokAuxiliaryDialCapture, 3)
				capturedProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					captures <- grokAuxiliaryDialCapture{target: request.URL.String(), headers: request.Header.Clone()}
					switch request.URL.Path {
					case "/v1/images/generations":
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"data":[{"b64_json":"aGVsbG8="}]}`)
					case "/v1/tts":
						w.Header().Set("Content-Type", "audio/mpeg")
						_, _ = io.WriteString(w, "audio-payload")
					case "/v1/videos/task-owner":
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"status":"completed"}`)
					case "/v1/videos/task-owner/content":
						w.Header().Set("Content-Type", "video/mp4")
						_, _ = io.WriteString(w, "video-payload")
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer capturedProxy.Close()
				selected := grokAuxiliaryAccount(AccountTypeOAuth, "http://captured-target.example/v1", "old-token", grokAuxiliaryProxy(t, capturedProxy.URL))
				selected.Credentials["header_overrides"] = map[string]any{"X-Relay-Snapshot": "captured"}
				owner := snapshotRequestCredentialAccount(selected)
				owner.Concurrency = 2
				owner.Credentials["access_token"] = "rotated-token"
				owner.Credentials["_token_version"] = int64(2)
				switch change {
				case "base-url-replaced":
					owner.Credentials["base_url"] = "http://replacement-target.example/v1"
				case "proxy-replaced":
					owner.Proxy = grokAuxiliaryProxy(t, replacementProxy.URL)
				case "headers-replaced":
					owner.Credentials["header_overrides"] = map[string]any{"X-Relay-Snapshot": "replacement"}
				}
				repo := &tokenRefreshAccountRepo{}
				repo.accountsByID = map[int64]*Account{selected.ID: owner}
				var sends int
				upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
					sends++
					require.Equal(t, selected.ID, accountID)
					require.Equal(t, 7, concurrency, "selected resource accounting survives token rotation")
					require.Equal(t, selected.Proxy.URL(), proxy)
					u, err := url.Parse(proxy)
					if err != nil {
						return nil, err
					}
					transport := &http.Transport{Proxy: http.ProxyURL(u)}
					defer transport.CloseIdleConnections()
					return (&http.Client{Transport: transport}).Do(request)
				}}
				cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream,
					grokTokenProvider: NewGrokTokenProvider(repo, &grokTokenCacheForProviderTest{}),
				}
				method := http.MethodPost
				if operation == "video-content" {
					method = http.MethodGet
				}
				c, recorder := grokMediaContentTestContext(method, "https://gateway.example/v1/auxiliary", nil)
				var result *OpenAIForwardResult
				var err error
				wantPaths := []string{"/v1/images/generations"}
				switch operation {
				case "images":
					result, err = svc.ForwardGrokMedia(context.Background(), c, selected, GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"grok-imagine-image","prompt":"hello"}`), "application/json")
				case "voice":
					result, err = svc.ForwardGrokVoice(context.Background(), c, selected, "tts", []byte(`{"text":"hello","voice_id":"eve"}`), "application/json")
					wantPaths = []string{"/v1/tts"}
				case "video-content":
					result, err = svc.ForwardGrokMedia(context.Background(), c, selected, GrokMediaEndpointVideoContent, "task-owner", nil, "")
					wantPaths = []string{"/v1/videos/task-owner", "/v1/videos/task-owner/content"}
				}
				if change != "access-token-rotation" {
					grokAuxiliaryAssertSafeError(t, err)
					require.Nil(t, result)
					require.Zero(t, sends, "a replacement owner must return to account selection before sending")
					require.Empty(t, captures)
					require.Zero(t, replacementProxyCalls.Load())
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, len(wantPaths), sends)
				require.Len(t, captures, len(wantPaths))
				for _, path := range wantPaths {
					capture := <-captures
					require.Equal(t, "http://captured-target.example"+path, capture.target)
					require.Equal(t, "Bearer rotated-token", capture.headers.Get("Authorization"))
					require.Equal(t, []string{"captured"}, headerValuesEqualFold(capture.headers, "X-Relay-Snapshot"))
				}
				require.Zero(t, replacementProxyCalls.Load(), "token rotation cannot change the captured route")
			})
		}
	}
}

func TestGrokAuxiliaryRealtimeUsesAcquiredOwner(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, operation := range []string{"open", "probe"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				selected := grokAuxiliaryAccount(kind, "https://obsolete-target.example/v1", "old-token", &Proxy{ID: 72, Protocol: "http", Host: "obsolete-proxy.example", Port: 8080})
				owner := snapshotRequestCredentialAccount(selected)
				owner.Credentials["base_url"] = "https://acquired-target.example/v1"
				owner.Credentials["access_token"], owner.Credentials["api_key"] = "new-token", "new-token"
				owner.Credentials["header_overrides"] = map[string]any{"X-Relay-Snapshot": "acquired"}
				owner.Proxy.Host = "acquired-proxy.example"
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				recordRequestCredentialOwner(ctx, owner, "new-token")
				ctx = withRequestCredentialOwner(ctx, selected, "new-token")
				dialer := &grokAuxiliaryDialer{}
				svc := &OpenAIGatewayService{openaiWSPassthroughDialer: dialer}
				if operation == "open" {
					connection, err := svc.OpenGrokRealtime(ctx, selected, "new-token", "grok-voice-test")
					require.NoError(t, err)
					require.NoError(t, connection.Close())
				} else {
					require.NoError(t, svc.ProbeGrokRealtime(ctx, selected, "new-token", "grok-voice-test"))
				}
				require.Len(t, dialer.captures, 1)
				capture := dialer.captures[0]
				require.Equal(t, "wss://acquired-target.example/v1/realtime?model=grok-voice-test", capture.target)
				require.Equal(t, "http://acquired-proxy.example:8080", capture.proxy)
				require.Equal(t, "Bearer new-token", capture.headers.Get("Authorization"))
				require.Equal(t, []string{"acquired"}, headerValuesEqualFold(capture.headers, "X-Relay-Snapshot"))
				require.True(t, dialer.connections[0].closed)
			})
		}
	}
}

func TestGrokAuxiliaryUnavailableOwnerNeverSends(t *testing.T) {
	for _, operation := range []string{"images", "video-content", "voice", "open", "probe"} {
		for _, failure := range []string{"missing-proxy", "unknown-owner"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				account := grokAuxiliaryAccount(AccountTypeAPIKey, "https://obsolete-target.example/v1", "selected-token", nil)
				token := "selected-token"
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				if failure == "missing-proxy" {
					id := int64(72)
					account.ProxyID = &id
				} else {
					token = "unowned-token"
					ctx = withRequestCredentialOwner(ctx, account, token)
				}
				upstream := &httpUpstreamRecorder{}
				dialer := &grokAuxiliaryDialer{}
				svc := &OpenAIGatewayService{httpUpstream: upstream, openaiWSPassthroughDialer: dialer}
				c, _ := grokMediaContentTestContext(http.MethodPost, "https://gateway.example/v1/auxiliary", nil)
				var err error
				switch operation {
				case "images":
					_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"grok-imagine-image","prompt":"hello"}`), "application/json")
				case "video-content":
					_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoContent, "task-owner", nil, "")
				case "voice":
					_, err = svc.ForwardGrokVoice(ctx, c, account, "tts", []byte(`{"text":"hello"}`), "application/json")
				case "open":
					_, err = svc.OpenGrokRealtime(ctx, account, token, "grok-voice-test")
				case "probe":
					err = svc.ProbeGrokRealtime(ctx, account, token, "grok-voice-test")
				}
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				require.Empty(t, dialer.captures)
			})
		}
	}
}
