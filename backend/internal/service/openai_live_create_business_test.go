//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyutil"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type liveCreateBusinessLease struct {
	accountID, userID, apiKeyID int64
	leaseID                     string
}

type liveCreateBusinessConcurrency struct {
	schedulerTestConcurrencyCache
	mu                               sync.Mutex
	regularAcquired, regularReleased []int64
	regular                          map[int64]int
	leaseAcquired, leaseReleased     []liveCreateBusinessLease
	held                             map[string]liveCreateBusinessLease
	acquireLease                     func(context.Context) (bool, error)
	refreshLease                     func(context.Context) (bool, error)
	leaseReleaseNotify               chan struct{}
}

func (c *liveCreateBusinessConcurrency) AcquireAccountSlot(ctx context.Context, accountID int64, _ int, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.regularAcquired = append(c.regularAcquired, accountID)
	c.regular[accountID]++
	return true, nil
}

func (c *liveCreateBusinessConcurrency) ReleaseAccountSlot(_ context.Context, accountID int64, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.regularReleased = append(c.regularReleased, accountID)
	c.regular[accountID]--
	return nil
}

func (c *liveCreateBusinessConcurrency) AcquireLiveLease(ctx context.Context, accountID int64, _ int, userID int64, _ int, apiKeyID int64, leaseID string, replacing bool) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !replacing || c.regular[accountID] != 1 {
		return false, errors.New("live acquisition requires exactly one ordinary account slot")
	}
	if c.acquireLease != nil {
		acquired, err := c.acquireLease(ctx)
		if err != nil || !acquired {
			return acquired, err
		}
	}
	lease := liveCreateBusinessLease{accountID, userID, apiKeyID, leaseID}
	c.leaseAcquired = append(c.leaseAcquired, lease)
	c.held[leaseID] = lease
	return true, nil
}

func (c *liveCreateBusinessConcurrency) RefreshLiveLease(ctx context.Context, _ int64, _ int64, _ int64, _ string) (bool, error) {
	if c.refreshLease != nil {
		return c.refreshLease(ctx)
	}
	return true, nil
}

func (c *liveCreateBusinessConcurrency) ReleaseLiveLease(_ context.Context, accountID, userID, apiKeyID int64, leaseID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease := liveCreateBusinessLease{accountID, userID, apiKeyID, leaseID}
	c.leaseReleased = append(c.leaseReleased, lease)
	delete(c.held, leaseID)
	if c.leaseReleaseNotify != nil {
		select {
		case c.leaseReleaseNotify <- struct{}{}:
		default:
		}
	}
	return nil
}

type liveCreateBusinessStore struct {
	*liveTestStore
	*schedulerTestGatewayCache
	saveErr      error
	saveAttempts []*LiveCallRecord
	beforeSave   func()
	claims       atomic.Int32
}

func (s *liveCreateBusinessStore) SaveLiveCall(ctx context.Context, record *LiveCallRecord, ttl time.Duration) error {
	copy := *record
	s.saveAttempts = append(s.saveAttempts, &copy)
	if s.beforeSave != nil {
		s.beforeSave()
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	return s.liveTestStore.SaveLiveCall(ctx, record, ttl)
}

// Creation owns the lease until a controller takes over. Declining the observer
// claim isolates that boundary without allowing a background test goroutine to
// end the call or release its successful lease.
func (s *liveCreateBusinessStore) ClaimLiveController(context.Context, string, string, string) (bool, error) {
	s.claims.Add(1)
	return false, nil
}

type liveCreateCleanupDialer struct {
	dial func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error)
}

func (d liveCreateCleanupDialer) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	return d.dial(ctx, target, headers, proxy)
}

type liveCreateCleanupConn struct {
	*liveTestFrameConn
	writeErr error
	ack      bool
}

func (c *liveCreateCleanupConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	if err := c.liveTestFrameConn.WriteFrame(ctx, kind, payload); err != nil {
		return err
	}
	if c.ack {
		c.reads <- liveTestFrame{messageType: coderws.MessageText, payload: []byte(`{"type":"session.closed"}`)}
	}
	return nil
}

type liveCreateBusinessSend struct {
	accountID int64
	proxy     string
}

