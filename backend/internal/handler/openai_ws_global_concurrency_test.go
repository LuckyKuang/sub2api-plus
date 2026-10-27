//go:build unit || !integration

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const wsGlobalConcurrencyUserID = int64(1701)

type wsGlobalConcurrencyUserRepo struct {
	service.UserRepository
	limit atomic.Int64
}

type wsGlobalConcurrencyAccountRepo struct {
	*openAIWSUsageHandlerAccountRepoStub
	selections atomic.Int64
}

func (r *wsGlobalConcurrencyAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	r.selections.Add(1)
	return r.openAIWSUsageHandlerAccountRepoStub.ListSchedulableByPlatform(ctx, platform)
}

func (r *wsGlobalConcurrencyAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *wsGlobalConcurrencyUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: wsGlobalConcurrencyUserID, Concurrency: int(r.limit.Load()), Status: service.StatusActive}, nil
}

// Only the renewal interval is accelerated. Admission and owner-preserving
// renewal still use the production Redis Lua scripts.
type wsGlobalConcurrencyCache struct {
	service.ConcurrencyCache
	service.APIKeyConcurrencyCache
	lease service.ConcurrencyLeaseCache
}

func (c *wsGlobalConcurrencyCache) ConcurrencySlotTTL() time.Duration { return 600 * time.Millisecond }
func (c *wsGlobalConcurrencyCache) RefreshUserSlot(ctx context.Context, id int64, owner string) (bool, error) {
	return c.lease.RefreshUserSlot(ctx, id, owner)
}
func (c *wsGlobalConcurrencyCache) RefreshAccountSlot(ctx context.Context, id int64, owner string) (bool, error) {
	return c.lease.RefreshAccountSlot(ctx, id, owner)
}

type openAIWSGlobalConcurrencyHarness struct {
	handler     *OpenAIGatewayHandler
	apiKey      *service.APIKey
	clientConn  *coderws.Conn
	handlerDone <-chan struct{}
	concurrency *service.ConcurrencyService
	pool        service.ConcurrencyCache
	userRepo    *wsGlobalConcurrencyUserRepo
	owners      *concurrencycache.UserOwners
	accounts    *wsGlobalConcurrencyAccountRepo
}

type wsGlobalConcurrencyBillingCache struct {
	service.BillingCache
	checks    atomic.Int64
	exhausted atomic.Bool
}

func (c *wsGlobalConcurrencyBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	c.checks.Add(1)
	if c.exhausted.Load() {
		return 0, nil
	}
	return 100, nil
}

func (c *wsGlobalConcurrencyBillingCache) DeductUserBalance(context.Context, int64, float64) error {
	return nil
}

func newOpenAIWSGlobalConcurrencyHarness(t *testing.T, upstreamURL string, initialLimit int, billingCaches ...service.BillingCache) *openAIWSGlobalConcurrencyHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool, owners := concurrencycache.New(t)
	leaseCache, ok := pool.(service.ConcurrencyLeaseCache)
	require.True(t, ok, "the production Redis cache must support owner-preserving renewal")
	apiKeyCache, ok := pool.(service.APIKeyConcurrencyCache)
	require.True(t, ok, "the production Redis cache must track API-key execution owners")
	renewingPool := &wsGlobalConcurrencyCache{ConcurrencyCache: pool, APIKeyConcurrencyCache: apiKeyCache, lease: leaseCache}
	concurrency := service.NewConcurrencyService(renewingPool)
	users := &wsGlobalConcurrencyUserRepo{}
	users.limit.Store(int64(initialLimit))
	concurrency.SetUserRepository(users)

	groupID := int64(4201)
	account := service.Account{
		ID: 9901, Name: "ws-user-capacity", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10,
		Credentials: map[string]any{"api_key": "sk-isolated-ws-capacity", "base_url": upstreamURL},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
		AccountGroups: []service.AccountGroup{{AccountID: 9901, GroupID: groupID}},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	var billingCache service.BillingCache
	if len(billingCaches) > 0 {
		cfg.RunMode = config.RunModeStandard
		billingCache = billingCaches[0]
	}
	billing := service.NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
	accounts := &wsGlobalConcurrencyAccountRepo{openAIWSUsageHandlerAccountRepoStub: &openAIWSUsageHandlerAccountRepoStub{account: account}}
	gateway := service.NewOpenAIGatewayService(
		accounts, usage, nil, nil, nil, nil, nil,
		cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing,
		&compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h := &OpenAIGatewayHandler{
		cfg: cfg, gatewayService: gateway, billingCacheService: billing,
		apiKeyService:     &service.APIKeyService{},
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Second),
	}
	attachDisabledIPAccessControlForWebSocketTest(h)
	key := &service.APIKey{ID: 1801, UserID: wsGlobalConcurrencyUserID, GroupID: &groupID,
		User: &service.User{ID: wsGlobalConcurrencyUserID, Status: service.StatusActive}}
	done := make(chan struct{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		// Deliberately stale: each turn must consult the authoritative user row.
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: wsGlobalConcurrencyUserID, Concurrency: 99})
		c.Next()
	})
	router.GET("/openai/v1/responses", func(c *gin.Context) {
		defer close(done)
		h.ResponsesWebSocket(c)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
	cancel()
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })
	return &openAIWSGlobalConcurrencyHarness{handler: h, apiKey: key, clientConn: client, handlerDone: done, concurrency: concurrency, pool: pool, userRepo: users, owners: owners, accounts: accounts}
}

