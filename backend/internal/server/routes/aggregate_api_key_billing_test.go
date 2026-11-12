//go:build unit

package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type aggregateBillingGroups struct {
	service.GroupRepository
	groups []service.Group
}

func (r aggregateBillingGroups) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

type aggregateBillingKeys struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r aggregateBillingKeys) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	copy := *r.key
	return &copy, nil
}
func (aggregateBillingKeys) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type aggregateBillingRates struct {
	service.UserGroupRateRepository
}

func (aggregateBillingRates) GetByUserAndGroup(_ context.Context, userID, groupID int64) (*float64, error) {
	if userID != 8603 {
		return nil, fmt.Errorf("unexpected user")
	}
	rate := 1.5
	if groupID == 8606 {
		rate = .5
	}
	return &rate, nil
}
func (aggregateBillingRates) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

type aggregateBillingAccounts struct{ *compositeBillingAccountRepo }

func (r aggregateBillingAccounts) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, _ bool) ([]service.Account, error) {
	var out []service.Account
	for _, account := range r.accounts {
		for _, binding := range account.AccountGroups {
			if binding.GroupID == *groupID {
				out = append(out, account)
				break
			}
		}
	}
	return out, nil
}

func TestAggregateRealRouteBillsActualGroupPersonalRateAndSharedKey(t *testing.T) {
	// User requirement: native group-1 price $2 × personal1.5 = $3;
	// group-2 price $4 × personal0.5 = $2. Both charges use ONE key and balance,
	// even when the user could otherwise own subscriptions. No subscription
	// repository is provided: any accidental subscription read fails the test.
	for _, mode := range []string{service.GroupBindingAll, service.GroupBindingCustom} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			wireCredentials := make(chan string, 2)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "/v1/chat/completions", r.URL.Path)
				require.Equal(t, "gpt-5.4", gjson.GetBytes(body, "model").String())
				require.NotEmpty(t, r.Header.Get("User-Agent"))
				wireCredentials <- r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"aggregate-bill","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":1000,"total_tokens":2000}}`)
			}))
			defer provider.Close()
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.MaxBodySize = 1 << 20
			cfg.Gateway.TextMaxBodySize = 1 << 20
			price1, price2 := 2.0, 4.0
			groups := []service.Group{
				{ID: 8606, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, SortOrder: 2, RateMultiplier: 9, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"shared-billing"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price2}}},
				{ID: 8601, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, SortOrder: 1, RateMultiplier: 7, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"shared-billing"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price1}}},
			}
			var accountList []service.Account
			for _, group := range groups {
				accountList = append(accountList, service.Account{ID: group.ID + 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10,
					Credentials: map[string]any{"api_key": fmt.Sprint("owner-", group.ID), "base_url": provider.URL, "model_mapping": map[string]any{"shared-billing": "gpt-5.4"}},
					Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}, AccountGroups: []service.AccountGroup{{AccountID: group.ID + 1, GroupID: group.ID}}})
			}
			accounts := aggregateBillingAccounts{&compositeBillingAccountRepo{&globalCapacityRouteAccountRepo{accounts: accountList}}}
			pool, _ := concurrencycache.New(t)
			concurrency := service.NewConcurrencyService(pool)
			concurrency.SetUserRepository(compositeBillingUserRepo{})
			cache := &compositeBillingCache{balance: 100}
			rates := aggregateBillingRates{}
			billingCache := service.NewBillingCacheService(cache, compositeBillingUserRepo{}, nil, nil, nil, rates, cfg, nil)
			defer billingCache.Stop()
			billing := service.NewBillingService(cfg, nil)
			channels := &compositeBillingChannels{}
			channelService := service.NewChannelService(channels, nil, nil, nil, nil)
			usage, ledger := &compositeBillingUsageRepo{}, &compositeBillingLedger{balance: 100}
			gateway := service.NewOpenAIGatewayService(accounts, usage, ledger, compositeBillingUserRepo{}, nil, rates, nil, cfg, nil, concurrency, billing, nil, billingCache, globalCapacityRouteHTTP{provider.Client()}, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channelService, billing), channelService, nil, nil, nil, nil)
			user, _ := (compositeBillingUserRepo{}).GetByID(context.Background(), 8603)
			key := &service.APIKey{ID: 8604, UserID: 8603, Key: "aggregate-bill-fixture", Status: service.StatusActive, GroupBindingMode: mode, User: user, Quota: 20}
			if mode == service.GroupBindingCustom {
				key.AggregateGroupIDs = []int64{8606, 8601}
			}
			apiKeys := service.ProvideAPIKeyService(accounts, channels, aggregateBillingKeys{key: key}, compositeBillingUserRepo{}, aggregateBillingGroups{groups: groups}, nil, rates, nil, cfg, billingCache, concurrency)
			handlers := &handler.Handlers{Gateway: handler.NewGatewayHandler(nil, gateway, nil, nil, nil, concurrency, billingCache, nil, apiKeys, nil, nil, nil, nil, cfg, nil), OpenAIGateway: handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, apiKeys, nil, nil, nil, nil, cfg), AsyncImage: handler.NewAsyncImageHandler(nil, nil)}
			router := gin.New()
			RegisterGatewayRoutes(router, handlers, middleware.NewAPIKeyAuthMiddleware(apiKeys, nil, cfg), apiKeys, nil, nil, nil, service.NewCompositeRouteResolver(nil), cfg)
			modelsRequest := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			modelsRequest.Header.Set("Authorization", "Bearer aggregate-bill-fixture")
			modelsResult := httptest.NewRecorder()
			router.ServeHTTP(modelsResult, modelsRequest)
			require.Equal(t, http.StatusOK, modelsResult.Code, modelsResult.Body.String())
			require.EqualValues(t, 1, gjson.Get(modelsResult.Body.String(), "data.#").Int())
			require.Equal(t, "shared-billing", gjson.Get(modelsResult.Body.String(), "data.0.id").String())
			require.Empty(t, wireCredentials, "model discovery uses local capability data without provider writes")
			for _, test := range []struct {
				hint  string
				group int64
				cost  float64
			}{{"", 8601, 3}, {"8606", 8606, 2}} {
				request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"shared-billing","messages":[{"role":"user","content":"hi"}]}`))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer aggregate-bill-fixture")
				request.Header.Set("X-Composite-Group-ID", test.hint)
				result := httptest.NewRecorder()
				router.ServeHTTP(result, request)
				require.Equal(t, http.StatusOK, result.Code, result.Body.String())
				require.Equal(t, strconv.FormatInt(test.group, 10), result.Header().Get("X-Composite-Group-ID"))
				require.Equal(t, fmt.Sprint("Bearer owner-", test.group), <-wireCredentials)
				ledger.mu.Lock()
				command := *ledger.commands[len(ledger.commands)-1]
				ledger.mu.Unlock()
				require.EqualValues(t, 8604, command.APIKeyID)
				require.EqualValues(t, test.group+1, command.AccountID)
				require.Equal(t, service.BillingTypeBalance, command.BillingType)
				require.Nil(t, command.SubscriptionID)
				require.Zero(t, command.SubscriptionCost)
				require.InDelta(t, test.cost, command.BalanceCost, 1e-8)
				require.InDelta(t, test.cost, command.APIKeyQuotaCost, 1e-8)
			}
			ledger.mu.Lock()
			defer ledger.mu.Unlock()
			require.Len(t, ledger.commands, 2)
			require.InDelta(t, 95, ledger.balance, 1e-8)
			usage.mu.Lock()
			defer usage.mu.Unlock()
			require.Len(t, usage.logs, 2)
			require.EqualValues(t, 8601, *usage.logs[0].GroupID)
			require.EqualValues(t, 8606, *usage.logs[1].GroupID)
			require.Nil(t, key.GroupID, "auth source retains aggregate binding after both requests")
		})
	}
}