type liveCreateBusinessUpstream struct {
	sends      []liveCreateBusinessSend
	beforeSend func()
}

func (u *liveCreateBusinessUpstream) Do(request *http.Request, proxy string, accountID int64, _ int) (*http.Response, error) {
	u.sends = append(u.sends, liveCreateBusinessSend{accountID, proxy})
	if u.beforeSend != nil {
		u.beforeSend()
	}
	_, parsed, err := proxyurl.Parse(proxy)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DisableKeepAlives: true}
	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	return (&http.Client{Transport: transport, Timeout: time.Second}).Do(request)
}

func (u *liveCreateBusinessUpstream) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, accountID, concurrency)
}

type liveCreateBusinessWire struct {
	mu      sync.Mutex
	headers []http.Header
	bodies  [][]byte
}

func liveCreateBusinessProxy(t *testing.T, status int, location string) (*Proxy, *liveCreateBusinessWire) {
	t.Helper()
	wire := &liveCreateBusinessWire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read live request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wire.mu.Lock()
		wire.headers = append(wire.headers, r.Header.Clone())
		wire.bodies = append(wire.bodies, body)
		wire.mu.Unlock()
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = io.WriteString(w, `{"error":{"type":"server_error","message":"temporarily unavailable"}}`)
			return
		}
		_, _ = io.WriteString(w, "v=answer\r\n")
	}))
	t.Cleanup(server.Close)
	_, parsed, err := proxyurl.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	return &Proxy{Protocol: "http", Host: parsed.Hostname(), Port: port, Status: StatusActive}, wire
}

func newLiveCreateBusinessService(t *testing.T, accounts []Account) (*OpenAIGatewayService, *liveCreateBusinessStore, *liveCreateBusinessConcurrency, *liveCreateBusinessUpstream, *atomic.Int32) {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	var directTCP atomic.Int32
	destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			directTCP.Add(1)
		}
	}
	destination.Start()
	t.Cleanup(destination.Close)
	previous := chatGPTLiveCallsURL
	chatGPTLiveCallsURL = destination.URL + "/calls?intent=quicksilver&architecture=avas"
	t.Cleanup(func() { chatGPTLiveCallsURL = previous })
	store := &liveCreateBusinessStore{liveTestStore: &liveTestStore{}, schedulerTestGatewayCache: &schedulerTestGatewayCache{}}
	concurrency := &liveCreateBusinessConcurrency{regular: make(map[int64]int), held: make(map[string]liveCreateBusinessLease)}
	upstream := &liveCreateBusinessUpstream{}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "live-create-business-test-secret"}}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: store, cfg: cfg,
		concurrencyService: NewConcurrencyService(concurrency), httpUpstream: upstream,
		liveAttestation:       liveAttestationStub{header: `{"v":1,"s":0,"t":"v1.business"}`},
		liveAttestationCipher: newLiveAttestationCipher(cfg),
		settingService: &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexClientVersion: "0.180.0",
		}}},
	}
	return svc, store, concurrency, upstream, &directTCP
}

func liveCreateBusinessAccount(id int64, priority int, token, ua string, proxy *Proxy) Account {
	proxy.ID = id + 100
	return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Concurrency: 2, Priority: priority, ProxyID: &proxy.ID, Proxy: proxy,
		Credentials: map[string]any{"access_token": token, "chatgpt_account_id": "chatgpt-owner", "user_agent": ua}}
}

func liveCreateBusinessRequest() *LiveCallRequest {
	return &LiveCallRequest{SDP: "v=offer\r\n", Session: json.RawMessage(`{"model":"gpt-live-business","instructions":"session content"}`)}
}

