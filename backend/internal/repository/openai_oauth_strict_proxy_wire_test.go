//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// Exercise the public account operations and their production OAuth client.
// The reachable destination is a direct-egress tripwire; a failed configured
// proxy must never make either refresh or deletion revocation reach it.
func TestOpenAIAccountOAuthProxyFailuresNeverDialDirect(t *testing.T) {
	for _, operation := range []string{"refresh", "revoke"} {
		for _, route := range []string{"missing", "mismatched", "invalid", "timeout", "refused", "authentication_required", "proxy", "unbound"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				var destinationCalls, destinationConnections, proxyCalls atomic.Int32
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					destinationCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"access_token":"rotated","refresh_token":"rotated-refresh","expires_in":3600}`))
				}))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						destinationConnections.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", destination.URL+"/oauth/token")
				t.Setenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE", destination.URL+"/oauth/revoke")
				svc := service.NewOpenAIOAuthService(nil, NewOpenAIOAuthClient())
				t.Cleanup(svc.Stop)
				// Enrichment is a separate optional request; this scenario owns the
				// refresh/revocation send and must never contact public services.
				svc.SetPrivacyClientFactory(func(string) (*req.Client, error) {
					return nil, errors.New("enrichment disabled in route test")
				})
				proxyID := int64(81)
				account := &service.Account{
					ID: 82, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
					ProxyID:     &proxyID,
					Credentials: map[string]any{"access_token": "old", "refresh_token": "refresh"},
				}
				switch route {
				case "missing":
				case "mismatched":
					account.Proxy = &service.Proxy{ID: 999, Protocol: "http", Host: "127.0.0.1", Port: 80}
				case "invalid":
					account.Proxy = &service.Proxy{ID: proxyID, Protocol: "file", Host: "127.0.0.1", Port: 80}
				case "refused":
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, err)
					account.Proxy = openAIOAuthStrictWireProxy(t, "http://"+listener.Addr().String(), proxyID)
					require.NoError(t, listener.Close())
				case "unbound":
					account.ProxyID = nil
					// Residual hydrated data cannot silently opt an unbound account
					// back into an old proxy.
					account.Proxy = &service.Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 1}
				default:
					release := make(chan struct{})
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						proxyCalls.Add(1)
						if route == "timeout" {
							<-release
							return
						}
						if route == "authentication_required" {
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"access_token":"rotated","refresh_token":"rotated-refresh","expires_in":3600}`))
					}))
					t.Cleanup(proxy.Close)
					t.Cleanup(func() { close(release) })
					account.Proxy = openAIOAuthStrictWireProxy(t, proxy.URL, proxyID)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer cancel()
				var err error
				if operation == "refresh" {
					_, err = svc.RefreshAccountToken(ctx, account)
				} else {
					err = svc.RevokeAccountTokens(ctx, account)
				}
				if route == "proxy" || route == "unbound" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				if route == "unbound" {
					require.Equal(t, int32(1), destinationCalls.Load())
				} else {
					require.Zero(t, destinationCalls.Load(), "a configured proxy cannot fall back to the local route")
					require.Zero(t, destinationConnections.Load(), "a failed proxy cannot open even a direct TCP connection")
				}
				if route == "proxy" || route == "timeout" || route == "authentication_required" {
					require.Positive(t, proxyCalls.Load(), "the configured proxy must own the network attempt")
				} else {
					require.Zero(t, proxyCalls.Load())
				}
			})
		}
	}
}

func openAIOAuthStrictWireProxy(t *testing.T, address string, id int64) *service.Proxy {
	t.Helper()
	u, err := url.Parse(address)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &service.Proxy{ID: id, Protocol: u.Scheme, Host: u.Hostname(), Port: port, Status: service.StatusActive}
}