func occupyWSGlobalConcurrencySlots(t *testing.T, h *openAIWSGlobalConcurrencyHarness, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		ctx, cancel := service.WithConcurrencyLeaseContext(context.Background())
		slot, err := h.concurrency.AcquireUserSlot(ctx, wsGlobalConcurrencyUserID, 99)
		require.NoError(t, err)
		require.True(t, slot.Acquired)
		t.Cleanup(func() { slot.ReleaseFunc(); cancel(nil) })
	}
}

func TestOpenAIResponsesWebSocket_GlobalConcurrencyRejectsFirstTurnImmediatelyBeforeSelection(t *testing.T) {
	// User-approved external overload contract: C=3 rejects request four
	// immediately, rather than retaining a queued connection. Authentication's
	// cached limit cannot enlarge current capacity, and rejection has no provider
	// selection, account-slot or upstream-write side effect.
	var providerCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		http.Error(w, "must not reach provider", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	h := newOpenAIWSGlobalConcurrencyHarness(t, upstream.URL, 3)
	occupyWSGlobalConcurrencySlots(t, h, 3)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err := h.clientConn.Read(ctx)
	cancel()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr, "full user capacity must close immediately, before waiting for any existing execution")
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
	require.Contains(t, closeErr.Reason, "too many concurrent requests")
	select {
	case <-h.handlerDone:
	case <-time.After(time.Second):
		t.Fatal("capacity-rejected WS handler did not finish")
	}
	require.Zero(t, h.accounts.selections.Load(), "full user capacity must reject before selecting a provider account")
	require.Zero(t, providerCalls.Load(), "full user capacity must not write upstream")
	users, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 3, users, "rejecting request four must retain the three admitted executions")
	accounts, err := h.pool.GetAccountConcurrency(context.Background(), 9901)
	require.NoError(t, err)
	require.Zero(t, accounts)
	keys, err := h.concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{1801})
	require.NoError(t, err)
	require.Zero(t, keys[1801], "the rejected WS API key must not publish an executing-request owner")
}

