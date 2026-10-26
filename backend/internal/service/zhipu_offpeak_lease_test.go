//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func offPeakLeaseClock(manager *ZhipuOffPeakTicketManager) func(time.Duration) {
	var elapsed atomic.Int64
	manager.now = func() time.Time { return time.Unix(1_700_000_000, elapsed.Load()) }
	return func(d time.Duration) { elapsed.Add(int64(d)) }
}

type offPeakLeaseUpstream struct {
	httpUpstreamRecorder
	send func(*http.Request) (*http.Response, error)
}

func (u *offPeakLeaseUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.send(req)
}

func (u *offPeakLeaseUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestZhipuOffPeakLeaseProtectsBlockedSendUntilCancellationReturns(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "sending", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)
	advance := offPeakLeaseClock(manager)
	installZhipuOffPeakTestProvider(t, manager)
	entered := make(chan struct{})
	upstream := &offPeakLeaseUpstream{send: func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://zcode.z.ai/api/v1/off-peak/anthropic/v1/messages", strings.NewReader(`{"model":"glm-5.3","messages":[]}`))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := (&OpenAIGatewayService{httpUpstream: upstream, projectTokens: manager.projectTokens}).doOpenAIUpstream(req, "", zhipuOffPeakTestAccount())
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("model send did not start: %v", err)
	case <-time.After(time.Second):
		t.Fatal("model send did not start")
	}
	advance(3 * DefaultOffPeakSettleIdle)
	manager.sweepOnce()
	require.Empty(t, client.settledTickets(), "waiting for response headers still uses the ticket")
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled send did not finish")
	}
	advance(DefaultOffPeakSettleIdle + time.Second)
	manager.sweepOnce()
	require.Equal(t, []string{"sending"}, client.settledTickets())
}

func TestZhipuOffPeakForwardAndProbeLeaseLastsThroughEveryLiveResponse(t *testing.T) {
	for _, probe := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "probe"}[probe], func(t *testing.T) {
			client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "live", State: zcode.TicketReady}}}
			manager := newZhipuOffPeakTestManager(t, client)
			advance := offPeakLeaseClock(manager)
			installZhipuOffPeakTestProvider(t, manager)
			var writers []*io.PipeWriter
			upstream := &offPeakLeaseUpstream{send: func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "live", req.Header.Get("X-Off-Peak-Ticket-ID"))
				r, w := io.Pipe()
				writers = append(writers, w)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: r}, nil
			}}
			oauth := manager.projectTokens
			forward := func() *http.Response {
				req, err := http.NewRequest(http.MethodPost, "https://zcode.z.ai/api/v1/off-peak/anthropic/v1/messages", strings.NewReader(`{"model":"glm-5.3","messages":[],"stream":true}`))
				require.NoError(t, err)
				var response *http.Response
				if probe {
					response, err = (&AccountTestService{httpUpstream: upstream, projectTokens: oauth}).doOpenAIAccountTestUpstream(req, "", zhipuOffPeakTestAccount(), true)
				} else {
					response, err = (&OpenAIGatewayService{httpUpstream: upstream, projectTokens: oauth}).doOpenAIUpstream(req, "", zhipuOffPeakTestAccount())
				}
				require.NoError(t, err)
				t.Cleanup(func() { _ = response.Body.Close() })
				return response
			}
			first, second := forward(), forward()
			advance(3 * DefaultOffPeakSettleIdle)
			manager.sweepOnce()
			require.Empty(t, client.settledTickets(), "open responses must not be considered idle")
			require.NoError(t, writers[0].Close())
			_, err := io.ReadAll(first.Body)
			require.NoError(t, err)
			require.NoError(t, first.Body.Close())
			require.NoError(t, first.Body.Close())
			advance(3 * DefaultOffPeakSettleIdle)
			manager.sweepOnce()
			require.Empty(t, client.settledTickets(), "EOF and repeated Close cannot release the other response")
			require.NoError(t, second.Body.Close())
			require.NoError(t, writers[1].Close())
			manager.sweepOnce()
			require.Empty(t, client.settledTickets(), "idle time starts when the last response completes")
			advance(DefaultOffPeakSettleIdle + time.Second)
			manager.sweepOnce()
			require.Equal(t, []string{"live"}, client.settledTickets())
		})
	}
}

type offPeakFailingBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *offPeakFailingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b *offPeakFailingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return io.ErrClosedPipe
}

