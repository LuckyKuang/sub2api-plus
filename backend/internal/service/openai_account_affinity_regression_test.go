//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// V01/V02/V08: the canonical owner precedes scoring, response affinity and
// local contention. The expected owner is the persisted business decision.
func TestCodexCanonicalAffinityRegression(t *testing.T) {
	for _, mode := range []string{"legacy", "advanced", "weighted"} {
		for _, busy := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/idle", true: "/busy"}[busy], func(t *testing.T) {
				ctx := context.Background()
				groupID := int64(7)
				store := newFakeOpenAISessionRouteStore()
				_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:7", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
				require.NoError(t, err)
				cfg := &config.Config{}
				cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				cfg.Gateway.Scheduling.StickySessionWaitTimeout = time.Second
				cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
				svc := &OpenAIGatewayService{
					cfg: cfg, cache: &schedulerTestGatewayCache{}, sessionRouteStore: store,
					accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
						{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 100, GroupIDs: []int64{7}},
						{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{7}},
					}},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{101: !busy, 102: true}}),
				}
				enabled, weighted := "true", "false"
				if mode == "legacy" {
					enabled = "false"
				}
				if mode == "weighted" {
					weighted = "true"
				}
				svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService(enabled, weighted)
				defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.EqualValues(t, 101, selection.Account.ID, "healthy owner must survive scoring and local pressure")
				if busy {
					require.NotNil(t, selection.WaitPlan)
					require.EqualValues(t, 101, selection.WaitPlan.AccountID)
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				route, err := store.Get(ctx, "group:7", OpenAISessionRouteDigest("session"))
				require.NoError(t, err)
				require.EqualValues(t, 101, route.OwnerAccountID)
				require.EqualValues(t, 1, route.Generation)
			})
		}
	}
}

func TestCodexCanonicalReadFailureStopsSelectionRegression(t *testing.T) {
	store := newFakeOpenAISessionRouteStore()
	store.fail = ErrOpenAISessionRouteStoreUnavailable
	svc := &OpenAIGatewayService{sessionRouteStore: store, cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
	require.Nil(t, selection, "an unavailable route is never a first-session cache miss")
}

// V03: a temporary cost veto preserves the owner even when B passes the gate.
// When admission recovers the very next actual send still uses A.
func TestCodexCanonicalProfitVetoPreservesOwnerRegression(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	store := newFakeOpenAISessionRouteStore()
	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:7", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
	require.NoError(t, err)
	wire := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
	svc := newOpenAIRejectedFieldTestService(wire)
	a, b := *newOpenAIRejectedFieldTestAccount(), *newOpenAIRejectedFieldTestAccount()
	a.ID, b.ID = 101, 102
	a.Credentials["api_key"], b.Credentials["api_key"] = "owner-a", "candidate-b"
	a.Status, b.Status, a.Schedulable, b.Schedulable = StatusActive, StatusActive, true, true
	a.GroupIDs, b.GroupIDs = []int64{7}, []int64{7}
	ownerCost, candidateCost := 2.0, 0.1
	a.RateMultiplier, b.RateMultiplier = &ownerCost, &candidateCost
	svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
	svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{a, b}}
	var acquired []int64
	svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
	gate := &openAIProfitControlGate{groupID: 7, platform: PlatformOpenAI, threshold: 1, pricingAt: time.Now()}
	ctx = context.WithValue(ctx, openAIProfitControlGateCtxKey{}, gate)
	selected, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selected)
	require.Empty(t, acquired)
	require.Empty(t, wire.requests)
	gate.threshold = 3
	selected, _, err = svc.SelectAccountWithScheduler(ctx, &groupID, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	defer selected.ReleaseFunc()
	require.EqualValues(t, 101, selected.Account.ID)
	ctx = ContextWithSelectionProfitGate(ctx, selected)
	body := []byte(`{"model":"gpt-5.1","stream":true,"input":"complete root"}`)
	c := newOpenAIRejectedFieldTestContext(body)
	c.Request = c.Request.WithContext(ctx)
	_, err = svc.Forward(ctx, c, selected.Account, body)
	require.NoError(t, err)
	require.Len(t, wire.requests, 1)
	require.Equal(t, "Bearer owner-a", wire.requests[0].Header.Get("Authorization"))
	route, err := store.Get(ctx, "group:7", OpenAISessionRouteDigest("session"))
	require.NoError(t, err)
	require.EqualValues(t, 101, route.OwnerAccountID)
	require.EqualValues(t, 1, route.Generation)
}

// V16/V17: real HTTP/WS builders must isolate native API-key metadata using
// stable credential owners, with matching identities across HTTP and WS for the same grant.
func TestCodexAPIKeyWireIsolationRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key_id", int64(77))
	for key, value := range map[string]string{"thread-id": "thread", "x-codex-parent-thread-id": "parent", "x-codex-installation-id": "installation", "x-codex-window-id": "window"} {
		c.Request.Header.Set(key, value)
	}
	svc := &OpenAIGatewayService{}
	build := func(id int64, token string, ws bool) http.Header {
		a := &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": token}}
		if ws {
			h, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, a, token, OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", "session", "", "")
			require.NoError(t, err)
			return h
		}
		r, err := svc.buildUpstreamRequest(context.Background(), c, a, []byte(`{"model":"gpt-5.1","input":"hello","prompt_cache_key":"session"}`), token, true, "session", true)
		require.NoError(t, err)
		return r.Header
	}
	a := build(101, "key-a", false)
	b := build(102, "key-b", false)
	for _, key := range []string{"thread-id", "x-codex-parent-thread-id", "x-codex-window-id"} {
		require.NotEmpty(t, a.Get(key), key)
		require.NotEqual(t, a.Get(key), b.Get(key), key)
		require.Equal(t, a.Get(key), build(101, "key-a", false).Get(key), key)
		require.Equal(t, a.Get(key), build(101, "key-a", true).Get(key), key)
	}
	require.Equal(t, a.Get("thread-id"), a.Get("x-client-request-id"))
	require.Empty(t, a.Get("x-codex-installation-id"))
	require.NotEqual(t, a.Get("thread-id"), build(101, "key-relinked", true).Get("thread-id"))
}
