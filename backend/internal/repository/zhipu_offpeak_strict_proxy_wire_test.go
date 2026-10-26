//go:build unit

package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func zhipuStrictWireDestination(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var directTCP atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			directTCP.Add(1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Setenv(zcode.EnvOffPeakBaseURL, server.URL)
	return server, &directTCP
}

func zhipuStrictWireProxy(t *testing.T, route string) (string, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	if route == "refused" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		address := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())
		return address, &attempts
	}
	if route == "malformed" {
		return "http://proxy-user-secret:proxy-password-secret@%zz", &attempts
	}
	release := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if r.Method != http.MethodConnect {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if route == "timeout" {
			<-release
			return
		}
		w.Header().Set("Proxy-Authenticate", `Basic realm="ticket-proxy"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(proxy.Close)
	t.Cleanup(func() { close(release) })
	_, parsed, err := proxyurl.Parse(proxy.URL)
	require.NoError(t, err)
	parsed.User = nil
	return parsed.Scheme + "://proxy-user-secret:proxy-password-secret@" + parsed.Host, &attempts
}

// Exercise the repository production client rather than an injected HTTPDoer.
// A reachable TLS destination detects even a failed direct TCP fallback.
func TestZhipuOffPeakProductionClientProxyFailuresNeverDialDirect(t *testing.T) {
	for _, operation := range []string{"take", "status", "settle"} {
		for _, route := range []string{"timeout", "refused", "authentication_required", "malformed"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				_, directTCP := zhipuStrictWireDestination(t)
				proxy, attempts := zhipuStrictWireProxy(t, route)
				client := NewZhipuOAuthClient()
				identity := zcode.DefaultIdentity()
				identity.ControlHeaders[zcode.HeaderDeviceMid] = "7d918214-79ad-4057-bb2b-4ac6bd24a907"
				ctx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), identity), 150*time.Millisecond)
				defer cancel()
				auth := zcode.OffPeakAuth{JWT: "owner-jwt-secret", PlanKey: "owner-pat-secret"}
				var err error
				switch operation {
				case "take":
					_, err = client.TakeTicket(ctx, "task-1", auth, proxy)
				case "status":
					_, _, err = client.TicketStatus(ctx, []string{"ticket-1"}, auth, proxy)
				case "settle":
					err = client.SettleTicket(ctx, "ticket-1", auth, proxy)
				}
				require.Error(t, err)
				require.Zero(t, directTCP.Load(), "a configured proxy failure cannot authorize even a direct TCP connection")
				for _, secret := range []string{"owner-jwt-secret", "owner-pat-secret", "proxy-user-secret", "proxy-password-secret"} {
					require.NotContains(t, err.Error(), secret)
				}
				if route == "timeout" {
					require.True(t, errors.Is(err, context.DeadlineExceeded), "the request deadline must bound the configured proxy wait")
					require.Positive(t, attempts.Load())
				} else if route == "authentication_required" {
					require.Positive(t, attempts.Load(), "407 must originate at the configured proxy")
				}
			})
		}
	}
}

func TestZhipuOffPeakProductionAcquisitionRejectsBrokenProxyBindings(t *testing.T) {
	for _, route := range []string{"missing", "mismatched", "malformed"} {
		t.Run(route, func(t *testing.T) {
			_, directTCP := zhipuStrictWireDestination(t)
			proxyURL, proxyAttempts := zhipuStrictWireProxy(t, "authentication_required")
			_, parsed, err := proxyurl.Parse(proxyURL)
			require.NoError(t, err)
			port, err := strconv.Atoi(parsed.Port())
			require.NoError(t, err)
			proxyID := int64(416)
			account := &service.Account{ID: 417, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth,
				ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port},
				Credentials: map[string]any{"plan_kind": service.ZhipuPlanOffPeak, "zcode_jwt_token": "owner-jwt-secret"}}
			switch route {
			case "missing":
				account.Proxy = nil
				proxyURL = ""
			case "mismatched":
				account.Proxy.ID++
			case "malformed":
				account.Proxy.Protocol = "file"
				proxyURL = account.Proxy.URL()
			}
			manager := service.NewZhipuOffPeakTicketManager(NewZhipuOAuthClient())
			manager.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, err = manager.Acquire(ctx, account, zcode.OffPeakAuth{JWT: "owner-jwt-secret", PlanKey: "owner-pat-secret"}, proxyURL)
			require.Error(t, err)
			require.Zero(t, directTCP.Load())
			require.Zero(t, proxyAttempts.Load(), "an invalid owner binding must fail before any proxy request")
			require.NotContains(t, err.Error(), "owner-jwt-secret")
			require.NotContains(t, err.Error(), "owner-pat-secret")
		})
	}
}