func TestOpenAILive_GlobalConcurrencyRejectsImmediatelyBeforeBillingAndSelection(t *testing.T) {
	// Live shares the user-wide pool. At C=3, a fourth external create must
	// return HTTP 429 while all existing work remains admitted. Billing, account
	// selection and a provider create are forbidden after that capacity decision.
	var providerCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		http.Error(w, "must not reach provider", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	h := newOpenAIWSGlobalConcurrencyHarness(t, upstream.URL, 3)
	occupyWSGlobalConcurrencySlots(t, h, 3)
	liveHandler := *h.handler
	liveHandler.billingCacheService = nil
	groupID := int64(4201)
	key := &service.APIKey{ID: 1802, UserID: wsGlobalConcurrencyUserID, GroupID: &groupID, User: h.apiKey.User,
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, AllowLive: true}}
	router := gin.New()
	router.POST("/v1/live", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: wsGlobalConcurrencyUserID, Concurrency: 99})
		liveHandler.Live(c)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/live", strings.NewReader(`{"sdp":"v=0\r\n","session":{"model":"gpt-live-test"}}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	require.NoError(t, err, "full user capacity must respond without waiting for any admitted execution")
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusTooManyRequests, response.StatusCode, "capacity takes precedence over the unavailable billing dependency")
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "Live concurrency limit reached")
	require.Zero(t, h.accounts.selections.Load())
	require.Zero(t, providerCalls.Load())
	users, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 3, users)
	accounts, err := h.pool.GetAccountConcurrency(context.Background(), 9901)
	require.NoError(t, err)
	require.Zero(t, accounts)
	keys, err := h.concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{1802})
	require.NoError(t, err)
	require.Zero(t, keys[1802])
}

func writeWSGlobalConcurrencyTurn(t *testing.T, conn *coderws.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"capacity acceptance"}`)))
}

func TestOpenAIResponsesWebSocket_GlobalConcurrencyUsesCurrentLimitOnNextTurn(t *testing.T) {
	// Business contract: an idle connection does not retain execution capacity;
	// after the admin lowers C to 1, another active request occupies that one slot.
	// The same WebSocket must reject its next turn despite its stale limit of 99.
	var calls atomic.Int32
	upstreamDone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			_, _, err = conn.Read(ctx)
			cancel()
			if err != nil {
				return
			}
			calls.Add(1)
			ctx, cancel = context.WithTimeout(r.Context(), 3*time.Second)
			err = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_capacity","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
			cancel()
			if err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	h := newOpenAIWSGlobalConcurrencyHarness(t, upstream.URL, 3)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, payload, err := h.clientConn.Read(ctx)
	cancel()
	require.NoError(t, err)
	require.Contains(t, string(payload), "response.completed")
	require.Eventually(t, func() bool {
		count, e := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
		return e == nil && count == 0
	}, time.Second, 5*time.Millisecond)

	otherCtx, stopOther := service.WithConcurrencyLeaseContext(context.Background())
	defer stopOther(nil)
	other, err := h.concurrency.AcquireUserSlot(otherCtx, wsGlobalConcurrencyUserID, 99)
	require.NoError(t, err)
	require.True(t, other.Acquired)
	defer other.ReleaseFunc()
	h.userRepo.limit.Store(1)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err = h.clientConn.Read(ctx)
	cancel()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
	require.Contains(t, closeErr.Reason, "too many concurrent requests")
	select {
	case <-h.handlerDone:
	case <-time.After(time.Second):
		t.Fatal("rejected next turn did not finish the connection handler")
	}
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("rejected next turn did not close the provider socket")
	}
	require.EqualValues(t, 1, calls.Load(), "the rejected next turn must not be sent to the provider")
	count, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 1, count, "rejecting the WebSocket turn must leave the independent request counted")
	other.ReleaseFunc()
	count, err = h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestOpenAIResponsesWebSocket_GlobalConcurrencyRejectsNextTurnBeforeStandardBilling(t *testing.T) {
	// An accepted first turn exercises normal balance billing. While this
	// connection is idle, three independent requests fill C=3 and the balance
	// dependency starts refusing admission. Request four must still close 1013
	// immediately, without reading balance or sending a second provider turn.
	var providerTurns atomic.Int64
	upstreamDone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			_, _, err = conn.Read(ctx)
			cancel()
			if err != nil {
				return
			}
			providerTurns.Add(1)
			ctx, cancel = context.WithTimeout(r.Context(), 3*time.Second)
			err = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_capacity_billing","model":"gpt-5.1","usage":{"input_tokens":0,"output_tokens":0}}}`))
			cancel()
			if err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	billingCache := &wsGlobalConcurrencyBillingCache{}
	h := newOpenAIWSGlobalConcurrencyHarness(t, upstream.URL, 3, billingCache)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, payload, err := h.clientConn.Read(ctx)
	cancel()
	require.NoError(t, err)
	require.Contains(t, string(payload), "response.completed")
	require.EqualValues(t, 1, billingCache.checks.Load(), "the successful first turn must exercise standard billing")
	require.Eventually(t, func() bool {
		users, userErr := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
		accounts, accountErr := h.pool.GetAccountConcurrency(context.Background(), 9901)
		return userErr == nil && accountErr == nil && users == 0 && accounts == 0
	}, time.Second, 5*time.Millisecond)
	occupyWSGlobalConcurrencySlots(t, h, 3)
	billingCache.exhausted.Store(true)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err = h.clientConn.Read(ctx)
	cancel()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr, "full user capacity must reject without entering normal billing dependencies")
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code, "capacity must take precedence over a failing balance check")
	require.Contains(t, closeErr.Reason, "too many concurrent requests")
	select {
	case <-h.handlerDone:
	case <-time.After(time.Second):
		t.Fatal("capacity-rejected subsequent turn did not finish the handler")
	}
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("capacity-rejected subsequent turn did not close the provider socket")
	}
	require.EqualValues(t, 1, billingCache.checks.Load(), "capacity rejection must not read or mutate billing dependencies")
	require.EqualValues(t, 1, providerTurns.Load(), "the rejected next turn must not be sent to the provider")
	users, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 3, users, "the three independent requests must remain admitted")
	accounts, err := h.pool.GetAccountConcurrency(context.Background(), 9901)
	require.NoError(t, err)
	require.Zero(t, accounts, "the rejected turn must not retain an account slot")
	keys, err := h.concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{1801})
	require.NoError(t, err)
	require.Zero(t, keys[1801], "the rejected turn must not publish an API-key execution owner")
}

