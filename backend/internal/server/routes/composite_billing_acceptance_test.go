//go:build unit

package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const compositeBillingPublicModel = "model001"

type compositeBillingAccountRepo struct {
	*globalCapacityRouteAccountRepo
}

func (r *compositeBillingAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	accounts, err := r.ListSchedulableByPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	var found []service.Account
	for _, account := range accounts {
		for _, binding := range account.AccountGroups {
			if binding.GroupID == groupID {
				found = append(found, account)
				break
			}
		}
	}
	return found, nil
}
func (*compositeBillingAccountRepo) UpdateLastUsed(context.Context, int64) error { return nil }

type compositeBillingRoutes struct {
	service.CompositeModelRouteRepository
	endpoint string
}

func (r *compositeBillingRoutes) ListByGroup(context.Context, int64, bool) ([]service.CompositeModelRoute, error) {
	endpoint := r.endpoint
	if endpoint == "" {
		endpoint = service.CompositeRouteEndpointChatCompletions
	}
	return []service.CompositeModelRoute{{GroupID: 8601, PublicModel: compositeBillingPublicModel, MatchType: service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformOpenAI, Endpoint: endpoint, UpstreamModel: "vendor-model", Enabled: true}}, nil
}

type compositeBillingChannels struct {
	service.ChannelRepository
	channels []service.Channel
}

func (r *compositeBillingChannels) ListAll(context.Context) ([]service.Channel, error) {
	return r.channels, nil
}
func (*compositeBillingChannels) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{8601: service.PlatformComposite}, nil
}

type compositeBillingUserRepo struct{ service.UserRepository }

func (compositeBillingUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 8603, Status: service.StatusActive, Balance: 100, Concurrency: 3}, nil
}

type compositeBillingCache struct {
	service.BillingCache
	mu      sync.Mutex
	balance float64
}

func (c *compositeBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.balance, nil
}
func (c *compositeBillingCache) SetUserBalance(_ context.Context, _ int64, balance float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.balance = balance
	return nil
}
func (c *compositeBillingCache) DeductUserBalance(_ context.Context, _ int64, amount float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.balance -= amount
	return nil
}
func (*compositeBillingCache) InvalidateUserBalance(context.Context, int64) error { return nil }
func (*compositeBillingCache) GetUserPlatformQuotaCache(context.Context, int64, string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

type compositeBillingUsageRepo struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []*service.UsageLog
}

func (r *compositeBillingUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *log
	r.logs = append(r.logs, &copy)
	return true, nil
}

type compositeBillingLedger struct {
	service.UsageBillingRepository
	mu       sync.Mutex
	balance  float64
	commands []*service.UsageBillingCommand
}

func (r *compositeBillingLedger) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *command
	r.commands = append(r.commands, &copy)
	r.balance -= command.BalanceCost
	newBalance := r.balance
	return &service.UsageBillingApplyResult{Applied: true, UsageLogPersisted: false, NewBalance: &newBalance}, nil
}

