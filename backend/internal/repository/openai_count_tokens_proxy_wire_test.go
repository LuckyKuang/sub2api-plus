//go:build unit

package repository

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
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

func countTokensWireCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"api.openai.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, roots
}

type countTokensWireRequest struct {
	method, host, path, body string
	headers                  http.Header
}

// Both count_tokens sending entries retain the official HTTPS destination and
// use the actual gateway, HTTPUpstream, proxy selection, CONNECT and TLS send.
// Only the isolated transport's trust roots and official-host address mapping
// differ: a mistakenly direct dial reaches the observable local origin instead
// of the public internet. The direct success control proves it is reachable.
func TestCountTokensProductionProxyContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	certificate, roots := countTokensWireCertificate(t)
	for _, entry := range []string{"responses-native", "messages-count"} {
		for _, mode := range []string{"success", "unbound-direct", "refused", "timeout", "authentication-required", "missing-bound-route"} {
			t.Run(entry+"/"+mode, func(t *testing.T) {
				var originTCP, directDials, proxyRequests atomic.Int64
				var mu sync.Mutex
				var wire []countTokensWireRequest
				var proxyAuthorization, proxyTarget string
				var proxyErrors []error
				recordProxyError := func(err error) {
					mu.Lock()
					proxyErrors = append(proxyErrors, err)
					mu.Unlock()
				}
				origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					body, err := io.ReadAll(request.Body)
					mu.Lock()
					if err != nil {
						proxyErrors = append(proxyErrors, err)
					}
					wire = append(wire, countTokensWireRequest{request.Method, request.Host, request.URL.Path, string(body), request.Header.Clone()})
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Connection", "close")
					_, _ = io.WriteString(w, `{"object":"response.input_tokens","input_tokens":42}`)
				}))
				origin.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
				origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						originTCP.Add(1)
					}
				}
				origin.StartTLS()
				t.Cleanup(origin.Close)
				release := make(chan struct{})
				var releaseOnce sync.Once
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					proxyRequests.Add(1)
					mu.Lock()
					proxyAuthorization, proxyTarget = request.Header.Get("Proxy-Authorization"), request.Host
					mu.Unlock()
					if request.Method != http.MethodConnect {
						recordProxyError(fmt.Errorf("count_tokens did not use CONNECT: %s", request.Method))
						http.Error(w, "CONNECT required", http.StatusBadRequest)
						return
					}
					if mode == "authentication-required" {
						w.Header().Set("Proxy-Authenticate", `Basic realm="isolated-count"`)
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					}
					client, buffered, err := w.(http.Hijacker).Hijack()
					if err != nil {
						recordProxyError(err)
						return
					}
					defer func() { _ = client.Close() }()
					if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
						recordProxyError(err)
						return
					}
					if err := buffered.Flush(); err != nil {
						recordProxyError(err)
						return
					}
					if mode == "timeout" {
						<-release // Stalled TLS tunnel; the origin receives no TCP connection.
						return
					}
					remote, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
					if err != nil {
						recordProxyError(err)
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
				proxyID := int64(7991)
				account := &service.Account{ID: 7992, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Concurrency: 2, ProxyID: &proxyID,
					Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port,
						Username: "private-count-proxy-user", Password: "private-count-proxy-password", Status: service.StatusActive},
					Credentials: map[string]any{"access_token": "private-count-wire-token", "refresh_token": "private-count-wire-refresh",
						"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
						"user_agent": "codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color"},
				}
				proxyURL := account.Proxy.URL()
				if mode == "unbound-direct" {
					account.ProxyID = nil // A residual object cannot create a route binding.
				}
				if mode == "missing-bound-route" {
					account.Proxy = nil
				}
				cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowPrivateHosts: true}}}
				upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
				for _, route := range []string{proxyURL, ""} {
					cached, err := upstream.getClientEntry(route, account.ID, account.Concurrency, service.HTTPUpstreamProfileOpenAI, false, false)
					require.NoError(t, err)
					transport := observedHTTPBaseTransport(t, cached.client.Transport)
					transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
					transport.TLSHandshakeTimeout = 150 * time.Millisecond
					dial := transport.DialContext
					transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
						if address == "api.openai.com:443" {
							directDials.Add(1)
							address = origin.Listener.Addr().String()
						}
						return dial(ctx, network, address)
					}
					t.Cleanup(cached.client.CloseIdleConnections)
				}
				gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg,
					nil, nil, nil, nil, nil, upstream, nil, service.NewOpenAITokenProvider(nil, nil, nil), nil, nil, nil, nil, nil, nil, nil)
				if mode == "refused" {
					proxy.Close()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				recorder := httptest.NewRecorder()
				inbound, _ := gin.CreateTestContext(recorder)
				path := "/v1/responses/input_tokens"
				if entry == "messages-count" {
					path = "/v1/messages/count_tokens"
				}
				inbound.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
				inbound.Request.Header.Set("Authorization", "Bearer private-inbound-count-token")
				inbound.Request.Header.Set("User-Agent", "untrusted-inbound-count/9.9")
				inbound.Request.Header.Set("Originator", "untrusted-inbound-count")
				inbound.Request.Header.Set("Version", "9.9")
				if entry == "messages-count" {
					err = gateway.ForwardCountTokensAsAnthropic(ctx, inbound, account,
						[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello world"}]}`), "gpt-5.4")
				} else {
					err = gateway.ForwardResponsesInputTokens(ctx, inbound, account,
						[]byte(`{"model":"gpt-5.4","input":[{"role":"user","content":"hello world"}]}`))
				}
				for _, cached := range upstream.clients {
					require.Zero(t, atomic.LoadInt64(&cached.inFlight), "every count_tokens outcome releases the production transport slot")
				}
				mu.Lock()
				requests := append([]countTokensWireRequest(nil), wire...)
				auth, target, observedErrors := proxyAuthorization, proxyTarget, append([]error(nil), proxyErrors...)
				mu.Unlock()
				require.Empty(t, observedErrors)
				if mode != "success" && mode != "unbound-direct" {
					require.Error(t, err)
					require.GreaterOrEqual(t, recorder.Code, 400, "a proxy failure cannot be a successful local estimate")
					require.Empty(t, requests, "the provider receives no request after a failed bound route")
					require.Zero(t, originTCP.Load(), "a proxy failure cannot authorize any direct origin TCP attempt")
					require.Zero(t, directDials.Load())
					if mode == "timeout" || mode == "authentication-required" {
						require.Equal(t, int64(1), proxyRequests.Load(), "there is no retry via a different route")
					} else {
						require.Zero(t, proxyRequests.Load())
					}
					for _, secret := range []string{"private-count-wire-token", "private-count-wire-refresh", "private-count-proxy-user", "private-count-proxy-password", "private-inbound-count-token"} {
						require.NotContains(t, err.Error(), secret)
						require.NotContains(t, recorder.Body.String(), secret)
					}
					return
				}
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, recorder.Code)
				var result struct {
					InputTokens int `json:"input_tokens"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
				require.Equal(t, 42, result.InputTokens)
				require.Len(t, requests, 1)
				require.Equal(t, int64(1), originTCP.Load())
				if mode == "unbound-direct" {
					require.Equal(t, int64(1), directDials.Load())
					require.Zero(t, proxyRequests.Load())
				} else {
					require.Zero(t, directDials.Load())
					require.Equal(t, int64(1), proxyRequests.Load())
					require.Equal(t, "api.openai.com:443", target)
					require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("private-count-proxy-user:private-count-proxy-password")), auth)
				}
				sent := requests[0]
				require.Equal(t, http.MethodPost, sent.method)
				require.Equal(t, "api.openai.com", sent.host)
				require.Equal(t, "/v1/responses/input_tokens", sent.path)
				require.Equal(t, "Bearer private-count-wire-token", sent.headers.Get("Authorization"))
				require.Empty(t, sent.headers.Get("Proxy-Authorization"), "proxy credentials cannot reach the provider")
				// Version synchronization preserves the configured account fingerprint.
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", sent.headers.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", sent.headers.Get("Originator"))
				require.Equal(t, "0.165.0", sent.headers.Get("Version"))
				require.Contains(t, sent.body, `"model":"gpt-5.4"`)
				require.Contains(t, sent.body, "hello world")
				require.NotContains(t, strings.ToLower(sent.body), "private-")
			})
		}
	}
}
