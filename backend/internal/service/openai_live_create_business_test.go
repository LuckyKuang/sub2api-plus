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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyutil"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
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

func (c *liveCreateBusinessConcurrency) RefreshLiveLease(context.Context, int64, int64, int64, string) (bool, error) {
	return true, nil
}

func (c *liveCreateBusinessConcurrency) ReleaseLiveLease(_ context.Context, accountID, userID, apiKeyID int64, leaseID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease := liveCreateBusinessLease{accountID, userID, apiKeyID, leaseID}
	c.leaseReleased = append(c.leaseReleased, lease)
	delete(c.held, leaseID)
	return nil
}

type liveCreateBusinessStore struct {
	*liveTestStore
	*schedulerTestGatewayCache
	saveErr      error
	saveAttempts []*LiveCallRecord
	beforeSave   func()
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
	return false, nil
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

func TestCreateLiveCallMappingFailureReleasesBothKindsOfCapacity(t *testing.T) {
	proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/call_unsaved")
	account := liveCreateBusinessAccount(603, 0, "save-owner-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
	svc, store, concurrency, _, directTCP := newLiveCreateBusinessService(t, []Account{account})
	saveErr := errors.New("mapping storage unavailable")
	store.saveErr = saveErr
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 703, UserID: 704}, 3)
	require.ErrorIs(t, err, saveErr)
	require.Nil(t, created)
	require.Equal(t, []int64{603}, concurrency.regularReleased)
	require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
	require.Empty(t, concurrency.held)
	require.Nil(t, store.record, "a failed save cannot expose a call mapping")
	require.Len(t, store.saveAttempts, 1)
	require.Len(t, wire.headers, 1, "storage failure cannot replay the upstream call")
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
}
