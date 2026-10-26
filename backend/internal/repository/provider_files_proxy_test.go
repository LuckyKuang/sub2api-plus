//go:build unit

package repository

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/stretchr/testify/require"
)

type providerFilesWireStore struct {
	service.ProviderFilesRepository
	files map[string]*service.ProviderFile
}

func (s *providerFilesWireStore) Reserve(_ context.Context, f *service.ProviderFile, _ *service.Account) (*service.ProviderFile, bool, error) {
	copy := *f
	s.files[f.PublicID] = &copy
	return f, true, nil
}
func (s *providerFilesWireStore) Get(_ context.Context, id string) (*service.ProviderFile, error) {
	f := s.files[id]
	if f == nil {
		return nil, service.ErrProviderFileNotFound
	}
	copy := *f
	return &copy, nil
}
func (s *providerFilesWireStore) Publish(_ context.Context, f *service.ProviderFile, _ *service.Account) error {
	copy := *f
	s.files[f.PublicID] = &copy
	return nil
}
func (s *providerFilesWireStore) SetState(_ context.Context, id, state, remote string) error {
	f := s.files[id]
	f.State = state
	f.RemoteID = remote
	return nil
}
func (s *providerFilesWireStore) BeginDelete(_ context.Context, id, token, digest string) (*service.ProviderFile, bool, error) {
	f := s.files[id]
	f.State = service.ProviderFileDeletePending
	return f, true, nil
}
func (s *providerFilesWireStore) FinishDelete(_ context.Context, id, token string, success bool) error {
	if success {
		s.files[id].State = service.ProviderFileDeleted
	}
	return nil
}

type providerFilesWireAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *providerFilesWireAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

func providerFilesWireCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(111), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{host}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, roots
}

