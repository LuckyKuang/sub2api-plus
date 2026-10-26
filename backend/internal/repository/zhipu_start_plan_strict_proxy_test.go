//go:build unit

package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type startPlanWireAccountRepo struct {
	service.AccountRepository
	writes atomic.Int32
}

func (r *startPlanWireAccountRepo) UpdateCNControlExtraIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error) {
	r.writes.Add(1)
	return true, nil
}

// The public observation service uses the real production HTTPUpstream and its
// transports. Only DNS/TLS trust is replaced to route the fixed official host
// to a reachable local TLS tripwire. Failure cases must never dial it directly.
func TestZhipuStartPlanProductionBalanceStrictProxy(t *testing.T) {
	for _, mode := range []string{"success", "timeout", "refused", "authentication_required", "missing", "mismatched"} {
		t.Run(mode, func(t *testing.T) {
			var directDials, proxyCalls, providerCalls atomic.Int32
			destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				providerCalls.Add(1)
				require.Equal(t, "/api/v1/zcode-plan/billing/balance?app_version=3.16.2", r.URL.RequestURI())
				require.Equal(t, "Bearer start-wire-jwt-secret", r.Header.Get("Authorization"))
				require.Equal(t, "ZCode/3.16.2", r.UserAgent())
				require.Empty(t, r.Header.Get("X-Coding-Plan-Api-Key"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[{"capabilities":["model:GLM-5.3"]}]}}`)
			}))
			t.Cleanup(destination.Close)
			release := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyCalls.Add(1)
				require.Equal(t, http.MethodConnect, r.Method)
				require.Equal(t, "zcode.z.ai:443", r.Host)
				if mode == "timeout" {
					<-release
					return
				}
				if mode != "success" {
					w.WriteHeader(http.StatusProxyAuthRequired)
					return
				}
				target, err := net.Dial("tcp", destination.Listener.Addr().String())
				if err != nil {
					w.WriteHeader(502)
					return
				}
				client, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					_ = target.Close()
					return
				}
				_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { _, _ = io.Copy(target, client); _ = target.Close(); _ = client.Close() }()
				go func() { _, _ = io.Copy(client, target); _ = client.Close(); _ = target.Close() }()
			}))
			t.Cleanup(proxy.Close)
			t.Cleanup(func() { close(release) })
			parsed, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(parsed.Port())
			require.NoError(t, err)
			proxyID := int64(301)
			a := &service.Account{ID: 302, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth, Status: service.StatusActive, Concurrency: 1,
				Credentials: map[string]any{"oauth_provider": "zai", "plan_kind": service.ZhipuPlanStartPlan, "account_mode": service.AccountModeCoding, "zcode_jwt_token": "start-wire-jwt-secret",
					service.SettingKeyOutboundIdentity: service.OutboundIdentitySelection{Preset: "zcode", Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"}}},
				ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port, Status: service.StatusActive}}
			if mode == "refused" {
				proxy.Close()
			}
			if mode == "missing" {
				a.Proxy = nil
			}
			if mode == "mismatched" {
				a.Proxy.ID++
			}
			cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowPrivateHosts: true}}}
			upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
			// Prewarm both routes so even an erroneous direct fallback goes through
			// the same deterministic DNS tripwire. Keep the production Proxy choice.
			for _, route := range []string{proxy.URL, ""} {
				entry, err := upstream.getOrCreateClient(route, a.ID, 1)
				require.NoError(t, err)
				transport := entry.client.Transport.(interface{ Unwrap() http.RoundTripper }).Unwrap().(*http.Transport)
				originalDial := transport.DialContext
				transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
					if address == "zcode.z.ai:443" {
						directDials.Add(1)
						address = destination.Listener.Addr().String()
					}
					return originalDial(ctx, network, address)
				}
				pool := x509.NewCertPool()
				pool.AddCert(destination.Certificate())
				transport.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: destination.Certificate().DNSNames[0], MinVersion: tls.VersionTLS12}
				entry.client.Timeout = 150 * time.Millisecond
				t.Cleanup(entry.client.CloseIdleConnections)
			}
			repo := &startPlanWireAccountRepo{}
			s := service.NewCNProviderQuotaService(repo, nil, upstream, cfg)
			result, err := s.QueryUsageForAccount(context.Background(), a)
			if mode == "missing" || mode == "mismatched" {
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, proxyCalls.Load())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, directDials.Load())
			if mode == "success" {
				require.True(t, result.Success)
				require.True(t, result.Persisted)
				require.Equal(t, "available", result.StartPlan.State)
				require.EqualValues(t, 1, providerCalls.Load())
				require.EqualValues(t, 1, repo.writes.Load())
			} else {
				require.False(t, result.Success)
				require.False(t, result.Persisted)
				require.Equal(t, "unknown", result.StartPlan.State)
				require.Zero(t, providerCalls.Load())
				require.Zero(t, repo.writes.Load())
				for _, secret := range []string{"start-wire-jwt-secret", "proxy-password"} {
					require.NotContains(t, result.Error, secret)
				}
			}
			if mode == "timeout" || mode == "authentication_required" || mode == "success" {
				require.Positive(t, proxyCalls.Load())
			}
			// Never pass the proxy's response body through as a provider result.
			require.False(t, strings.Contains(result.Error, "Proxy-Authenticate"))
		})
	}
}