func TestCreateLiveCallFailoverOwnsOnlySuccessfulAccountMappingAndLease(t *testing.T) {
	firstProxy, firstWire := liveCreateBusinessProxy(t, http.StatusServiceUnavailable, "")
	secondProxy, secondWire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_business_b")
	firstUA := "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal"
	secondUA := "codex_vscode/0.180.0 (Ubuntu 24.04; x86_64) vscode"
	first := liveCreateBusinessAccount(601, 0, "first-owner-bearer", firstUA, firstProxy)
	second := liveCreateBusinessAccount(602, 10, "second-owner-bearer", secondUA, secondProxy)
	svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{first, second})
	identity := LiveCallIdentity{APIKeyID: 701, UserID: 702, UserAgent: "caller metadata", IPAddress: "127.0.0.1", InboundEndpoint: "/v1/realtime/calls"}
	store.beforeSave = func() {
		require.Equal(t, []int64{601, 602}, concurrency.regularReleased, "each ordinary account slot must be released before saving the successful call")
		require.Len(t, concurrency.held, 1)
		for _, lease := range concurrency.held {
			require.EqualValues(t, 602, lease.accountID)
		}
	}
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), identity, 3)
	require.NoError(t, err)
	require.Equal(t, "call_business_b", created.CallID)
	require.Nil(t, created.createCleanup, "successful calls must discard temporary authentication material")
	require.Zero(t, svc.liveCreateReservations.Load(), "successful persistence returns creation/compensation capacity")
	require.EqualValues(t, 602, created.Account.ID)
	require.Equal(t, []byte("v=answer\r\n"), created.SDP)
	require.Equal(t, []liveCreateBusinessSend{{601, firstProxy.URL()}, {602, secondProxy.URL()}}, upstream.sends)
	require.Zero(t, directTCP.Load())
	for _, tc := range []struct {
		wire                           *liveCreateBusinessWire
		token, ua, originator, version string
	}{
		{firstWire, "first-owner-bearer", firstUA, "codex_cli_rs", "0.180.0"},
		{secondWire, "second-owner-bearer", secondUA, "codex_vscode", "0.180.0"},
	} {
		tc.wire.mu.Lock()
		headers := append([]http.Header(nil), tc.wire.headers...)
		bodies := append([][]byte(nil), tc.wire.bodies...)
		tc.wire.mu.Unlock()
		require.Len(t, headers, 1)
		require.Equal(t, "Bearer "+tc.token, headers[0].Get("Authorization"))
		require.Equal(t, tc.ua, headers[0].Get("User-Agent"))
		require.Equal(t, tc.originator, headers[0].Get("Originator"))
		require.Equal(t, tc.version, headers[0].Get("Version"))
		var forwarded LiveCallRequest
		require.NoError(t, json.Unmarshal(bodies[0], &forwarded))
		require.Equal(t, liveCreateBusinessRequest().SDP, forwarded.SDP)
		require.JSONEq(t, string(liveCreateBusinessRequest().Session), string(forwarded.Session))
	}
	require.Equal(t, []int64{601, 602}, concurrency.regularAcquired)
	require.Equal(t, []int64{601, 602}, concurrency.regularReleased)
	require.Equal(t, map[int64]int{601: 0, 602: 0}, concurrency.regular)
	require.Len(t, concurrency.leaseAcquired, 2)
	require.Len(t, concurrency.leaseReleased, 1)
	require.Equal(t, concurrency.leaseAcquired[0], concurrency.leaseReleased[0])
	require.NotEqual(t, concurrency.leaseAcquired[0].leaseID, concurrency.leaseAcquired[1].leaseID)
	require.Len(t, store.saveAttempts, 1)
	record, err := svc.GetLiveCallForIdentity(context.Background(), created.CallID, identity)
	require.NoError(t, err)
	require.EqualValues(t, 602, record.AccountID)
	require.Equal(t, identity.APIKeyID, record.APIKeyID)
	require.Equal(t, identity.UserID, record.UserID)
	require.Equal(t, concurrency.leaseAcquired[1].leaseID, record.LeaseID)
	require.Equal(t, "gpt-live-business", record.Model)
	require.Equal(t, map[string]liveCreateBusinessLease{record.LeaseID: concurrency.leaseAcquired[1]}, concurrency.held, "the stored successful call alone retains a live lease")
}

