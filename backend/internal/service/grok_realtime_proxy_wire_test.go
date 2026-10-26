//go:build unit

package service

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
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type grokRealtimeWireDestination struct {
	dialer          *coderOpenAIWSClientDialer
	local, official string
}

func (d *grokRealtimeWireDestination) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.official = target
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, 0, nil, err
	}
	return d.dialer.Dial(ctx, d.local+parsed.RequestURI(), headers, proxy)
}

// Only the official origin is mapped to the isolated TLS listener. The actual
// production WebSocket client performs CONNECT, proxy auth, TLS verification,
// handshake and framing. No live credential or external request is involved.
func TestGrokRealtimeProductionProxyHandshakeAndFailureNeverDialDirect(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, operation := range []string{"open", "probe"} {
			for _, mode := range []string{"success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect"} {
				t.Run(accountType+"/"+operation+"/"+mode, func(t *testing.T) {
					var targetTCP, proxyRequests atomic.Int64
					var wireMu sync.Mutex
					var wireHeaders http.Header
					var wireQuery, proxyAuthorization, proxyTarget string
					origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						wireMu.Lock()
						wireHeaders, wireQuery = r.Header.Clone(), r.URL.RawQuery
						wireMu.Unlock()
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							return
						}
						defer func() { _ = conn.CloseNow() }()
						for {
							kind, body, readErr := conn.Read(r.Context())
							if readErr != nil {
								return
							}
							if conn.Write(r.Context(), kind, body) != nil {
								return
							}
						}
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
						if mode == "authentication-required" {
							w.Header().Set("Proxy-Authenticate", `Basic realm="local-grok"`)
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
						if mode == "timeout" || mode == "cancelled" {
							<-release
							return
						}
						if mode == "edited-after-connect" {
							<-release
						}
						remote, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
						if err != nil {
							http.Error(w, "isolated tunnel unavailable", http.StatusBadGateway)
							return
						}
						client, buffered, err := w.(http.Hijacker).Hijack()
						if err != nil {
							_ = remote.Close()
							return
						}
						defer func() { _ = client.Close(); _ = remote.Close() }()
						_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
						if buffered.Flush() != nil {
							return
						}
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
					account := grokStrictProxyAccount(accountType, "valid-bound")
					account.Proxy.Host, account.Proxy.Port = parsed.Hostname(), port
					production := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
					proxyClient, err := production.proxyHTTPClient(account.Proxy.URL())
					require.NoError(t, err)
					transport := proxyClient.Transport.(*http.Transport)
					transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
					t.Cleanup(transport.CloseIdleConnections)
					dialer := &grokRealtimeWireDestination{dialer: production, local: "wss" + strings.TrimPrefix(origin.URL, "https")}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, openaiWSPassthroughDialer: dialer}
					if mode == "refused" {
						proxy.Close()
					}
					deadline := 2 * time.Second
					if mode == "timeout" {
						deadline = 150 * time.Millisecond
					}
					ctx, cancel := context.WithTimeout(context.Background(), deadline)
					defer cancel()
					type dialResult struct {
						upstream *GrokRealtimeUpstream
						err      error
					}
					result := make(chan dialResult, 1)
					go func() {
						if operation == "probe" {
							result <- dialResult{err: svc.ProbeGrokRealtime(ctx, account, "private-grok-token", "grok-voice-latest")}
							return
						}
						upstream, err := svc.OpenGrokRealtime(ctx, account, "private-grok-token", "grok-voice-latest")
						result <- dialResult{upstream, err}
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
							account.Credentials["access_token"] = "replacement-token"
							account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
							account.Proxy.Host, account.Proxy.Password = "replacement.invalid", "replacement-password"
							releaseOnce.Do(func() { close(release) })
						}
					}
					var outcome dialResult
					select {
					case outcome = <-result:
					case <-time.After(3 * time.Second):
						t.Fatal("WebSocket dial exceeded its deadline")
					}
					require.Equal(t, "wss://api.x.ai/v1/realtime?model=grok-voice-latest", dialer.official)
					if mode != "success" && mode != "edited-after-connect" {
						grokAuxiliaryAssertSafeError(t, outcome.err)
						require.Nil(t, outcome.upstream)
						require.Zero(t, targetTCP.Load(), "failed proxy cannot authorize a direct TCP connection")
						if mode == "cancelled" {
							require.ErrorIs(t, outcome.err, context.Canceled)
						}
						if mode == "timeout" {
							require.ErrorIs(t, outcome.err, context.DeadlineExceeded)
						}
						if mode != "refused" {
							require.Equal(t, int64(1), proxyRequests.Load())
						}
						return
					}
					require.NoError(t, outcome.err)
					if operation == "open" {
						require.NotNil(t, outcome.upstream)
						defer func() { _ = outcome.upstream.Close() }()
						require.NoError(t, outcome.upstream.conn.WriteJSON(ctx, map[string]string{"type": "session.update"}))
						echoed, readErr := outcome.upstream.conn.ReadMessage(ctx)
						require.NoError(t, readErr)
						require.JSONEq(t, `{"type":"session.update"}`, string(echoed))
					}
					require.Equal(t, int64(1), targetTCP.Load())
					require.Equal(t, int64(1), proxyRequests.Load())
					wireMu.Lock()
					headers, query, auth, target := wireHeaders.Clone(), wireQuery, proxyAuthorization, proxyTarget
					wireMu.Unlock()
					require.Equal(t, origin.Listener.Addr().String(), target)
					require.Equal(t, "model=grok-voice-latest", query)
					require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("private-proxy-user:private-proxy-password")), auth)
					require.Equal(t, "Bearer private-grok-token", headers.Get("Authorization"))
					require.Equal(t, "grok-shell/1.0.46 (linux; x86_64)", headers.Get("User-Agent"))
					require.Equal(t, "grok-shell", headers.Get("X-Grok-Client-Identifier"))
					require.Equal(t, "1.0.46", headers.Get("X-Grok-Client-Version"))
					require.Empty(t, headers.Get("Proxy-Authorization"))
				})
			}
		}
	}
}
