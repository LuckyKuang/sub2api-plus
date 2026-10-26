//go:build unit

package repository

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokAuxiliaryWireRequest struct {
	method, path, body string
	headers            http.Header
}

// These independent Voice/Imagine/status/content entrypoints use production
// HTTPUpstream through authenticated CONNECT and a verified local TLS origin.
// A TLS-handshake timeout is shortened only in the isolated cached transport;
// the fixture does not replace its proxy, dialer or request execution.
func TestGrokAuxiliaryHTTPProductionProxyContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		for _, operation := range []string{"voice", "image", "video", "status", "content"} {
			for _, mode := range []string{"success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect"} {
				t.Run(accountType+"/"+operation+"/"+mode, func(t *testing.T) {
					var targetTCP, proxyRequests atomic.Int64
					var wireMu sync.Mutex
					var wire []grokAuxiliaryWireRequest
					var proxyAuthorization, proxyTarget string
					origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						data, _ := io.ReadAll(r.Body)
						wireMu.Lock()
						wire = append(wire, grokAuxiliaryWireRequest{r.Method, r.URL.Path, string(data), r.Header.Clone()})
						wireMu.Unlock()
						w.Header().Set("Content-Type", "application/json")
						response := `{"data":[{"url":"https://images.example.test/output.png"}],"request_id":"task-1","status":"completed"}`
						if strings.HasSuffix(r.URL.Path, "/content") {
							w.Header().Set("Content-Type", "video/mp4")
							response = "isolated-video-bytes"
						}
						if strings.HasSuffix(r.URL.Path, "/tts") {
							w.Header().Set("Content-Type", "audio/wav")
							response = "isolated-audio-bytes"
						}
						_, _ = io.WriteString(w, response)
					}))
					origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
						if state == http.StateNew {
							targetTCP.Add(1)
						}
					}
					origin.StartTLS()
					t.Cleanup(origin.Close)
					release, entered := make(chan struct{}), make(chan struct{})
					var releaseOnce, enteredOnce sync.Once
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						proxyRequests.Add(1)
						wireMu.Lock()
						proxyAuthorization, proxyTarget = r.Header.Get("Proxy-Authorization"), r.Host
						wireMu.Unlock()
						enteredOnce.Do(func() { close(entered) })
						if r.Method != http.MethodConnect {
							http.Error(w, "CONNECT required", http.StatusBadRequest)
							return
						}
						if mode == "cancelled" {
							<-release
						}
						if mode == "authentication-required" || mode == "cancelled" {
							w.Header().Set("Proxy-Authenticate", `Basic realm="isolated-grok"`)
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
						if mode == "edited-after-connect" {
							<-release
						}
						client, buffered, err := w.(http.Hijacker).Hijack()
						if err != nil {
							return
						}
						defer func() { _ = client.Close() }()
						_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
						if buffered.Flush() != nil {
							return
						}
						if mode == "timeout" {
							<-release
							return
						} // accepted CONNECT, stalled TLS tunnel, zero origin connections.
						remote, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
						if err != nil {
							return
						}
						defer func() { _ = remote.Close() }()
						copied := make(chan struct{})
						go func() { _, _ = io.Copy(remote, buffered); close(copied) }()
						_, _ = io.Copy(client, remote)
						_ = remote.Close()
						_ = client.Close()
						<-copied
					}))
					t.Cleanup(proxy.Close)
					t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
					parsed, err := url.Parse(proxy.URL)
					require.NoError(t, err)
					port, err := strconv.Atoi(parsed.Port())
					require.NoError(t, err)
					proxyID := int64(7941)
					account := &service.Account{ID: 7942, Platform: service.PlatformGrok, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"api_key": "private-grok-wire-token", "access_token": "private-grok-wire-token", "refresh_token": "private-grok-wire-refresh", "expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), "base_url": origin.URL + "/v1",
							"outbound_identity": map[string]any{"preset": "grok", "user_agent": "grok-shell/1.0.46 (linux; x86_64)"}},
						ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port, Username: "private-proxy-user", Password: "private-proxy-password", Status: service.StatusActive}}
					cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowPrivateHosts: true}}}
					upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
					entry, err := upstream.getClientEntry(account.Proxy.URL(), account.ID, account.Concurrency, service.HTTPUpstreamProfileGrok, false, false)
					require.NoError(t, err)
					transport := observedHTTPBaseTransport(t, entry.client.Transport)
					transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
					if mode == "timeout" {
						transport.TLSHandshakeTimeout = 150 * time.Millisecond
					}
					t.Cleanup(entry.client.CloseIdleConnections)
					gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, service.NewGrokTokenProvider(nil, nil), nil, nil, nil, nil, nil, nil)
					body := []byte(`{"model":"grok-imagine-image","prompt":"a tree"}`)
					endpoint, requestID, ingress, method := service.GrokMediaEndpointImagesGenerations, "", "/v1/images/generations", http.MethodPost
					switch operation {
					case "voice":
						ingress = "/v1/tts"
						body = []byte(`{"text":"hello","voice_id":"eve"}`)
					case "video":
						endpoint, ingress = service.GrokMediaEndpointVideosGenerations, "/v1/videos/generations"
						body = []byte(`{"model":"grok-imagine-video","prompt":"a tree"}`)
					case "status":
						endpoint, requestID, ingress, method = service.GrokMediaEndpointVideoStatus, "task-1", "/v1/videos/task-1", http.MethodGet
						body = nil
					case "content":
						endpoint, requestID, ingress, method = service.GrokMediaEndpointVideoContent, "task-1", "/v1/videos/task-1/content", http.MethodGet
						body = nil
					}
					if mode == "refused" {
						proxy.Close()
					}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					inbound, _ := gin.CreateTestContext(httptest.NewRecorder())
					inbound.Request = httptest.NewRequest(method, ingress, strings.NewReader(string(body))).WithContext(ctx)
					type forwardResult struct {
						result *service.OpenAIForwardResult
						err    error
					}
					completed := make(chan forwardResult, 1)
					go func() {
						if operation == "voice" {
							result, err := gateway.ForwardGrokVoice(ctx, inbound, account, "tts", body, "application/json")
							completed <- forwardResult{result, err}
							return
						}
						result, err := gateway.ForwardGrokMedia(ctx, inbound, account, endpoint, requestID, body, "application/json")
						completed <- forwardResult{result, err}
					}()
					if mode == "cancelled" || mode == "edited-after-connect" {
						select {
						case <-entered:
						case <-ctx.Done():
							t.Fatal("proxy did not receive CONNECT")
						}
						if mode == "cancelled" {
							cancel()
						} else {
							account.Credentials["api_key"], account.Credentials["access_token"] = "replacement-token", "replacement-token"
							account.Credentials["base_url"] = "https://replacement.invalid/v1"
							account.Proxy.Host, account.Proxy.Password = "replacement.invalid", "replacement-password"
						}
						releaseOnce.Do(func() { close(release) })
					}
					var outcome forwardResult
					select {
					case outcome = <-completed:
					case <-time.After(4 * time.Second):
						t.Fatal("isolated HTTP operation did not complete")
					}
					if mode != "success" && mode != "edited-after-connect" {
						require.Error(t, outcome.err)
						require.Nil(t, outcome.result)
						require.Zero(t, targetTCP.Load(), "a proxy failure cannot authorize direct TCP egress")
						for _, secret := range []string{"private-grok-wire-token", "private-grok-wire-refresh", "private-proxy-user", "private-proxy-password"} {
							require.NotContains(t, outcome.err.Error(), secret)
						}
						if mode != "refused" {
							require.Equal(t, int64(1), proxyRequests.Load())
						}
						return
					}
					require.NoError(t, outcome.err)
					require.NotNil(t, outcome.result)
					wireMu.Lock()
					requests, auth, target := append([]grokAuxiliaryWireRequest(nil), wire...), proxyAuthorization, proxyTarget
					wireMu.Unlock()
					wantCalls := 1
					if operation == "content" {
						wantCalls = 2
					}
					require.Len(t, requests, wantCalls)
					require.Positive(t, targetTCP.Load())
					require.Positive(t, proxyRequests.Load())
					require.Equal(t, origin.Listener.Addr().String(), target)
					require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("private-proxy-user:private-proxy-password")), auth)
					for _, request := range requests {
						require.Equal(t, "Bearer private-grok-wire-token", request.headers.Get("Authorization"))
						require.Empty(t, request.headers.Get("Proxy-Authorization"))
						require.Equal(t, "1.0.46", request.headers.Get("X-Grok-Client-Version"))
						require.Equal(t, "grok-shell", request.headers.Get("X-Grok-Client-Identifier"))
						wantUA := "xai-grok-build/1.0.46"
						if operation == "voice" {
							wantUA = "grok-shell/1.0.46 (linux; x86_64)"
						}
						require.Equal(t, wantUA, request.headers.Get("User-Agent"))
					}
					require.Equal(t, method, requests[0].method)
					wantPath := ingress
					if operation == "content" {
						wantPath = "/v1/videos/task-1"
					}
					require.Equal(t, wantPath, requests[0].path)
					if operation == "voice" {
						require.JSONEq(t, `{"text":"hello","voice_id":"eve"}`, requests[0].body)
					}
					if operation == "image" {
						require.JSONEq(t, `{"model":"grok-imagine-image","prompt":"a tree"}`, requests[0].body)
					}
					if operation == "video" {
						require.JSONEq(t, `{"model":"grok-imagine-video","prompt":"a tree"}`, requests[0].body)
					}
					if operation == "status" || operation == "content" {
						require.Empty(t, requests[0].body)
					}
					if operation == "content" {
						require.Equal(t, "/v1/videos/task-1/content", requests[1].path)
						require.Equal(t, http.MethodGet, requests[1].method)
						require.Empty(t, requests[1].body)
					}
				})
			}
		}
	}
}