func TestCreateLiveCallMappingFailureClosesUpstreamBeforeReleasingCapacity(t *testing.T) {
	proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_unsaved")
	account := liveCreateBusinessAccount(603, 0, "save-owner-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, _, directTCP := newLiveCreateBusinessService(t, []Account{account})
	saveErr := errors.New("mapping storage unavailable")
	store.saveErr = saveErr
	unrelated := liveCreateBusinessLease{accountID: 999, userID: 998, apiKeyID: 997, leaseID: "another-live-owner"}
	concurrency.held[unrelated.leaseID] = unrelated
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalProxy := proxy.URL()
	store.beforeSave = func() {
		// A caller disconnect and an account edit must not prevent closing the
		// session under the grant and route that actually created it.
		cancel()
		account.Credentials["access_token"] = "replacement-bearer"
		account.Credentials["chatgpt_account_id"] = "replacement-owner"
		proxy.Host = "replacement.invalid"
	}
	conn := &liveCreateCleanupConn{liveTestFrameConn: newLiveTestFrameConn(), ack: true}
	dials := 0
	svc.openaiWSPassthroughDialer = liveCreateCleanupDialer{dial: func(cleanupCtx context.Context, target string, headers http.Header, route string) (openAIWSClientConn, int, http.Header, error) {
		dials++
		require.NoError(t, cleanupCtx.Err(), "request cancellation must not cancel compensation")
		deadline, ok := cleanupCtx.Deadline()
		require.True(t, ok, "compensation must carry its own deadline")
		require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		require.Equal(t, "wss://chatgpt.com/backend-api/codex/call_unsaved", target)
		require.Equal(t, originalProxy, route)
		require.Equal(t, "Bearer save-owner-bearer", headers.Get("Authorization"))
		require.Equal(t, wire.headers[0].Get("User-Agent"), headers.Get("User-Agent"))
		for _, key := range []string{"Originator", "Version", "session-id", "thread-id", liveAttestationHeader, "Chatgpt-Account-Id"} {
			require.Equal(t, wire.headers[0].Get(key), headers.Get(key), key)
		}
		require.Empty(t, concurrency.leaseReleased, "upstream close must precede lease release")
		require.Len(t, concurrency.held, 2)
		return conn, http.StatusSwitchingProtocols, nil, nil
	}}
	created, err := svc.CreateLiveCall(ctx, liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 703, UserID: 704}, 3)
	require.ErrorIs(t, err, saveErr)
	require.Nil(t, created)
	require.Equal(t, []int64{603}, concurrency.regularReleased)
	require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
	require.Equal(t, map[string]liveCreateBusinessLease{unrelated.leaseID: unrelated}, concurrency.held, "cleanup cannot release another call's capacity")
	require.Nil(t, store.record, "a failed save cannot expose a call mapping")
	require.Len(t, store.saveAttempts, 1)
	require.Len(t, wire.headers, 1, "storage failure cannot replay the upstream call")
	require.Equal(t, 1, dials, "failed persistence must close the actual created call")
	require.Zero(t, svc.liveCreateReservations.Load(), "confirmed cleanup returns its reservation")
	select {
	case frame := <-conn.writes:
		require.JSONEq(t, `{"type":"session.close"}`, string(frame.payload))
	default:
		t.Fatal("mapping failure abandoned the provider session without session.close")
	}
	select {
	case <-conn.closed:
	default:
		t.Fatal("cleanup left the provider control socket open")
	}
	require.Zero(t, store.claims.Load(), "an unsaved call cannot start the normal observer")
	require.Zero(t, directTCP.Load())
}

