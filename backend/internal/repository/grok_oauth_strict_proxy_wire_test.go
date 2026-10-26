//go:build unit

package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type grokOAuthReplacementProxyRepo struct {
	service.ProxyRepository
	proxy *service.Proxy
	err   error
	calls atomic.Int32
}

func (r *grokOAuthReplacementProxyRepo) GetByID(context.Context, int64) (*service.Proxy, error) {
	r.calls.Add(1)
	return r.proxy, r.err
}

// A durable proxy edit between account acquisition and refresh must not pair
// the selected account's old refresh token with the replacement proxy.
func TestGrokAccountRefreshNeverMixesOldTokenWithNewProxy(t *testing.T) {
	for _, replacement := range []string{"edited", "deleted", "lookup_failed"} {
		t.Run(replacement, func(t *testing.T) {
			var oldCalls, newCalls, directConnections atomic.Int32
			destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
			destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					directConnections.Add(1)
				}
			}
			destination.Start()
			t.Cleanup(destination.Close)
			captured := make(chan string, 1)
			oldProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				oldCalls.Add(1)
				if err := r.ParseForm(); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				captured <- r.Form.Get("refresh_token")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"rotated","refresh_token":"rotated-refresh","expires_in":3600}`))
			}))
			t.Cleanup(oldProxy.Close)
			newProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				newCalls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(newProxy.Close)
			proxy := openAIOAuthStrictWireProxy(t, oldProxy.URL, 111)
			account := &service.Account{ID: 112, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, ProxyID: &proxy.ID, Proxy: proxy,
				Credentials: map[string]any{"access_token": "old-access", "refresh_token": "old-refresh", "subscription_tier": "paid"}}
			repo := &grokOAuthReplacementProxyRepo{}
			switch replacement {
			case "edited":
				repo.proxy = openAIOAuthStrictWireProxy(t, newProxy.URL, proxy.ID)
			case "lookup_failed":
				repo.err = errors.New("proxy lookup unavailable")
			}
			svc := service.NewGrokOAuthService(repo, &grokOAuthClient{tokenURL: destination.URL + "/oauth2/token"})
			t.Cleanup(svc.Stop)
			info, err := svc.RefreshAccountToken(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, "rotated", info.AccessToken)
			require.Equal(t, "old-refresh", <-captured)
			require.Equal(t, int32(1), oldCalls.Load())
			require.Zero(t, newCalls.Load())
			require.Zero(t, directConnections.Load())
			require.Zero(t, repo.calls.Load(), "refresh cannot replace an acquired network snapshot through a new proxy lookup")
		})
	}
}

func TestGrokAccountOAuthRefreshProxyFailuresNeverDialDirect(t *testing.T) {
	for _, route := range []string{"missing", "mismatched", "invalid", "timeout", "refused", "authentication_required"} {
		t.Run(route, func(t *testing.T) {
			var directConnections, proxyCalls atomic.Int32
			destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					directConnections.Add(1)
				}
			}
			destination.Start()
			t.Cleanup(destination.Close)
			proxyID := int64(113)
			account := &service.Account{ID: 114, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, ProxyID: &proxyID,
				Credentials: map[string]any{"access_token": "old-access", "refresh_token": "old-refresh"}}
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
			default:
				release := make(chan struct{})
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					proxyCalls.Add(1)
					if route == "timeout" {
						<-release
						return
					}
					w.WriteHeader(http.StatusProxyAuthRequired)
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { close(release) })
				account.Proxy = openAIOAuthStrictWireProxy(t, proxy.URL, proxyID)
			}
			svc := service.NewGrokOAuthService(nil, &grokOAuthClient{tokenURL: destination.URL + "/oauth2/token"})
			t.Cleanup(svc.Stop)
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			_, err := svc.RefreshAccountToken(ctx, account)
			require.Error(t, err)
			require.Zero(t, directConnections.Load())
			if route == "timeout" || route == "authentication_required" {
				require.Positive(t, proxyCalls.Load())
			}
		})
	}
}
