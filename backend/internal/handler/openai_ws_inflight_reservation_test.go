//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsInflightCache struct {
	*handlerInflightCache
	created []string
}

func (m *wsInflightCache) ReserveInflightBalance(ctx context.Context, userID int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	allowed, other, err := m.handlerInflightCache.ReserveInflightBalance(ctx, userID, id, amount, balance, ttl)
	if allowed && err == nil {
		m.mu.Lock()
		m.created = append(m.created, id)
		m.mu.Unlock()
	}
	return allowed, other, err
}

func (m *wsInflightCache) UpdateInflightBalance(_ context.Context, _ int64, id string, amount, balance float64, _ time.Duration) (bool, float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	other, owners := 0.0, 0
	for owner, held := range m.res {
		if owner != id {
			other += held
			owners++
		}
	}
	if _, exists := m.res[id]; !exists || (owners > 0 && balance-other < amount) {
		return false, other, nil
	}
	if amount > m.res[id] {
		m.res[id] = amount
	}
	return true, other, nil
}

func (m *wsInflightCache) DeductUserBalance(_ context.Context, _ int64, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balance -= amount
	return nil
}

func (m *wsInflightCache) heldAmounts() []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	amounts := make([]float64, 0, len(m.res))
	for _, amount := range m.res {
		amounts = append(amounts, amount)
	}
	return amounts
}

func (m *wsInflightCache) createdIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.created...)
}

type wsInflightBill struct {
	command *service.UsageBillingCommand
	release chan struct{}
}

type wsInflightLedger struct {
	service.UsageBillingRepository
	bills chan wsInflightBill
	stop  chan struct{}
}