func TestCreateLiveCallMappingFailureRetainsCapacityUntilCleanupRecovery(t *testing.T) {
	for _, failure := range []string{"dial", "close-write", "close-unconfirmed", "cache-renewal"} {
		t.Run(failure, func(t *testing.T) {
			proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_recover")
			account := liveCreateBusinessAccount(613, 0, "recover-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
			svc, store, concurrency, _, directTCP := newLiveCreateBusinessService(t, []Account{account})
			store.saveErr = errors.New("mapping storage unavailable")
			// Other creations or unresolved cleanups already own 127 reservations.
			// The failing call takes the final one until provider cleanup succeeds.
			svc.liveCreateReservations.Store(127)
			concurrency.leaseReleaseNotify = make(chan struct{}, 1)
			if failure == "cache-renewal" {
				concurrency.refreshLease = func(context.Context) (bool, error) {
					return false, errors.New("distributed lease cache temporarily unavailable")
				}
			}
			var dials atomic.Int32
			allowRecovery := make(chan struct{})
			conn := &liveCreateCleanupConn{liveTestFrameConn: newLiveTestFrameConn(), ack: true}
			svc.openaiWSPassthroughDialer = liveCreateCleanupDialer{dial: func(ctx context.Context, _ string, headers http.Header, route string) (openAIWSClientConn, int, http.Header, error) {
				if dials.Add(1) == 1 {
					if failure == "close-write" {
						return &liveCreateCleanupConn{liveTestFrameConn: newLiveTestFrameConn(), writeErr: errors.New("upstream socket failed")}, http.StatusSwitchingProtocols, nil, nil
					}
					if failure == "close-unconfirmed" {
						return &liveCreateCleanupConn{liveTestFrameConn: newLiveTestFrameConn()}, http.StatusSwitchingProtocols, nil, nil
					}
					return nil, http.StatusBadGateway, nil, errors.New("cleanup proxy unavailable")
				}
				select {
				case <-allowRecovery:
				case <-ctx.Done():
					return nil, 0, nil, ctx.Err()
				}
				if headers.Get("Authorization") != "Bearer recover-bearer" || route != proxy.URL() {
					return nil, 0, nil, errors.New("cleanup changed credential owner or proxy")
				}
				return conn, http.StatusSwitchingProtocols, nil, nil
			}}
			created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 713, UserID: 714}, 3)
			require.ErrorIs(t, err, store.saveErr)
			require.Nil(t, created)
			require.EqualValues(t, 128, svc.liveCreateReservations.Load())
			rejected, rejectErr := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 715, UserID: 716}, 3)
			require.ErrorIs(t, rejectErr, ErrLiveUnavailable, "orphan protection must reject immediately at its independent service bound")
			require.Nil(t, rejected)
			concurrency.mu.Lock()
			require.Empty(t, concurrency.leaseReleased, "failed cleanup cannot claim provider execution ended")
			require.Len(t, concurrency.held, 1)
			concurrency.mu.Unlock()
			close(allowRecovery)
			select {
			case <-concurrency.leaseReleaseNotify:
			case <-time.After(3 * time.Second):
				t.Fatal("recovered cleanup failed to release this call's lease")
			}
			require.Eventually(t, func() bool { return svc.liveCreateReservations.Load() == 127 }, time.Second, time.Millisecond, "cleanup recovery must return only its reservation")
			concurrency.mu.Lock()
			require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
			require.Empty(t, concurrency.held)
			concurrency.mu.Unlock()
			require.EqualValues(t, 2, dials.Load(), "cleanup recovery must not replay session creation")
			require.Len(t, wire.headers, 1)
			require.Nil(t, store.record)
			require.Zero(t, store.claims.Load())
			require.Zero(t, directTCP.Load())
		})
	}
}

func TestCreateLiveCallMappingFailureUnconfirmedCleanupIsBoundedBySessionExpiry(t *testing.T) {
	proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_expire")
	account := liveCreateBusinessAccount(623, 0, "expire-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, _, directTCP := newLiveCreateBusinessService(t, []Account{account})
	svc.cfg.Gateway.Live.MaxSessionDurationSeconds = 1
	store.saveErr = errors.New("mapping storage unavailable")
	concurrency.leaseReleaseNotify = make(chan struct{}, 1)
	var dials atomic.Int32
	svc.openaiWSPassthroughDialer = liveCreateCleanupDialer{dial: func(ctx context.Context, _ string, _ http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
		dials.Add(1)
		return nil, http.StatusBadGateway, nil, errors.New("cleanup remains unavailable")
	}}
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 723, UserID: 724}, 3)
	require.ErrorIs(t, err, store.saveErr)
	require.Nil(t, created)
	concurrency.mu.Lock()
	require.Empty(t, concurrency.leaseReleased)
	require.Len(t, concurrency.held, 1)
	concurrency.mu.Unlock()
	select {
	case <-concurrency.leaseReleaseNotify:
		require.False(t, time.Now().Before(store.saveAttempts[0].ExpiresAt), "failed cleanup must retain capacity through the bounded session lifetime")
	case <-time.After(2 * time.Second):
		t.Fatal("unmapped-call compensation outlived its maximum session duration")
	}
	require.Eventually(t, func() bool { return svc.liveCreateReservations.Load() == 0 }, time.Second, time.Millisecond, "expiry must return bounded compensation capacity")
	require.EqualValues(t, 2, dials.Load(), "retry must be paced instead of spinning")
	require.Len(t, wire.headers, 1)
	require.Zero(t, store.claims.Load())
	require.Zero(t, directTCP.Load())
}

