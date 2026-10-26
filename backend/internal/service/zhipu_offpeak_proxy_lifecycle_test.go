//go:build unit

package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyutil"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

// This adapter uses the production ticket protocol over a real net/http proxy
// transport. Only the loopback test CA and a short test timeout are supplied.
type offPeakLifecycleNativeClient struct {
	roots       *x509.CertPool
	origin      string
	settleSends *atomic.Int32
}

func (c offPeakLifecycleNativeClient) client(proxy string) (*zcode.OffPeakClient, func(), error) {
	_, parsed, err := proxyurl.Parse(proxy)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.roots}}
	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, nil, err
	}
	return zcode.NewOffPeakClient(&http.Client{Transport: transport, Timeout: 100 * time.Millisecond}, c.origin), transport.CloseIdleConnections, nil
}

func (c offPeakLifecycleNativeClient) Availability(ctx context.Context, auth zcode.OffPeakAuth, proxy string) (*zcode.OffPeakAvailability, error) {
	client, closeIdle, err := c.client(proxy)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	return client.Availability(ctx, auth)
}

func (c offPeakLifecycleNativeClient) TakeTicket(ctx context.Context, task string, auth zcode.OffPeakAuth, proxy string) (*zcode.OffPeakTicket, error) {
	client, closeIdle, err := c.client(proxy)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	return client.TakeTicket(ctx, task, auth)
}

func (c offPeakLifecycleNativeClient) TicketStatus(ctx context.Context, tickets []string, auth zcode.OffPeakAuth, proxy string) ([]zcode.OffPeakTicket, time.Duration, error) {
	client, closeIdle, err := c.client(proxy)
	if err != nil {
		return nil, 0, err
	}
	defer closeIdle()
	return client.TicketStatus(ctx, tickets, auth)
}

func (c offPeakLifecycleNativeClient) SettleTicket(ctx context.Context, ticket string, auth zcode.OffPeakAuth, proxy string) error {
	c.settleSends.Add(1)
	client, closeIdle, err := c.client(proxy)
	if err != nil {
		return err
	}
	defer closeIdle()
	return client.SettleTicket(ctx, ticket, auth)
}

