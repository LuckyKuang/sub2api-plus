//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type genericRetailInflightWire struct {
	service.HTTPUpstream
	calls       int
	model       string
	reservation float64
	snapshot    *service.BillingPricingSnapshot
}

func (w *genericRetailInflightWire) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	w.calls++
	w.model = gjson.GetBytes(body, "model").String()
	w.reservation = service.InflightReservationFromContext(req.Context()).Amount()
	w.snapshot = service.BillingPricingSnapshotFromContext(req.Context())
	response := `{"id":"msg_retail_mapping","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":1000}}`
	contentType := "application/json"
	if gjson.GetBytes(body, "stream").Bool() {
		contentType = "text/event-stream"
		response = "event: message_start\n" +
			`data: {"type":"message_start","message":{"id":"msg_retail_mapping","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4","usage":{"input_tokens":1000,"output_tokens":0}}}` + "\n\n" +
			"event: content_block_start\n" +
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event: content_block_delta\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
			"event: content_block_stop\n" +
			`data: {"type":"content_block_stop","index":0}` + "\n\n" +
			"event: message_delta\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1000}}` + "\n\n" +
			"event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}, "X-Request-Id": {"generic-retail-mapping"}},
		Body: io.NopCloser(strings.NewReader(response))}, nil
}

func (w *genericRetailInflightWire) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return w.Do(req, proxy, accountID, concurrency)
}

// Without a channel, the generic gateway's existing retail contract bills the
// original known model even when an API-key account forwards another model.
// Balance covers Haiku plus the foreign hold, but cannot cover Sonnet. Selected
// account reconciliation must keep this successful business flow available.
func TestGenericHTTPAccountMappingPreservesDefaultRetailAdmissionAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "chat/completions", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint, stream), func(t *testing.T) {
				ctx := context.Background()
				const userID, groupID, accountID = int64(89801), int64(89802), int64(89803)
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				cache := repository.NewBillingCache(rdb)
				require.NoError(t, cache.SetUserBalance(ctx, userID, .006))
				raw := repository.NewConcurrencyCache(rdb, 15, 900)
				owned, err := raw.AcquireUserSlot(ctx, userID, 3, "foreign-user-owner")
				require.NoError(t, err)
				require.True(t, owned)
				owned, err = raw.AcquireAccountSlot(ctx, accountID, 2, "foreign-account-owner")
				require.NoError(t, err)
				require.True(t, owned)
				counts := &handlerAccountWaitPlanCache{ConcurrencyCache: raw}
				concurrency := service.NewConcurrencyService(counts)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
				account := &service.Account{ID: accountID, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
					AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
					Credentials: map[string]any{"api_key": "retail-fixture", "base_url": "https://retail.invalid",
						"model_mapping": map[string]any{"claude-3-haiku": "claude-sonnet-4"}}}
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.MaxBodySize = 1 << 20
				// Admission estimates one capped input token and 1000 output
				// tokens: Haiku's $0.25/M + $1.25/M card requires $0.00125025.
				cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, DefaultMaxTokens: 1000, MaxInputTokens: 1, MaxOutputTokens: 1000}
				billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				key := &service.APIKey{ID: 89804, UserID: userID, GroupID: &group.ID, Group: group, Status: service.StatusActive,
					User: &service.User{ID: userID, Status: service.StatusActive, Concurrency: 3, Balance: .006}}
				foreign, err := billing.ReserveInflight(ctx, key.User, group, nil, .0001)
				require.NoError(t, err)
				require.NotNil(t, foreign)
				t.Cleanup(foreign.HandlerDone)
				foreignHolds, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89801}").Result()
				require.NoError(t, err)
				require.Len(t, foreignHolds, 1)
				wire, ledger := &genericRetailInflightWire{}, &sevenProviderBillingRepo{}
				scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
				prices := service.NewBillingService(cfg, nil)
				gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, ledger, nil, nil, nil, nil, cfg, scheduler,
					concurrency, prices, nil, billing, nil, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
					service.NewModelPricingResolver(nil, prices), nil, nil, nil)
				h := &GatewayHandler{gatewayService: gateway, billingCacheService: billing, cfg: cfg, maxAccountSwitches: 1,
					concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0)}
				body := fmt.Sprintf(`{"model":"claude-3-haiku","stream":%t,"max_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`, stream)
				handle := h.Messages
				if endpoint == "chat/completions" {
					body = fmt.Sprintf(`{"model":"claude-3-haiku","stream":%t,"max_completion_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`, stream)
					handle = h.ChatCompletions
				} else if endpoint == "responses" {
					body = fmt.Sprintf(`{"model":"claude-3-haiku","stream":%t,"max_output_tokens":1000,"input":"hi"}`, stream)
					handle = h.Responses
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body)).WithContext(context.WithValue(ctx, ctxkey.Group, group))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
				handle(c)
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "ok")
				require.Equal(t, 1, wire.calls, "preserve the successful request without retries")
				require.Equal(t, "claude-sonnet-4", wire.model, "the configured account mapping must still reach the actual wire")
				require.InDelta(t, .00125025, wire.reservation, 1e-12, "reserve the accepted Haiku retail contract, not Sonnet's higher catalog price")
				require.NotNil(t, wire.snapshot)
				// The upstream reports 1000 input + 1000 output tokens. Haiku's
				// original retail card totals $0.0015; the account's Sonnet map
				// changes the wire model without changing this default contract.
				quote := gateway.QuoteUsageCost(ctx, &service.RecordUsageInput{PricingSnapshot: wire.snapshot,
					APIKey: key, User: key.User, Account: account,
					Result:             &service.ForwardResult{Model: "claude-3-haiku", UpstreamModel: "claude-sonnet-4", Usage: service.ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}},
					ChannelUsageFields: service.ChannelUsageFields{OriginalModel: "claude-3-haiku", ChannelMappedModel: "claude-3-haiku"}})
				require.InDelta(t, .0015, quote, 1e-12)
				require.Len(t, ledger.commands, 1, "the real handler must settle exactly once")
				require.InDelta(t, .0015, ledger.commands[0].BalanceCost, 1e-12, "the actual ledger must charge the independently specified retail amount")
				require.NotNil(t, ledger.commands[0].UsageLog)
				require.InDelta(t, .0015, ledger.commands[0].UsageLog.ActualCost, 1e-12)
				require.NotNil(t, ledger.commands[0].UsageLog.UpstreamModel)
				require.Equal(t, "claude-sonnet-4", *ledger.commands[0].UsageLog.UpstreamModel)
				balance, err := cache.GetUserBalance(ctx, userID)
				require.NoError(t, err)
				require.InDelta(t, .0045, balance, 1e-12, "the balance cache must reflect only the accepted retail deduction")
				require.Equal(t, int32(1), counts.accountAcquisitions.Load())
				require.Equal(t, int32(1), counts.userAcquisitions.Load())
				require.Equal(t, int32(1), counts.userReleases.Load())
				owners, err := raw.GetAccountConcurrency(ctx, accountID)
				require.NoError(t, err)
				require.Equal(t, 1, owners)
				users, err := raw.GetUserConcurrency(ctx, userID)
				require.NoError(t, err)
				require.Equal(t, 1, users)
				remaining, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89801}").Result()
				require.NoError(t, err)
				require.Equal(t, foreignHolds, remaining, "settlement must release its own hold while preserving the other request")
			})
		}
	}
}