func TestCreateLiveCallMappingFailureClosesActualProviderThroughOriginalProxy(t *testing.T) {
	providerHeaders := make(chan http.Header, 1)
	providerFrames := make(chan []byte, 1)
	providerStopped := make(chan struct{}, 1)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerHeaders <- r.Header.Clone()
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow(); providerStopped <- struct{}{} }()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		providerFrames <- payload
		if err := conn.Write(ctx, coderws.MessageText, []byte(`{"type":"session.closed"}`)); err != nil {
			return
		}
		_, _, _ = conn.Read(ctx)
	}))
	t.Cleanup(provider.Close)
	var creates, connects atomic.Int32
	createHeaders := make(chan http.Header, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			creates.Add(1)
			createHeaders <- r.Header.Clone()
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Location", "/backend-api/codex/call_wire_unsaved")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "v=answer\r\n")
			return
		}
		connects.Add(1)
		remote, err := net.DialTimeout("tcp", provider.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, "local provider unavailable", http.StatusBadGateway)
			return
		}
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			return
		}
		defer func() { _ = remote.Close(); _ = client.Close() }()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := buffered.Flush(); err != nil {
			return
		}
		copied := make(chan struct{})
		go func() { _, _ = io.Copy(remote, buffered); close(copied) }()
		_, _ = io.Copy(client, remote)
		_ = remote.Close()
		_ = client.Close()
		<-copied
	}))
	t.Cleanup(proxyServer.Close)
	_, proxyURL, err := proxyurl.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxyURL.Port())
	require.NoError(t, err)
	proxy := &Proxy{Protocol: "http", Host: proxyURL.Hostname(), Port: port, Status: StatusActive}
	account := liveCreateBusinessAccount(633, 0, "wire-unsaved-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, _, directTCP := newLiveCreateBusinessService(t, []Account{account})
	store.saveErr = errors.New("mapping write failed after upstream success")
	production := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
	proxyClient, err := production.proxyHTTPClient(proxy.URL())
	require.NoError(t, err)
	transport := proxyClient.Transport.(*http.Transport)
	transport.TLSClientConfig = provider.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(transport.CloseIdleConnections)
	local := &liveWireLocalDestination{dialer: production, local: "wss" + strings.TrimPrefix(provider.URL, "https")}
	svc.openaiWSPassthroughDialer = local
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 733, UserID: 734}, 3)
	require.ErrorIs(t, err, store.saveErr)
	require.Nil(t, created)
	select {
	case payload := <-providerFrames:
		require.JSONEq(t, `{"type":"session.close"}`, string(payload), "a real provider close must precede capacity release")
	case <-time.After(time.Second):
		t.Fatal("mapping failure failed to close the actual upstream session")
	}
	select {
	case <-providerStopped:
	case <-time.After(time.Second):
		t.Fatal("cleanup left the actual upstream socket open")
	}
	var sentCreate, sentClose http.Header
	select {
	case sentCreate = <-createHeaders:
	default:
		t.Fatal("missing upstream create")
	}
	select {
	case sentClose = <-providerHeaders:
	default:
		t.Fatal("missing upstream cleanup handshake")
	}
	for _, key := range []string{"Authorization", "Chatgpt-Account-Id", "User-Agent", "Originator", "Version", "session-id", "thread-id", liveAttestationHeader} {
		require.NotEmpty(t, sentCreate.Get(key), key)
		require.Equal(t, sentCreate.Get(key), sentClose.Get(key), key)
	}
	require.Equal(t, "wss://chatgpt.com/backend-api/codex/call_wire_unsaved", local.official)
	require.EqualValues(t, 1, creates.Load(), "mapping failure cannot replay creation")
	require.EqualValues(t, 1, connects.Load(), "cleanup must use the original HTTP CONNECT proxy")
	require.Zero(t, directTCP.Load())
	require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
	require.Empty(t, concurrency.held)
	require.Zero(t, store.claims.Load())
}

