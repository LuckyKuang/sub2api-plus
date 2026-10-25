//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The WS sender shares the same strict egress requirement as HTTP: a proxy
// failure cannot fall through to coder/websocket's default direct transport.
func TestWebSocketProxyFailureNeverDialsDestination(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	for _, secure := range []bool{false, true} {
		for _, failure := range []string{"refused", "timeout", "authentication"} {
			t.Run(fmt.Sprintf("tls=%t/%s", secure, failure), func(t *testing.T) {
				var destinationConnections atomic.Int64
				target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
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
				entered := make(chan struct{}, 2)
				release := make(chan struct{})
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					entered <- struct{}{}
					if failure == "timeout" {
						<-release
						return
					}
					w.WriteHeader(http.StatusProxyAuthRequired)
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { close(release) })
				if failure == "refused" {
					proxy.Close()
				}
				dialer := newDefaultOpenAIWSClientDialer()
				proxyDialer, ok := dialer.(*coderOpenAIWSClientDialer)
				require.True(t, ok)
				t.Cleanup(func() {
					for _, entry := range proxyDialer.proxyClients {
						closeOpenAIWSProxyClient(entry.client)
					}
				})
				for attempt := 0; attempt < 2; attempt++ {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					if failure == "timeout" {
						cancel()
						ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
					}
					conn, _, _, err := dialer.Dial(ctx, "ws"+strings.TrimPrefix(target.URL, "http"), nil, proxy.URL)
					cancel()
					if conn != nil {
						_ = conn.Close()
					}
					require.Error(t, err)
					if failure == "timeout" {
						select {
						case <-entered:
						default:
							t.Fatal("timeout must occur after reaching the configured proxy")
						}
					}
					require.Zero(t, destinationConnections.Load(), "WS proxy failure must never authorize a direct TCP handshake")
				}
			})
		}
	}
}