func (m *wsInflightLedger) Apply(ctx context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	bill := wsInflightBill{command: command, release: make(chan struct{})}
	select {
	case m.bills <- bill:
	case <-m.stop:
		return nil, context.Canceled
	}
	select {
	case <-bill.release:
		return &service.UsageBillingApplyResult{Applied: true}, nil
	case <-m.stop:
		return nil, context.Canceled
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type wsInflightFrame struct {
	payload []byte
	reply   chan string
}

type wsInflightHarness struct {
	client       *coderws.Conn
	frames       chan wsInflightFrame
	ledger       *wsInflightLedger
	cache        *wsInflightCache
	slots        *concurrencyCacheMock
	dials        atomic.Int32
	done         chan struct{}
	keyRepo      *wsGroupPricingAPIKeyRepoStub
	keyService   *service.APIKeyService
	billing      *service.BillingCacheService
	key          *service.APIKey
	accountRepo  *openAIWSFailoverHandlerAccountRepoStub
	clientURL    string
	providerStop chan struct{}
}

func newWSInflightHarness(t *testing.T, mode string, balance float64, pricing []service.ChannelModelPricing, failover bool, configure ...func(*config.Config)) *wsInflightHarness {
	return newWSInflightHarnessWithSetup(t, mode, balance, pricing, failover, nil, nil, configure...)
}

func newWSInflightHarnessWithSetup(t *testing.T, mode string, balance float64, pricing []service.ChannelModelPricing, failover bool, settings *service.SettingService, setup func(*wsInflightHarness, *OpenAIGatewayHandler), configure ...func(*config.Config)) *wsInflightHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &wsInflightHarness{
		frames: make(chan wsInflightFrame, 8), done: make(chan struct{}, 4),
		cache:        &wsInflightCache{handlerInflightCache: newHandlerInflightCache(balance)},
		ledger:       &wsInflightLedger{bills: make(chan wsInflightBill, 8), stop: make(chan struct{})},
		providerStop: make(chan struct{}),
	}
	providerCtx, cancelProvider := context.WithCancel(context.Background())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.dials.Add(1)
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				return
			}
			frame := wsInflightFrame{payload: payload, reply: make(chan string, 1)}
			select {
			case f.frames <- frame:
			case <-f.providerStop:
				return
			case <-r.Context().Done():
				return
			}
			select {
			case reply := <-frame.reply:
				if gjson.Get(reply, "type").String() == "error" {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, reply)
				} else {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", reply)
				}
			case <-f.providerStop:
				return
			case <-r.Context().Done():
			}
			return
		}
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			readCtx, cancelRead := context.WithTimeout(providerCtx, 10*time.Second)
			_, payload, err := conn.Read(readCtx)
			cancelRead()
			if err != nil {
				return
			}
			frame := wsInflightFrame{payload: payload, reply: make(chan string, 1)}
			select {
			case f.frames <- frame:
			case <-f.providerStop:
				return
			case <-r.Context().Done():
				return
			}
			select {
			case reply := <-frame.reply:
				writeCtx, cancelWrite := context.WithTimeout(providerCtx, 3*time.Second)
				err = conn.Write(writeCtx, coderws.MessageText, []byte(reply))
				cancelWrite()
				if err != nil {
					return
				}
			case <-f.providerStop:
				return
			case <-r.Context().Done():
				return
			}
		}
	}))

	cfg := &config.Config{}
	cfg.RunMode = config.RunModeStandard
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled = true, true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2, cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true, true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds, cfg.Gateway.OpenAIWS.ReadTimeoutSeconds, cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3, 10, 3
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, DefaultMaxTokens: 1000}
	for _, apply := range configure {
		apply(cfg)
	}
	f.keyRepo = newWSGroupPricingAPIKeyRepoStub(1)
	f.keyRepo.group.ModelPricing = pricing
	f.keyService = newWSGroupPricingAPIKeyService(f.keyRepo)
	var err error
	f.key, err = f.keyService.GetByKey(context.Background(), wsGroupPricingKey)
	require.NoError(t, err)
	account := service.Account{
		ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{"api_key": "sk-ws-test", "base_url": upstream.URL,
			"model_mapping": map[string]any{"gpt-6-luna": "gpt-6-sol", "gpt-5.6-luna": "gpt-5.6-sol"}},
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode": mode},
		AccountGroups: []service.AccountGroup{{AccountID: 9901, GroupID: wsGroupPricingGroupID}},
	}
	accounts := []service.Account{account}
	if failover {
		next := account
		next.ID, next.Priority = 9902, 2
		next.AccountGroups = []service.AccountGroup{{AccountID: 9902, GroupID: wsGroupPricingGroupID}}
		accounts = append(accounts, next)
	}
	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	f.accountRepo = accountRepo
	f.billing = service.NewBillingCacheService(f.cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(f.billing.Stop)
	logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
	f.slots = &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	concurrency := service.NewConcurrencyService(f.slots)
	gateway := service.NewOpenAIGatewayService(accountRepo, logs, f.ledger, nil, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), service.NewRateLimitService(accountRepo, nil, cfg, nil, nil), f.billing,
		&compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, nil, nil, settings, nil, nil)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 2, QueueSize: 8, TaskTimeout: 10 * time.Second})
	t.Cleanup(pool.Stop)
	t.Cleanup(func() { close(f.ledger.stop) })
	h := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway, billingCacheService: f.billing,
		apiKeyService: f.keyService, usageRecordWorkerPool: pool, maxAccountSwitches: 2,
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Second)}
	attachDisabledIPAccessControlForWebSocketTest(h)
	if setup != nil {
		setup(f, h)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), f.key)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: f.key.User.ID, Concurrency: 1})
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, f.key.Group))
		c.Next()
	})
	router.GET("/openai/v1/responses", func(c *gin.Context) { h.ResponsesWebSocket(c); f.done <- struct{}{} })
	server := httptest.NewServer(router)
	f.clientURL = "ws" + strings.TrimPrefix(server.URL, "http") + "/openai/v1/responses"
	f.reconnect(t)
	t.Cleanup(func() {
		// Assertions may fail while the gateway is waiting for a deliberately
		// withheld provider reply. Stop that responder before waiting on servers.
		close(f.providerStop)
		cancelProvider()
		_ = f.client.CloseNow()
		server.Close()
		upstream.Close()
	})
	return f
}

func (f *wsInflightHarness) reconnect(t *testing.T) {
	t.Helper()
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDial()
	var err error
	f.client, _, err = coderws.Dial(dialCtx, f.clientURL, &coderws.DialOptions{CompressionMode: coderws.CompressionContextTakeover})
	require.NoError(t, err)
}

func (f *wsInflightHarness) send(t *testing.T, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, f.client.Write(ctx, coderws.MessageText, []byte(payload)))
}

func (f *wsInflightHarness) frame(t *testing.T) wsInflightFrame {
	t.Helper()
	select {
	case frame := <-f.frames:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("accepted turn did not reach upstream")
		return wsInflightFrame{}
	}
}

func (f *wsInflightHarness) bill(t *testing.T) wsInflightBill {
	t.Helper()
	select {
	case bill := <-f.ledger.bills:
		return bill
	case <-time.After(5 * time.Second):
		t.Fatal("completed turn did not reach settlement")
		return wsInflightBill{}
	}
}