func TestCreateLiveCallCompensationCapacityFullRejectsWithoutAnyUpstreamWrite(t *testing.T) {
	proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_reserved")
	account := liveCreateBusinessAccount(643, 0, "reserved-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{account})
	svc.liveCreateReservations.Store(128)
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 743, UserID: 744}, 3)
	require.ErrorIs(t, err, ErrLiveUnavailable)
	require.Nil(t, created)
	require.Empty(t, concurrency.regularAcquired, "internal reservation failure must precede account selection")
	require.Empty(t, concurrency.leaseAcquired)
	require.Empty(t, upstream.sends)
	require.Empty(t, store.saveAttempts)
	require.Empty(t, wire.headers)
	require.EqualValues(t, 128, svc.liveCreateReservations.Load(), "a rejected call cannot return capacity owned by other calls")
	// When an older cleanup finishes, the newly available reservation can be
	// used normally and is returned immediately after mapping persistence.
	svc.liveCreateReservations.Store(127)
	created, err = svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 743, UserID: 744}, 3)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.EqualValues(t, 127, svc.liveCreateReservations.Load())
	require.Len(t, upstream.sends, 1)
	require.Len(t, store.saveAttempts, 1)
	require.Zero(t, directTCP.Load())
}

func TestCreateLiveCallLeaseFailureOrCancellationReleasesOrdinarySlot(t *testing.T) {
	for _, outcome := range []string{"unavailable", "full", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_create")
			account := liveCreateBusinessAccount(604, 0, "lease-owner-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
			svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{account})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("lease cache unavailable")
			concurrency.acquireLease = func(context.Context) (bool, error) {
				switch outcome {
				case "full":
					return false, nil
				case "cancelled":
					cancel()
					return false, context.Canceled
				default:
					return false, wantErr
				}
			}
			created, err := svc.CreateLiveCall(ctx, liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 705, UserID: 706}, 3)
			switch outcome {
			case "full":
				require.ErrorIs(t, err, ErrLiveConcurrencyFull)
			case "cancelled":
				require.ErrorIs(t, err, context.Canceled)
			default:
				require.ErrorIs(t, err, wantErr)
			}
			require.Nil(t, created)
			require.Equal(t, []int64{604}, concurrency.regularAcquired)
			require.Equal(t, []int64{604}, concurrency.regularReleased)
			require.Zero(t, concurrency.regular[604])
			require.Empty(t, concurrency.held)
			require.Empty(t, concurrency.leaseReleased, "a failed acquisition does not own a live lease")
			require.Empty(t, upstream.sends)
			require.Empty(t, wire.headers)
			require.Empty(t, store.saveAttempts)
			require.Nil(t, store.record)
			require.Zero(t, directTCP.Load())
			require.Zero(t, svc.liveCreateReservations.Load(), "lease acquisition failure must return its creation reservation")
		})
	}
}

func TestCreateLiveCallCancellationDuringSendReleasesAllAcquiredCapacity(t *testing.T) {
	proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_create")
	account := liveCreateBusinessAccount(605, 0, "cancel-owner-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{account})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream.beforeSend = cancel
	created, err := svc.CreateLiveCall(ctx, liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 707, UserID: 708}, 3)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, created)
	require.Equal(t, []int64{605}, concurrency.regularReleased)
	require.Zero(t, concurrency.regular[605])
	require.Len(t, concurrency.leaseAcquired, 1)
	require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
	require.Empty(t, concurrency.held)
	require.Empty(t, wire.headers)
	require.Empty(t, store.saveAttempts)
	require.Nil(t, store.record)
	require.Zero(t, directTCP.Load())
	require.Zero(t, svc.liveCreateReservations.Load(), "upstream creation failure must return its reservation")
}