// Keep the official endpoint, real HTTPUpstream, authenticated CONNECT and
// verified TLS. Only DNS/address mapping and trust roots are isolated. A direct
// attempt would reach the observable origin; positive controls prove that path.
func TestProviderFilesProductionProxyContract(t *testing.T) {
	for _, profile := range []string{"deepseek-apikey", "deepseek-oauth", "minimax-apikey"} {
		operations := []string{"upload"}
		if profile != "minimax-apikey" {
			operations = append(operations, "delete")
		}
		for _, operation := range operations {
			for _, failure := range []string{"success", "unbound-direct", "refused", "timeout", "407", "cancelled"} {
				t.Run(profile+"/"+operation+"/"+failure, func(t *testing.T) {
					publicHost := "api.deepseek.com"
					platform := service.PlatformDeepseek
					if profile == "minimax-apikey" {
						publicHost = "api.minimax.io"
						platform = service.PlatformMiniMax
					}
					var directDials, originTCP, originCalls, proxyCalls atomic.Int64
					var wireMu sync.Mutex
					var wireHeaders http.Header
					var wirePath string
					origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						originCalls.Add(1)
						wireMu.Lock()
						wireHeaders = r.Header.Clone()
						wirePath = r.URL.Path
						wireMu.Unlock()
						_, _ = io.Copy(io.Discard, r.Body)
						w.Header().Set("Content-Type", "application/json")
						body := `{"id":"file-api-wire","type":"file"}`
						if r.Method == http.MethodDelete {
							body = `{"id":"file-api-wire","type":"file_deleted"}`
						} else if platform == service.PlatformMiniMax {
							body = `{"file":{"file_id":"wire-video"},"base_resp":{"status_code":0}}`
						}
						_, _ = io.WriteString(w, body)
					}))
					certificate, roots := providerFilesWireCertificate(t, publicHost)
					origin.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
					origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
						if state == http.StateNew {
							originTCP.Add(1)
						}
					}
					origin.StartTLS()
					t.Cleanup(origin.Close)
					var mode atomic.Value
					mode.Store("success")
					if operation == "upload" {
						mode.Store(failure)
					}
					release := make(chan struct{})
					entered := make(chan struct{})
					var once sync.Once
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						proxyCalls.Add(1)
						require.Equal(t, http.MethodConnect, r.Method)
						require.Equal(t, publicHost+":443", r.Host)
						require.Empty(t, r.Header.Get("Authorization"))
						require.Empty(t, r.Header.Get("X-Api-Key"))
						require.NotEmpty(t, r.Header.Get("Proxy-Authorization"))
						if mode.Load().(string) == "407" {
							w.Header().Set("Proxy-Authenticate", `Basic realm="files"`)
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
						if mode.Load().(string) == "timeout" || mode.Load().(string) == "cancelled" {
							once.Do(func() { close(entered) })
							<-release
							return
						}
						client, buffered, err := w.(http.Hijacker).Hijack()
						if err != nil {
							return
						}
						defer func() { _ = client.Close() }()
						remote, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
						if err != nil {
							return
						}
						defer func() { _ = remote.Close() }()
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
					t.Cleanup(func() { close(release) })
					parsed, err := url.Parse(proxy.URL)
					require.NoError(t, err)
					port, err := strconv.Atoi(parsed.Port())
					require.NoError(t, err)
					proxyID := int64(8301)
					account := &service.Account{ID: 8302, Platform: platform, Type: service.AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "private-files-wire-key", "api_protocol": service.APIProtocolAnthropic}, ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port, Username: "private-files-proxy-user", Password: "private-files-proxy-password"}}
					if profile == "deepseek-oauth" {
						account.Type = service.AccountTypeOAuth
						account.Credentials = map[string]any{"access_token": "private-files-wire-token", "oauth_provider": platform, "oauth_region": "cn", "oauth_login_id": "login-wire"}
					}
					if profile == "minimax-apikey" {
						account.Credentials["api_protocol"] = service.APIProtocolChatCompletions
						account.Credentials["base_url"] = "https://api.minimax.io/v1"
					}
					cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowPrivateHosts: true}}}
					upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
					var clients []*http.Client
					for _, route := range []string{account.Proxy.URL(), ""} {
						entry, err := upstream.getClientEntry(route, account.ID, account.Concurrency, service.HTTPUpstreamProfileDefault, false, false)
						require.NoError(t, err)
						transport := observedHTTPBaseTransport(t, entry.client.Transport)
						transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
						dial := transport.DialContext
						transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
							if address == publicHost+":443" {
								directDials.Add(1)
								address = origin.Listener.Addr().String()
							}
							return dial(ctx, network, address)
						}
						clients = append(clients, entry.client)
						t.Cleanup(entry.client.CloseIdleConnections)
					}
					store := &providerFilesWireStore{files: map[string]*service.ProviderFile{}}
					gateway := service.NewOpenAIGatewayService(&providerFilesWireAccountRepo{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
					svc := service.NewProviderFilesService(store, gateway)
					groupID := int64(4)
					key := &service.APIKey{ID: 3, UserID: 2, GroupID: &groupID}
					data := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
					filename, mimeType, purpose := "test.png", "image/png", ""
					if platform == service.PlatformMiniMax {
						data = append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 60)...)
						filename, mimeType, purpose = "test.mp4", "video/mp4", "video_understanding"
					}
					input, err := service.ValidateProviderFileUpload(platform, filename, mimeType, purpose, 0, bytes.NewReader(data))
					require.NoError(t, err)
					var file *service.ProviderFile
					if operation == "delete" {
						file, err = svc.Upload(context.Background(), key, account, input, "setup")
						require.NoError(t, err)
						for _, client := range clients {
							client.CloseIdleConnections()
						}
						mode.Store(failure)
						directDials.Store(0)
						originTCP.Store(0)
						originCalls.Store(0)
						proxyCalls.Store(0)
					}
					if failure == "refused" {
						proxy.Close()
					}
					if failure == "unbound-direct" {
						account.ProxyID = nil
					}
					ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
					defer cancel()
					result := make(chan error, 1)
					go func() {
						if operation == "delete" {
							result <- svc.Delete(ctx, key, platform, file.PublicID)
						} else {
							_, err := svc.Upload(ctx, key, account, input, "wire")
							result <- err
						}
					}()
					if failure == "cancelled" {
						select {
						case <-entered:
							cancel()
						case <-ctx.Done():
							t.Fatal("proxy not entered")
						}
					}
					err = <-result
					if failure == "success" || failure == "unbound-direct" {
						require.NoError(t, err)
						require.EqualValues(t, 1, originCalls.Load())
						require.EqualValues(t, 1, originTCP.Load())
						wireMu.Lock()
						headers, path := wireHeaders.Clone(), wirePath
						wireMu.Unlock()
						if profile == "deepseek-oauth" {
							require.Equal(t, "private-files-wire-token", headers.Get("X-Dsh-Auth-Token"))
							require.Empty(t, headers.Get("Authorization"))
							require.True(t, strings.HasPrefix(path, "/anthropic/v1/files"))
						} else if profile == "deepseek-apikey" {
							require.Equal(t, "private-files-wire-key", headers.Get("X-Api-Key"))
							require.Empty(t, headers.Get("Authorization"))
							require.True(t, strings.HasPrefix(path, "/anthropic/v1/files"))
						} else {
							require.Equal(t, "Bearer private-files-wire-key", headers.Get("Authorization"))
							require.Equal(t, "/v1/files/upload", path)
						}
						require.Empty(t, headers.Get("Proxy-Authorization"))
						require.NotEmpty(t, headers.Get("User-Agent"))
						if failure == "success" {
							require.Zero(t, directDials.Load())
							require.Positive(t, proxyCalls.Load())
						} else {
							require.EqualValues(t, 1, directDials.Load())
							require.Zero(t, proxyCalls.Load())
						}
					} else {
						require.Error(t, err)
						require.Zero(t, directDials.Load())
						require.Zero(t, originTCP.Load())
						require.Zero(t, originCalls.Load())
						require.NotContains(t, err.Error(), "private-files")
					}
				})
			}
		}
	}
}