func TestZhipuOffPeakLeaseEndsOnSendErrorReadErrorCloseAndCancellation(t *testing.T) {
	for _, outcome := range []string{"send error", "no body", "read error", "close error", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "done", State: zcode.TicketReady}}}
			manager := newZhipuOffPeakTestManager(t, client)
			advance := offPeakLeaseClock(manager)
			ticket, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &offPeakFailingBody{closed: make(chan struct{})}
			response := &http.Response{StatusCode: 200, Body: body}
			var sendErr error
			switch outcome {
			case "send error":
				sendErr = errors.New("transport failed")
			case "no body":
				response.Body = nil
			}
			holdZhipuOffPeakResponse(ctx, ticket, response, sendErr)
			switch outcome {
			case "read error":
				_, err = response.Body.Read(make([]byte, 1))
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			case "close error":
				require.ErrorIs(t, response.Body.Close(), io.ErrClosedPipe)
			case "cancel":
				cancel()
				select {
				case <-body.closed:
				case <-time.After(time.Second):
					t.Fatal("cancellation did not close the model response")
				}
			}
			require.Eventually(t, func() bool {
				holder := manager.holderFor(77)
				holder.leaseMu.Lock()
				defer holder.leaseMu.Unlock()
				return holder.active == 0
			}, time.Second, time.Millisecond)
			advance(DefaultOffPeakSettleIdle + time.Second)
			manager.sweepOnce()
			require.Equal(t, []string{"done"}, client.settledTickets())
		})
	}
}

func TestZhipuOffPeakCredentialRotationDrainsOnlyItsOriginalOwner(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "old", State: zcode.TicketReady}, {TicketID: "new", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)
	manager.projectTokens = nil
	advance := offPeakLeaseClock(manager)
	oldAccount := zhipuOffPeakTestAccount()
	oldAccount.Type = AccountTypeAPIKey
	oldProxyID := int64(11)
	oldAccount.ProxyID = &oldProxyID
	oldAccount.Proxy = &Proxy{ID: oldProxyID, Protocol: "https", Host: "old-proxy.test", Port: 443}
	oldAccount.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.2.0"}
	oldAuth := zhipuOffPeakTestAuth()
	old, err := manager.Acquire(context.Background(), oldAccount, oldAuth, "https://old-proxy.test:443")
	require.NoError(t, err)
	newAccount := snapshotRequestCredentialAccount(oldAccount)
	newAccount.Credentials["access_token"] = "new-login"
	newAccount.Proxy.Host = "new-proxy.test"
	newAccount.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.4.0"}
	newAuth := zcode.OffPeakAuth{JWT: "new-plan-jwt", PlanKey: "new-pat"}
	current, err := manager.Acquire(context.Background(), newAccount, newAuth, "https://new-proxy.test:443")
	require.NoError(t, err)
	require.Equal(t, "new", current.TicketID)
	advance(3 * DefaultOffPeakSettleIdle)
	manager.sweepOnce()
	require.Empty(t, client.settledTickets(), "credential rotation cannot settle either live generation")
	old.Release()
	manager.sweepOnce()
	require.Equal(t, []string{"old"}, client.settledTickets())
	require.Equal(t, []zcode.OffPeakAuth{oldAuth}, client.settleAuths)
	require.Equal(t, []string{"https://old-proxy.test:443"}, client.settleProxies)
	require.Equal(t, "4.2.0", client.identities[2].Version)
	old.Release()
	advance(3 * DefaultOffPeakSettleIdle)
	manager.sweepOnce()
	require.Equal(t, []string{"old"}, client.settledTickets(), "old completion cannot release the replacement generation")
	current.Release()
	advance(DefaultOffPeakSettleIdle + time.Second)
	manager.sweepOnce()
	require.Equal(t, []string{"old", "new"}, client.settledTickets())
	require.Equal(t, newAuth, client.settleAuths[1])
	require.Equal(t, []string{"https://old-proxy.test:443", "https://new-proxy.test:443"}, client.settleProxies)
	require.Equal(t, "4.4.0", client.identities[3].Version)
}

func TestZhipuOffPeak401ControlRetryUsesNewRequestIDAndOwnerPAT(t *testing.T) {
	var ids []string
	var keys []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.Header.Get("x-request-id"))
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), id.Version())
		ids = append(ids, id.String())
		keys = append(keys, r.Header.Get("X-Coding-Plan-Api-Key"))
		require.Equal(t, "Bearer plan-jwt", r.Header.Get("Authorization"))
		require.Equal(t, "ZCode/3.16.2", r.UserAgent())
		if len(ids) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"can_take_number":true}`)
	}))
	t.Cleanup(server.Close)
	linked := newProjectTokenTestService(t, &projectTokenTestClient{})
	account := projectTokenTestAccount(99)
	account.Credentials["plan_kind"] = ZhipuPlanOffPeak
	ctx := WithAccountOutboundIdentity(context.Background(), account)
	identity, _ := outboundidentity.FromContext(ctx)
	holder := &zhipuOffPeakHolder{account: account, identity: identity, auth: zcode.OffPeakAuth{JWT: "plan-jwt"}}
	manager := &ZhipuOffPeakTicketManager{projectTokens: linked}
	client := zcode.NewOffPeakClient(server.Client(), server.URL)
	err := manager.withProjectAuth(ctx, holder, func(auth zcode.OffPeakAuth) error {
		_, err := client.Availability(ctx, auth)
		return err
	})
	require.NoError(t, err)
	require.Len(t, ids, 2)
	require.NotEqual(t, ids[0], ids[1])
	require.Equal(t, []string{"pat-login-1", "pat-login-2"}, keys)
}
