//go:build unit || !integration

package proxyutil

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/stretchr/testify/require"
)

// A failed SOCKS handshake is an account network failure, never permission to
// retry using the forward dialer's direct destination connection.
func TestSOCKSProxyFailureNeverDialsDestination(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	for _, scheme := range []string{"socks5", "socks5h"} {
		for _, failure := range []string{"refused", "timeout", "authentication", "destination-denied"} {
			t.Run(scheme+"/"+failure, func(t *testing.T) {
				var destinationConnections atomic.Int64
				target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
				target.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						destinationConnections.Add(1)
					}
				}
				target.Start()
				t.Cleanup(target.Close)
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				entered := make(chan struct{}, 2)
				release := make(chan struct{})
				var serving sync.WaitGroup
				serving.Add(1)
				go func() {
					defer serving.Done()
					for {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						func() {
							defer func() { _ = conn.Close() }()
							_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
							entered <- struct{}{}
							greeting := make([]byte, 3)
							if _, err := io.ReadFull(conn, greeting); err != nil {
								return
							}
							if failure == "timeout" {
								<-release
								return
							}
							if failure == "authentication" {
								_, _ = conn.Write([]byte{5, 255})
								return
							}
							if _, err := conn.Write([]byte{5, 0}); err != nil {
								return
							}
							request := make([]byte, 10) // numeric loopback destination
							if _, err := io.ReadFull(conn, request); err != nil {
								return
							}
							_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
						}()
					}
				}()
				t.Cleanup(func() { close(release); _ = listener.Close(); serving.Wait() })
				if failure == "refused" {
					require.NoError(t, listener.Close())
				}
				_, parsed, err := proxyurl.Parse(scheme + "://" + listener.Addr().String())
				require.NoError(t, err)
				transport := &http.Transport{}
				require.NoError(t, ConfigureTransportProxy(transport, parsed))
				t.Cleanup(transport.CloseIdleConnections)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				if failure == "timeout" {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				}
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
				require.NoError(t, err)
				resp, err := (&http.Client{Transport: transport}).Do(req)
				if resp != nil {
					_ = resp.Body.Close()
				}
				require.Error(t, err)
				if failure != "refused" {
					select {
					case <-entered:
					default:
						t.Fatal("request must reach the configured SOCKS proxy")
					}
				}
				require.Zero(t, destinationConnections.Load(), "a SOCKS proxy error must never cause direct egress")
			})
		}
	}
}