func (f *wsInflightHarness) complete(t *testing.T, frame wsInflightFrame, id string) {
	t.Helper()
	frame.reply <- fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"model":%q,"usage":{"input_tokens":2,"output_tokens":1}}}`, id, gjson.GetBytes(frame.payload, "model").String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, event, err := f.client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
}

func (f *wsInflightHarness) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("websocket handler did not release connection ownership")
	}
}

func wsInflightPayload(model string, maxTokens, size int, metadata string) string {
	body := fmt.Sprintf(`{"type":"response.create","model":%q,"input":"hello","max_output_tokens":%d%s}`, model, maxTokens, metadata)
	return strings.Replace(body, `"hello"`, `"hello`+strings.Repeat("x", size-len(body))+`"`, 1)
}

func wsInflightPublicCards() []service.ChannelModelPricing {
	price := func(v float64) *float64 { return &v }
	return []service.ChannelModelPricing{
		{Platform: service.PlatformOpenAI, Models: []string{"gpt-6-luna"}, BillingMode: service.BillingModeToken,
			InputPrice: price(.001), OutputPrice: price(.002), FastMultiplier: price(2), ReasoningEffortMultipliers: map[string]float64{"high": 3}},
		{Platform: service.PlatformOpenAI, Models: []string{"gpt-5.6-luna"}, BillingMode: service.BillingModeToken,
			InputPrice: price(.002), OutputPrice: price(.003), FlexMultiplier: price(.5), ReasoningEffortMultipliers: map[string]float64{"low": 2}},
	}
}

func TestOpenAIResponsesWebSocket_IndependentTurnHoldsUseCurrentPayloadAndFrozenPricing(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			f := newWSInflightHarness(t, mode, 100, wsInflightPublicCards(), false)
			firstWireModel, secondWireModel := "gpt-6-sol", "gpt-5.6-sol"
			if mode == service.OpenAIWSIngressModePassthrough {
				firstWireModel, secondWireModel = "gpt-6-luna", "gpt-5.6-luna"
			}
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, `,"service_tier":"priority","reasoning":{"effort":"high"}`))
			first := f.frame(t)
			require.Equal(t, firstWireModel, gjson.GetBytes(first.payload, "model").String())
			// 1000 input x .001 + 1000 output x .002; Fast 2 x effort 3.
			require.Equal(t, []float64{18}, f.cache.heldAmounts())
			f.complete(t, first, "resp_hold_first")
			firstBill := f.bill(t)
			require.InDelta(t, .024, firstBill.command.BalanceCost, 1e-10)
			require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 2 }))
			f.send(t, wsInflightPayload("gpt-5.6-luna", 2000, 8000, `,"service_tier":"flex","reasoning":{"effort":"low"}`))
			second := f.frame(t)
			require.Equal(t, secondWireModel, gjson.GetBytes(second.payload, "model").String())
			// New body/output caps: (2000 x .002 + 2000 x .003) x Flex .5 x effort 2 x group 2.
			require.ElementsMatch(t, []float64{18, 20}, f.cache.heldAmounts())
			require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 9 }))
			f.complete(t, second, "resp_hold_second")
			secondBill := f.bill(t)
			owners := f.cache.createdIDs()
			require.Len(t, owners, 2)
			require.NotEqual(t, owners[0], owners[1], "each turn must register its own balance owner")
			require.InDelta(t, .014, secondBill.command.BalanceCost, 1e-10, "in-flight admin changes must not reprice the admitted turn")
			require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
			f.waitDone(t)
			require.ElementsMatch(t, []float64{18, 20}, f.cache.heldAmounts(), "connection exit cannot release pending settlement references")
			close(firstBill.release)
			require.Eventually(t, func() bool { return f.cache.count() == 1 }, time.Second, time.Millisecond)
			require.Equal(t, []float64{20}, f.cache.heldAmounts())
			close(secondBill.release)
			require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
		})
	}
}

func TestOpenAIResponsesWebSocket_FinalMappedEstimateRejectsBeforeDialAndReleasesSlots(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			input, output := 2e-6, 10e-6
			f := newWSInflightHarness(t, mode, .015, []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output}}, false)
			other, err := f.billing.ReserveInflight(context.Background(), f.key.User, f.key.Group, nil, .005)
			require.NoError(t, err)
			defer other.HandlerDone()
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, err = f.client.Read(ctx)
			cancel()
			var closeErr coderws.CloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
			require.Contains(t, closeErr.Reason, "billing check failed")
			f.waitDone(t)
			require.Zero(t, f.dials.Load(), "selected-account $0.012 exceeds remaining $0.010; rejection must precede upstream dial")
			require.Empty(t, f.frames)
			require.Empty(t, f.ledger.bills)
			require.Equal(t, []float64{.005}, f.cache.heldAmounts(), "rejected preliminary Luna hold must be released")
			require.Equal(t, int32(1), atomic.LoadInt32(&f.slots.releaseUserCalled))
			require.Equal(t, int32(1), atomic.LoadInt32(&f.slots.releaseAccountCalled))
		})
	}
}

func TestOpenAIResponsesWebSocket_PassthroughQuoteUsesActualWireModel(t *testing.T) {
	input, output := 2e-6, 10e-6
	f := newWSInflightHarness(t, service.OpenAIWSIngressModePassthrough, .015, []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output}}, false)
	other, err := f.billing.ReserveInflight(context.Background(), f.key.User, f.key.Group, nil, .005)
	require.NoError(t, err)
	defer other.HandlerDone()
	f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
	frame := f.frame(t)
	require.Equal(t, "gpt-6-luna", gjson.GetBytes(frame.payload, "model").String(), "passthrough's public wire must ignore ordinary account mapping")
	amounts := f.cache.heldAmounts()
	require.Len(t, amounts, 2)
	require.InDelta(t, .0056, amounts[0]+amounts[1], 1e-12, "only the actual Luna price consumes admission capacity")
	f.complete(t, frame, "resp_passthrough_quote")
	bill := f.bill(t)
	require.InDelta(t, .0000007, bill.command.BalanceCost, 1e-12, "2 input at $0.10/M plus 1 output at $0.50/M")
	require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
	f.waitDone(t)
	amounts = f.cache.heldAmounts()
	require.Len(t, amounts, 2, "pending asynchronous settlement must keep its quote alive")
	require.InDelta(t, .0056, amounts[0]+amounts[1], 1e-12)
	close(bill.release)
	require.Eventually(t, func() bool { return f.cache.count() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, []float64{.005}, f.cache.heldAmounts())
}

func TestOpenAIResponsesWebSocket_InflightPassthroughHTTPFallbackRejectsMappedPriceBeforeWrite(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*config.Config)
	}{
		{name: "force HTTP", configure: func(cfg *config.Config) { cfg.Gateway.OpenAIWS.ForceHTTP = true }},
		{name: "large first frame", configure: func(cfg *config.Config) {
			cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = true
			cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes = 1000
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, output := 2e-6, 10e-6
			f := newWSInflightHarness(t, service.OpenAIWSIngressModePassthrough, .015, []service.ChannelModelPricing{{
				Platform: service.PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: service.BillingModeToken,
				InputPrice: &input, OutputPrice: &output,
			}}, false, tc.configure)
			other, err := f.billing.ReserveInflight(context.Background(), f.key.User, f.key.Group, nil, .005)
			require.NoError(t, err)
			defer other.HandlerDone()
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, err = f.client.Read(ctx)
			cancel()
			var closeErr coderws.CloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
			require.Contains(t, closeErr.Reason, "billing check failed")
			f.waitDone(t)
			require.Zero(t, f.dials.Load(), "HTTP fallback would map Luna to Sol: $0.012 must be rejected before an HTTP request is sent")
			require.Empty(t, f.frames)
			require.Empty(t, f.ledger.bills)
			require.Equal(t, []float64{.005}, f.cache.heldAmounts())
			require.Equal(t, int32(1), atomic.LoadInt32(&f.slots.releaseUserCalled))
			require.Equal(t, int32(1), atomic.LoadInt32(&f.slots.releaseAccountCalled))
		})
	}
}

func TestOpenAIResponsesWebSocket_InflightConnectionMappingRemainsBoundUntilReconnect(t *testing.T) {
	input, output := 2e-6, 10e-6
	f := newWSInflightHarness(t, service.OpenAIWSIngressModeCtxPool, .015, []service.ChannelModelPricing{{
		Platform: service.PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: service.BillingModeToken,
		InputPrice: &input, OutputPrice: &output,
	}}, false)
	payload := wsInflightPayload("gpt-6-luna", 1000, 4000, "")
	f.send(t, payload)
	first := f.frame(t)
	require.Equal(t, "gpt-6-sol", gjson.GetBytes(first.payload, "model").String())
	require.Equal(t, []float64{.012}, f.cache.heldAmounts())
	f.complete(t, first, "resp_mapping_before_edit")
	firstBill := f.bill(t)
	require.InDelta(t, .000014, firstBill.command.BalanceCost, 1e-12)
	close(firstBill.release)
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)

	// The authenticated upstream connection keeps its original account snapshot.
	// Replace the repository credentials atomically without mutating that snapshot.
	credentials := make(map[string]any, len(f.accountRepo.accounts[0].Credentials))
	for key, value := range f.accountRepo.accounts[0].Credentials {
		credentials[key] = value
	}
	credentials["model_mapping"] = map[string]any{"gpt-6-luna": "gpt-6-luna", "gpt-5.6-luna": "gpt-5.6-luna"}
	f.accountRepo.accounts[0].Credentials = credentials
	f.send(t, payload)
	unchanged := f.frame(t)
	require.Equal(t, "gpt-6-sol", gjson.GetBytes(unchanged.payload, "model").String(), "editing the repository does not replace this connection's wire account")
	require.Equal(t, []float64{.012}, f.cache.heldAmounts(), "the live connection still needs the Sol quote")
	f.complete(t, unchanged, "resp_mapping_still_bound")
	unchangedBill := f.bill(t)
	require.InDelta(t, .000014, unchangedBill.command.BalanceCost, 1e-12)
	close(unchangedBill.release)
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)

	other, err := f.billing.ReserveInflight(context.Background(), f.key.User, f.key.Group, nil, .005)
	require.NoError(t, err)
	defer other.HandlerDone()

	f.send(t, payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _, err = f.client.Read(ctx)
	cancel()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	require.Contains(t, closeErr.Reason, "billing check failed")
	f.waitDone(t)
	require.Empty(t, f.frames, "old connection would still write Sol; the latest cheaper mapping cannot admit that write")
	require.Empty(t, f.ledger.bills, "a rejected turn must not generate a usage bill")
	require.Equal(t, []float64{.005}, f.cache.heldAmounts())
	require.Equal(t, int32(3), atomic.LoadInt32(&f.slots.releaseUserCalled))
	require.Equal(t, int32(2), atomic.LoadInt32(&f.slots.releaseAccountCalled))

	// A new connection selects the edited account and can use the cheaper mapping.
	f.reconnect(t)
	f.send(t, payload)
	newFrame := f.frame(t)
	require.Equal(t, "gpt-6-luna", gjson.GetBytes(newFrame.payload, "model").String())
	amounts := f.cache.heldAmounts()
	require.Len(t, amounts, 2)
	require.InDelta(t, .0056, amounts[0]+amounts[1], 1e-12)
	f.complete(t, newFrame, "resp_mapping_after_reconnect")
	newBill := f.bill(t)
	require.InDelta(t, .0000007, newBill.command.BalanceCost, 1e-12)
	close(newBill.release)
	require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
	f.waitDone(t)
	require.Eventually(t, func() bool { return f.cache.count() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, []float64{.005}, f.cache.heldAmounts())
}

func TestOpenAIResponsesWebSocket_FailedLaterTurnRetryRetainsPublicModelAndPricingOwner(t *testing.T) {
	// HTTP bridge can reconstruct a later turn before any partial output. Native
	// passthrough instead closes with 1013 and requires the client to reconnect.
	f := newWSInflightHarness(t, service.OpenAIWSIngressModeHTTPBridge, 100, wsInflightPublicCards(), true)
	f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, `,"service_tier":"priority","reasoning":{"effort":"high"}`))
	f.complete(t, f.frame(t), "resp_retry_first")
	firstBill := f.bill(t)
	close(firstBill.release)
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
	require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 2 }))
	f.send(t, wsInflightPayload("gpt-5.6-luna", 2000, 8000, `,"service_tier":"flex","reasoning":{"effort":"low"}`))
	failed := f.frame(t)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(failed.payload, "model").String())
	require.Equal(t, []float64{20}, f.cache.heldAmounts())
	require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 9 }))
	failed.reply <- `{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"The usage limit has been reached"}}`
	retried := f.frame(t)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(retried.payload, "model").String(), "retry must map the failed turn's public model, not the connection's first model")
	require.Equal(t, []float64{20}, f.cache.heldAmounts(), "fresh retry owner must retain the failed turn's admitted pricing generation")
	f.complete(t, retried, "resp_retry_recovered")
	retryBill := f.bill(t)
	require.Equal(t, "gpt-5.6-luna", retryBill.command.Model)
	require.InDelta(t, .014, retryBill.command.BalanceCost, 1e-10)
	close(retryBill.release)
	require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
	f.waitDone(t)
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
	owners := f.cache.createdIDs()
	require.Len(t, owners, 3, "first, failed second and retry must have distinct reservation owners")
	require.NotEqual(t, owners[0], owners[1])
	require.NotEqual(t, owners[1], owners[2])
	require.Empty(t, f.ledger.bills, "failed attempt without usage cannot create a bill")
}