func TestCompositeChatBillingUsesPublicPriceAndIndependentFactorsThroughRealRoute(t *testing.T) {
	// Literal business expectations: a $2/request public offer at platform2 ×
	// model3 costs $12, or $18 at peak1.5. Mapping the same public offer twice
	// must not replace its price/rate identity with a known upstream GPT card.
	// An explicit channel_mapped source instead selects its actual $5 card,
	// retaining the public model's factors for a literal $30 charge.
	// Embeddings use the input-only $2/M card: 1000 input × P2 × M3 = $.012.
	for _, scenario := range []struct {
		name, card                           string
		peak, noModel                        bool
		wantBase, wantActual, wantMultiplier float64
	}{
		{"public-group-card-overrides-known-upstream", "group", false, false, 2, 12, 6},
		{"plain-openai-group-public-card-overrides-known-upstream", "group", false, false, 2, 12, 6},
		{"public-group-card-with-peak", "group", true, false, 2, 18, 9},
		{"no-group-card-inherits-public-channel", "channel", false, false, 2, 12, 6},
		{"empty-group-card-inherits-public-channel", "empty-group", false, false, 2, 12, 6},
		{"no-custom-card-inherits-known-built-in", "builtin", false, false, 0.0175, 0.105, 6},
		{"missing-model-factor-defaults-to-one", "group", false, true, 2, 4, 2},
		{"actual-channel-mapping-retains-selected-card", "channel-mapped", false, false, 5, 30, 6},
		{"embeddings-input-only-card-without-output-price", "embeddings-input-only", false, false, .002, .012, 6},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			embeddings := scenario.card == "embeddings-input-only"
			endpoint := service.CompositeRouteEndpointChatCompletions
			path := "/v1/chat/completions"
			wireModel := "gpt-5.4"
			if embeddings {
				endpoint, path, wireModel = service.CompositeRouteEndpointEmbeddings, "/v1/embeddings", "text-embedding-3-small"
			}
			wireBodies := make(chan []byte, 2)
			wireCredentials := make(chan string, 2)
			var wireCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read failed", http.StatusBadRequest)
					return
				}
				wireCalls.Add(1)
				select {
				case wireBodies <- body:
				default:
				}
				select {
				case wireCredentials <- r.Header.Get("Authorization"):
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", "billing-acceptance")
				if embeddings {
					if r.URL.Path != "/v1/embeddings" {
						http.Error(w, "expected embeddings endpoint", http.StatusBadRequest)
						return
					}
					_, _ = io.WriteString(w, `{"object":"list","model":"text-embedding-3-small","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1000,"total_tokens":1000}}`)
					return
				}
				_, _ = io.WriteString(w, `{"id":"billing_acceptance","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":1000,"total_tokens":2000}}`)
			}))
			defer provider.Close()
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.MaxBodySize, cfg.Gateway.TextMaxBodySize = 1<<20, 1<<20
			if embeddings {
				// Exercise the production estimator as well as settlement. An
				// embeddings request must not require an imaginary output price.
				cfg.Billing.InflightReservation.Enabled = true
				cfg.Billing.InflightReservation.FailClosedOnUnpriced = true
			}
			group := &service.Group{ID: 8601, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, BillingPolicyVersion: 4,
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 2, service.PlatformGrok: 88},
					ModelMultipliers: []domain.BillingModelRate{{Platform: service.PlatformGrok, Model: compositeBillingPublicModel, Multiplier: 99},
						{Platform: service.PlatformOpenAI, Model: "vendor-model", Multiplier: 99}, {Platform: service.PlatformOpenAI, Model: "gpt-5.4", Multiplier: 99}}}}
			accountAlias := "vendor-model"
			if scenario.name == "plain-openai-group-public-card-overrides-known-upstream" {
				group.Platform = service.PlatformOpenAI
				accountAlias = compositeBillingPublicModel
			}
			if !scenario.noModel {
				group.BillingRatePolicy.ModelMultipliers = append(group.BillingRatePolicy.ModelMultipliers, domain.BillingModelRate{Platform: service.PlatformOpenAI, Model: compositeBillingPublicModel, Multiplier: 3})
			}
			price := 2.0
			card := service.ChannelModelPricing{Platform: service.PlatformOpenAI, Models: []string{compositeBillingPublicModel}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}
			channels := &compositeBillingChannels{}
			if scenario.card == "group" || scenario.card == "channel-mapped" {
				group.ModelPricing = []service.ChannelModelPricing{card}
			}
			if scenario.card == "channel" || scenario.card == "empty-group" {
				channels.channels = []service.Channel{{ID: 8605, Name: "public-offer", Status: service.StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []service.ChannelModelPricing{card}}}
			}
			if scenario.card == "empty-group" {
				group.ModelPricing = []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{compositeBillingPublicModel}, BillingMode: service.BillingModeToken}}
			}
			if embeddings {
				inputPrice := 2e-6
				group.ModelPricing = []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{compositeBillingPublicModel}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice}}
				group.BillingRatePolicy.ModelMultipliers = append(group.BillingRatePolicy.ModelMultipliers,
					domain.BillingModelRate{Platform: service.PlatformOpenAI, Model: wireModel, Multiplier: 99})
			}
			if scenario.card == "channel-mapped" {
				channelPrice := 5.0
				channels.channels = []service.Channel{{ID: 8605, Name: "channel-selected-offer", Status: service.StatusActive, GroupIDs: []int64{group.ID},
					BillingModelSource: service.BillingModelSourceChannelMapped,
					ModelMapping:       map[string]map[string]string{service.PlatformOpenAI: {"vendor-model": "bill-model"}},
					ModelPricing:       []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"bill-model"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &channelPrice}}}}
				accountAlias = "bill-model"
				group.BillingRatePolicy.ModelMultipliers = append(group.BillingRatePolicy.ModelMultipliers,
					domain.BillingModelRate{Platform: service.PlatformOpenAI, Model: "bill-model", Multiplier: 99})
			}
			at := time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location())
			if scenario.peak {
				group.SubscriptionType = service.SubscriptionTypeSubscription
				group.PeakRateEnabled = true
				group.PeakStart = "10:00"
				group.PeakEnd = "11:00"
				group.PeakRateMultiplier = 1.5
			}
			account := service.Account{ID: 8602, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10,
				Credentials: map[string]any{"api_key": "billing-owner-token", "base_url": provider.URL, "model_mapping": map[string]any{accountAlias: wireModel}},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}, AccountGroups: []service.AccountGroup{{AccountID: 8602, GroupID: group.ID}}}
			accounts := &compositeBillingAccountRepo{&globalCapacityRouteAccountRepo{accounts: []service.Account{account}}}
			pool, _ := concurrencycache.New(t)
			concurrency := service.NewConcurrencyService(pool)
			concurrency.SetUserRepository(compositeBillingUserRepo{})
			cache := &compositeBillingCache{balance: 100}
			billingCache := service.NewBillingCacheService(cache, compositeBillingUserRepo{}, nil, nil, nil, nil, cfg, nil)
			defer billingCache.Stop()
			billing := service.NewBillingService(cfg, nil)
			channelService := service.NewChannelService(channels, nil, nil, nil, nil)
			usage, ledger := &compositeBillingUsageRepo{}, &compositeBillingLedger{balance: 100}
			gateway := service.NewOpenAIGatewayService(accounts, usage, ledger, compositeBillingUserRepo{}, nil, nil, nil, cfg, nil, concurrency,
				billing, nil, billingCache, globalCapacityRouteHTTP{provider.Client()}, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channelService, billing), channelService, nil, nil, nil, nil)
			key := &service.APIKey{ID: 8604, UserID: 8603, GroupID: &group.ID, Group: group, User: &service.User{ID: 8603, Status: service.StatusActive, Balance: 100, Concurrency: 3}}
			handlers := &handler.Handlers{Gateway: handler.NewGatewayHandler(nil, gateway, nil, nil, nil, concurrency, billingCache, nil, &service.APIKeyService{}, nil, nil, nil, nil, cfg, nil),
				OpenAIGateway: handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg), AsyncImage: handler.NewAsyncImageHandler(nil, nil)}
			router := gin.New()
			RegisterGatewayRoutes(router, handlers, middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 99})
				ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
				ctx = service.WithResolvedTargetPlatform(ctx, service.PlatformOpenAI)
				if scenario.peak {
					// Freeze only the peak clock case. All other requests capture
					// their snapshot in the production audit/admission path after
					// the actual Composite route has resolved.
					ctx = gateway.WithBillingPricingSnapshot(ctx, key, at, compositeBillingPublicModel)
				}
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			}), nil, nil, nil, nil, service.NewCompositeRouteResolver(&compositeBillingRoutes{endpoint: endpoint}), cfg)
			server := httptest.NewServer(router)
			defer server.Close()
			client := server.Client()
			client.Timeout = 3 * time.Second
			requestBody := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, compositeBillingPublicModel)
			if embeddings {
				requestBody = fmt.Sprintf(`{"model":%q,"input":"hello"}`, compositeBillingPublicModel)
			}
			response, err := client.Post(server.URL+path, "application/json", strings.NewReader(requestBody))
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			responseBody, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode, string(responseBody))
			require.EqualValues(t, 1, wireCalls.Load(), "one accepted request must make exactly one provider call")
			select {
			case sent := <-wireBodies:
				require.Equal(t, wireModel, gjson.GetBytes(sent, "model").String(), "Composite route and account mapping must both execute")
			default:
				t.Fatal("real provider received no request")
			}
			require.Equal(t, "Bearer billing-owner-token", <-wireCredentials)
			select {
			case extra := <-wireBodies:
				t.Fatalf("one accepted request sent twice: %s", extra)
			default:
			}
			ledger.mu.Lock()
			commands := append([]*service.UsageBillingCommand(nil), ledger.commands...)
			remaining := ledger.balance
			ledger.mu.Unlock()
			require.Len(t, commands, 1, "normal mode must apply one durable money command")
			command := commands[0]
			require.EqualValues(t, 8603, command.UserID)
			require.EqualValues(t, 8602, command.AccountID)
			require.Equal(t, service.BillingTypeBalance, command.BillingType, "peak schedule does not require consuming a subscription")
			require.InDelta(t, scenario.wantActual, command.BalanceCost, 1e-8)
			require.Zero(t, command.SubscriptionCost)
			require.InDelta(t, 100-scenario.wantActual, remaining, 1e-8)
			usage.mu.Lock()
			logs := append([]*service.UsageLog(nil), usage.logs...)
			usage.mu.Unlock()
			require.Len(t, logs, 1)
			log := logs[0]
			if embeddings {
				require.Equal(t, 1000, log.InputTokens)
				require.Zero(t, log.OutputTokens, "an embeddings response must not invent output usage")
			}
			require.InDelta(t, scenario.wantBase, log.TotalCost, 1e-8)
			require.InDelta(t, scenario.wantActual, log.ActualCost, 1e-8)
			require.InDelta(t, scenario.wantMultiplier, log.RateMultiplier, 1e-12)
			require.Equal(t, compositeBillingPublicModel, log.BillingRateSnapshot["model"])
			require.Equal(t, service.PlatformOpenAI, log.BillingRateSnapshot["platform"])
			require.Equal(t, 2.0, log.BillingRateSnapshot["platform_multiplier"])
			wantModel := 3.0
			if scenario.noModel {
				wantModel = 1
			}
			require.Equal(t, wantModel, log.BillingRateSnapshot["model_multiplier"])
			wantUnit := "request"
			if scenario.card == "builtin" || embeddings {
				wantUnit = "token"
			}
			require.Equal(t, wantUnit, log.BillingRateSnapshot["billing_unit"])
			count, err := pool.GetUserConcurrency(context.Background(), key.UserID)
			require.NoError(t, err)
			require.Zero(t, count)
			accountCount, err := pool.GetAccountConcurrency(context.Background(), account.ID)
			require.NoError(t, err)
			require.Zero(t, accountCount)
		})
	}
}