func TestOpenAIResponsesWebSocket_GlobalUserLeaseLossStopsActualProviderSocket(t *testing.T) {
	// Removing the admitted user owner represents authoritative capacity loss.
	// The client remains connected; a real provider socket must stop before its
	// normal execution timeout, independently from the ingress connection lease.
	started := make(chan struct{})
	stopped := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			stopped <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, _, err = conn.Read(ctx)
		if err != nil {
			stopped <- err
			return
		}
		close(started)
		_, _, err = conn.Read(ctx)
		if ctx.Err() != nil {
			stopped <- ctx.Err()
			return
		}
		stopped <- err
	}))
	defer upstream.Close()
	h := newOpenAIWSGlobalConcurrencyHarness(t, upstream.URL, 1)
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not receive the admitted turn")
	}
	count, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	// A normal client keeps reading so coderws can acknowledge the gateway's
	// close control frame. It must not initiate cancellation of this execution.
	clientClosed := make(chan error, 1)
	go func() {
		readCtx, stopRead := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopRead()
		_, _, readErr := h.clientConn.Read(readCtx)
		clientClosed <- readErr
	}()
	require.NoError(t, h.owners.RemoveAll(context.Background(), wsGlobalConcurrencyUserID))
	select {
	case err := <-stopped:
		require.Error(t, err, "provider must observe the admitted socket being closed")
		require.NotErrorIs(t, err, context.DeadlineExceeded, "normal provider timeout is not evidence of lease cancellation")
	case <-time.After(time.Second):
		t.Fatal("actual provider WebSocket survived loss of its regular user execution lease")
	}
	select {
	case readErr := <-clientClosed:
		require.Error(t, readErr, "the gateway must close its client connection after capacity loss")
		require.NotErrorIs(t, readErr, context.DeadlineExceeded, "the client must receive the gateway close rather than time out")
	case <-time.After(time.Second):
		t.Fatal("client did not receive the capacity-loss close frame")
	}
	select {
	case <-h.handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler survived its lost user lease")
	}
	require.Eventually(t, func() bool {
		users, e := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
		accounts, ae := h.pool.GetAccountConcurrency(context.Background(), 9901)
		return e == nil && ae == nil && users == 0 && accounts == 0
	}, time.Second, 5*time.Millisecond)
}