// A CONNECT proxy synthesizes the TLS origin within its tunnel. The separate
// reachable destination counts direct TCP dials and is never needed by proxy
// traffic. This distinguishes fail-closed behavior from a direct TLS failure.
func TestZhipuOffPeakProxySettlementFailureRecoveryRetiresExactlyOnce(t *testing.T) {
	for _, failure := range []string{"timeout", "refused", "authentication_required", "upstream_unavailable"} {
		t.Run(failure, func(t *testing.T) {
			var directTCP, proxyAttempts, settleSends atomic.Int32
			var failing atomic.Bool
			var mu sync.Mutex
			var settledHeaders []http.Header
			destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
			destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					directTCP.Add(1)
				}
			}
			destination.StartTLS()
			t.Cleanup(destination.Close)
			roots := x509.NewCertPool()
			roots.AddCert(destination.Certificate())
			tlsConfig := destination.TLS.Clone()
			release := make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyAttempts.Add(1)
				if r.Method != http.MethodConnect {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if failing.Load() {
					switch failure {
					case "timeout":
						<-release
						return
					case "authentication_required":
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					case "upstream_unavailable":
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
				}
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Error("proxy must support CONNECT hijacking")
					return
				}
				conn, buffered, err := hijacker.Hijack()
				if err != nil {
					t.Errorf("proxy hijack: %v", err)
					return
				}
				defer conn.Close()
				if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					t.Errorf("CONNECT ack: %v", err)
					return
				}
				if err = buffered.Flush(); err != nil {
					t.Errorf("CONNECT flush: %v", err)
					return
				}
				secure := tls.Server(conn, tlsConfig)
				if err = secure.Handshake(); err != nil {
					t.Errorf("proxy TLS handshake: %v", err)
					return
				}
				request, err := http.ReadRequest(bufio.NewReader(secure))
				if err != nil {
					t.Errorf("proxy origin request: %v", err)
					return
				}
				_, _ = io.Copy(io.Discard, request.Body)
				_ = request.Body.Close()
				response := &http.Response{ProtoMajor: 1, ProtoMinor: 1, StatusCode: http.StatusOK, Header: make(http.Header), Close: true}
				response.Header.Set("Connection", "close")
				if strings.HasSuffix(request.URL.Path, "/settle") {
					mu.Lock()
					settledHeaders = append(settledHeaders, request.Header.Clone())
					mu.Unlock()
					// Official request() and settleResponseSchema accept an empty 204.
					response.StatusCode = http.StatusNoContent
					response.Body = http.NoBody
				} else {
					body := `{"code":0,"data":{"ticket_id":"retained-ticket","state":"ready"}}`
					response.Body = io.NopCloser(strings.NewReader(body))
					response.ContentLength = int64(len(body))
				}
				if err = response.Write(secure); err != nil {
					t.Errorf("proxy origin response: %v", err)
				}
			})
			proxy := httptest.NewServer(handler)
			t.Cleanup(proxy.Close)
			t.Cleanup(func() { close(release) })
			_, parsed, err := proxyurl.Parse(proxy.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(parsed.Port())
			require.NoError(t, err)
			account := zhipuOffPeakTestAccount()
			proxyID := int64(516)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port}
			manager := NewZhipuOffPeakTicketManager(offPeakLifecycleNativeClient{roots: roots, origin: destination.URL, settleSends: &settleSends}).WithSettleIdle(time.Second)
			manager.Stop()
			manager.projectTokens = NewZhipuProjectTokenManager(&projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
				return &zcode.ProjectAccessToken{Token: "ak.sk", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}}, nil)
			now := time.Now()
			manager.now = func() time.Time { return now }
			ticket, err := manager.Acquire(context.Background(), account, zhipuOffPeakTestAuth(), account.Proxy.URL())
			require.NoError(t, err)
			require.Equal(t, "retained-ticket", ticket.TicketID)
			ticket.Release()
			require.EqualValues(t, 1, proxyAttempts.Load())
			now = now.Add(2 * time.Second)
			failing.Store(true)
			if failure == "refused" {
				proxy.Close()
			}
			manager.sweepOnce()
			holder := manager.holderFor(account.ID)
			require.EqualValues(t, 1, settleSends.Load(), "the failed settlement must actually have been attempted")
			if failure != "refused" {
				require.EqualValues(t, 2, proxyAttempts.Load(), "the failed settlement must reach the captured proxy")
			}
			require.Equal(t, "retained-ticket", holder.ticketID)
			require.NotEmpty(t, holder.taskID)
			require.Zero(t, directTCP.Load())
			// A mutation after acquisition must not transfer old settlement ownership.
			account.Credentials["zcode_jwt_token"] = "replacement-jwt"
			account.Proxy.Host = "replacement.invalid"
			failing.Store(false)
			if failure == "refused" {
				listener, listenErr := net.Listen("tcp", parsed.Host)
				require.NoError(t, listenErr)
				recovered := httptest.NewUnstartedServer(handler)
				require.NoError(t, recovered.Listener.Close())
				recovered.Listener = listener
				recovered.Start()
				t.Cleanup(recovered.Close)
			}
			manager.sweepOnce()
			require.EqualValues(t, 2, settleSends.Load(), "recovery must actually resend the same ticket through its original proxy")
			attemptsAfterSuccess := proxyAttempts.Load()
			require.Empty(t, holder.ticketID, "an empty successful acknowledgement retires the outstanding ticket")
			require.Empty(t, holder.taskID)
			require.Equal(t, zcode.TicketSettled, holder.state)
			mu.Lock()
			headers := append([]http.Header(nil), settledHeaders...)
			mu.Unlock()
			require.Len(t, headers, 1)
			require.Equal(t, "Bearer zcode-jwt", headers[0].Get("Authorization"))
			require.Equal(t, "ak.sk", headers[0].Get("X-Coding-Plan-Api-Key"))
			require.Equal(t, "ZCode/3.16.2", headers[0].Get("User-Agent"))
			manager.sweepOnce()
			require.EqualValues(t, 2, settleSends.Load(), "successful retirement cannot trigger duplicate settlement")
			require.Equal(t, attemptsAfterSuccess, proxyAttempts.Load())
			require.Zero(t, directTCP.Load())
		})
	}
}
