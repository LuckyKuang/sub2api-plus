//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
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

// Only the official hostname is replaced by an isolated local TLS endpoint.
// Service header/owner/route preparation and the WebSocket dialer are production
// implementations. No external destination can be reached by this fixture.
type liveWireLocalDestination struct {
	dialer   *coderOpenAIWSClientDialer
	local    string
	official string
}

func (d *liveWireLocalDestination) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.official = target
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, 0, nil, err
	}
	return d.dialer.Dial(ctx, d.local+parsed.EscapedPath(), headers, proxy)
}

// docs/OUTBOUND_IDENTITY.md and the Live contract require the saved credential
// owner, attestation and route at the actual WebSocket handshake. Proxy failures
// must not authorize a direct TCP attempt or leak credentials in returned errors.
func TestLiveSidebandProductionProxyHandshakeAndFailureNeverDialDirect(t *testing.T) {
	for _, mode := range []string{"success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect"} {
		t.Run(mode, func(t *testing.T) {
			var targetTCP, proxyRequests atomic.Int64
			var wireMu sync.Mutex
			var wireHeaders http.Header
			var proxyAuthorization string
			var proxyTarget string
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wireMu.Lock()
				wireHeaders = r.Header.Clone()
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
					if writeErr := conn.Write(r.Context(), kind, body); writeErr != nil {
						return
					}
				}
			}))
			upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					targetTCP.Add(1)
				}
			}
			upstream.StartTLS()
			t.Cleanup(upstream.Close)
			release := make(chan struct{})
			entered := make(chan struct{})
			var releaseOnce, enteredOnce sync.Once
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyRequests.Add(1)
				wireMu.Lock()
				proxyAuthorization = r.Header.Get("Proxy-Authorization")
				proxyTarget = r.Host
				wireMu.Unlock()
				enteredOnce.Do(func() { close(entered) })
				if r.Method != http.MethodConnect {
					http.Error(w, "CONNECT required", http.StatusBadRequest)
					return
				}
				if mode == "authentication-required" {
					w.Header().Set("Proxy-Authenticate", `Basic realm="local-contract"`)
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
				remote, dialErr := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
				if dialErr != nil {
					http.Error(w, "local tunnel unavailable", http.StatusBadGateway)
					return
				}
				client, buffered, hijackErr := w.(http.Hijacker).Hijack()
				if hijackErr != nil {
					_ = remote.Close()
					return
				}
				defer func() { _ = client.Close(); _ = remote.Close() }()
				_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				if flushErr := buffered.Flush(); flushErr != nil {
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
			proxyID := int64(7931)
			account := &Account{ID: 7932, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Status: StatusActive, Concurrency: 1,
				Credentials: map[string]any{"access_token": "private-live-wire-token", "chatgpt_account_id": "live-wire-owner"},
				ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port,
					Username: "private-wire-proxy-user", Password: "private-wire-proxy-password", Status: StatusActive}}
			cipher := newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "local-live-wire-test-key"}})
			attestation := `{"v":1,"s":0,"t":"v1.local-wire-attestation"}`
			ciphertext, err := cipher.Encrypt(attestation)
			require.NoError(t, err)
			record := &LiveCallRecord{CallID: "call_wire", CallHash: hashLiveCallID("call_wire"), AccountID: account.ID,
				APIKeyID: 7933, UserID: 7934, LeaseID: "wire-lease", Controller: LiveControllerPending,
				AttestationCiphertext: ciphertext, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
				OutboundSnapshot: &LiveOutboundSnapshot{Owner: openAICodexStateOwner(context.Background(), account),
					SessionID: "01912345-6789-7abc-8def-0123456789ab", ThreadID: "01912345-7000-7fed-9234-123456789abc",
					UserAgent: DefaultOpenAICodexUserAgent, Originator: "codex_cli_rs", Version: codexCLIVersion}}
			originalRecord := *record
			originalSnapshot := *record.OutboundSnapshot
			originalRecord.OutboundSnapshot = &originalSnapshot
			production := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
			proxyClient, err := production.proxyHTTPClient(account.Proxy.URL())
			require.NoError(t, err)
			transport := proxyClient.Transport.(*http.Transport)
			transport.TLSClientConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			t.Cleanup(transport.CloseIdleConnections)
			localDialer := &liveWireLocalDestination{dialer: production, local: "wss" + strings.TrimPrefix(upstream.URL, "https")}
			svc := &OpenAIGatewayService{accountRepo: &liveTestAccountRepo{account: account}, liveAttestationCipher: cipher,
				openaiWSPassthroughDialer: localDialer}
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
				connection liveFrameConn
				err        error
			}
			result := make(chan dialResult, 1)
			go func() { conn, dialErr := svc.dialLiveSideband(ctx, record); result <- dialResult{conn, dialErr} }()
			if mode == "cancelled" || mode == "edited-after-connect" {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("local proxy did not receive the handshake")
				}
				if mode == "cancelled" {
					cancel()
				} else {
					// The route and bearer were already captured before CONNECT.
					account.Credentials["access_token"] = "replacement-live-token"
					account.Credentials["chatgpt_account_id"] = "replacement-live-owner"
					account.Proxy.Host = "replacement.invalid"
					account.Proxy.Password = "replacement-proxy-password"
					releaseOnce.Do(func() { close(release) })
				}
			}
			var outcome dialResult
			select {
			case outcome = <-result:
			case <-time.After(3 * time.Second):
				t.Fatal("WebSocket dial ignored cancellation/deadline")
			}
			require.Equal(t, originalRecord, *record, "failed handshakes cannot mutate the call or its lease")
			require.Equal(t, "wss://chatgpt.com/backend-api/codex/call_wire", localDialer.official)
			if mode != "success" && mode != "edited-after-connect" {
				require.Error(t, outcome.err)
				require.Nil(t, outcome.connection)
				require.Zero(t, targetTCP.Load(), "no TLS or TCP connection to the destination after proxy failure")
				if mode == "cancelled" {
					require.ErrorIs(t, outcome.err, context.Canceled)
				}
				if mode == "timeout" {
					require.True(t, errors.Is(outcome.err, context.DeadlineExceeded))
				}
				for _, secret := range []string{"private-live-wire-token", "private-wire-proxy-user", "private-wire-proxy-password", attestation} {
					require.NotContains(t, outcome.err.Error(), secret)
				}
				if mode != "refused" {
					require.Equal(t, int64(1), proxyRequests.Load())
				}
				return
			}
			require.NoError(t, outcome.err)
			require.NotNil(t, outcome.connection)
			defer func() { _ = outcome.connection.Close() }()
			for _, frame := range []struct {
				kind coderws.MessageType
				body []byte
			}{
				{coderws.MessageText, []byte(`{"type":"session.update","session":{"instructions":"safe fixture"}}`)},
				{coderws.MessageBinary, []byte{0, 1, 2, 255}},
			} {
				require.NoError(t, outcome.connection.WriteFrame(ctx, frame.kind, frame.body))
				kind, echoed, readErr := outcome.connection.ReadFrame(ctx)
				require.NoError(t, readErr)
				require.Equal(t, frame.kind, kind)
				require.Equal(t, frame.body, echoed)
			}
			require.Equal(t, int64(1), targetTCP.Load())
			require.Equal(t, int64(1), proxyRequests.Load())
			wireMu.Lock()
			headers, auth, target := wireHeaders.Clone(), proxyAuthorization, proxyTarget
			wireMu.Unlock()
			require.Equal(t, upstream.Listener.Addr().String(), target)
			require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("private-wire-proxy-user:private-wire-proxy-password")), auth)
			require.Equal(t, "Bearer private-live-wire-token", headers.Get("Authorization"))
			require.Equal(t, "live-wire-owner", headers.Get("ChatGPT-Account-ID"))
			require.Equal(t, attestation, headers.Get(liveAttestationHeader))
			require.Equal(t, DefaultOpenAICodexUserAgent, headers.Get("User-Agent"))
			require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
			require.Equal(t, codexCLIVersion, headers.Get("Version"))
			require.Empty(t, headers.Get("Proxy-Authorization"), "proxy credentials stay outside the TLS tunnel")
			require.Empty(t, headers.Get("OpenAI-Beta"))
		})
	}
}
