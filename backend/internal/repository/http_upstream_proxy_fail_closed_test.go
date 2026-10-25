//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// Business requirement: a bound account's proxy failure must never authorize
// local egress. Count accepted destination TCP connections, including failed TLS
// handshakes, rather than merely checking the helper's chosen proxy string.
func TestHTTPUpstreamProxyFailureNeverDialsDestination(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	var envProxyConnections atomic.Int64
	envProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		envProxyConnections.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(envProxy.Close)
	t.Setenv("HTTP_PROXY", envProxy.URL)
	t.Setenv("HTTPS_PROXY", envProxy.URL)
	for _, secure := range []bool{false, true} {
		for _, failure := range []string{"refused", "timeout", "authentication"} {
			for _, fingerprint := range []bool{false, true} {
				t.Run(fmt.Sprintf("tls=%t/%s/fingerprint=%t", secure, failure, fingerprint), func(t *testing.T) {
					var destinationConnections atomic.Int64
					target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
					target.Config.ConnState = func(_ net.Conn, state http.ConnState) {
						if state == http.StateNew {
							destinationConnections.Add(1)
						}
					}
					if secure {
						target.StartTLS()
					} else {
						target.Start()
					}
					t.Cleanup(target.Close)
					var proxyConnections atomic.Int64
					release := make(chan struct{})
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						proxyConnections.Add(1)
						if failure == "timeout" {
							<-release
							return
						}
						w.WriteHeader(http.StatusProxyAuthRequired)
					}))
					t.Cleanup(proxy.Close)
					t.Cleanup(func() { close(release) })
					proxyURL := proxy.URL
					if failure == "refused" {
						proxy.Close()
					}
					upstream := NewHTTPUpstream(nil)
					for attempt := 0; attempt < 2; attempt++ {
						ctx, cancel := context.WithTimeout(t.Context(), time.Second)
						if failure == "timeout" {
							cancel()
							ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
						}
						req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL+"/provider-request", nil)
						require.NoError(t, err)
						var resp *http.Response
						if fingerprint {
							resp, err = upstream.DoWithTLS(req, proxyURL, 91, 1, &tlsfingerprint.Profile{Name: "strict-account-proxy"})
						} else {
							resp, err = upstream.Do(req, proxyURL, 91, 1)
						}
						cancel()
						if resp != nil {
							require.NoError(t, resp.Body.Close())
						}
						if failure == "authentication" && !secure {
							require.NoError(t, err)
							require.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
						} else {
							require.Error(t, err)
						}
						if failure == "timeout" {
							require.True(t, errors.Is(err, context.DeadlineExceeded), "proxy timeout must reach the caller")
							require.Positive(t, proxyConnections.Load(), "timeout must occur at the configured proxy; a subsequent request may await its existing CONNECT dial")
						}
						require.Zero(t, destinationConnections.Load(), "failed account proxy must never fall back to local destination dialing")
					}
				})
			}
		}
	}
	require.Zero(t, envProxyConnections.Load(), "explicit account proxy must not fall back to an environment proxy")
}
