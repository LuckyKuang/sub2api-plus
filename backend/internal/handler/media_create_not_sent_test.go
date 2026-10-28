//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mediaCreateNotSentCapacityRepo struct {
	testutil.StubGatewayUnfinishedTaskRepo
	markCalls int
}

func (r *mediaCreateNotSentCapacityRepo) MarkUncertain(ctx context.Context, reservation *service.GatewayTaskReservation, reason string) error {
	r.markCalls++
	return r.StubGatewayUnfinishedTaskRepo.MarkUncertain(ctx, reservation, reason)
}

// Exercise the actual scheduler, forwarding services and shared lifecycle
// handler. A preparation failure after create intent was persisted must not
// consume capacity forever; a transport timeout must keep it occupied.
func TestMediaCreateNotSentReleasesCapacityAndSentUnknownRetainsIt(t *testing.T) {
	for _, supplier := range []string{"grok", "seedance"} {
		for _, scenario := range []string{"invalid_base_url", "missing_credential", "sent_timeout"} {
			t.Run(supplier+"/"+scenario, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				account := service.Account{ID: 1, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{24},
					Credentials: map[string]any{"api_key": "isolated-secret", "base_url": "https://relay.example/v1"}}
				if supplier == "seedance" {
					account.Platform = service.PlatformOpenAI
					account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/v3"
					account.Credentials["openai_capabilities"] = []string{"seedance"}
				}
				if scenario == "invalid_base_url" {
					account.Credentials["base_url"] = "file:///rejected-provider-base"
				}
				if scenario == "missing_credential" {
					delete(account.Credentials, "api_key")
				}
				slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
				concurrency := service.NewConcurrencyService(slots)
				bindings := &grokMediaSlotBindings{}
				upstream := &grokMediaSlotUpstream{call: func(r *http.Request, accountID int64) (*http.Response, error) {
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, int64(1), accountID)
					return nil, context.DeadlineExceeded
				}}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Gateway.MaxUnfinishedTasksPerUser = 1
				repo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, service.NewGrokTokenProvider(repo, nil), nil, nil, nil, nil, nil, nil)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				capacity := &mediaCreateNotSentCapacityRepo{}
				h.unfinishedTasks = service.NewGatewayUnfinishedTaskService(capacity, cfg)
				c, response := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				key.Group.RateMultiplier = 1
				if supplier == "seedance" {
					key.Group.Platform = service.PlatformOpenAI
					key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
					c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`))
					c.Request.Header.Set("Content-Type", "application/json")
					h.SeedanceTasks(c)
				} else {
					h.GrokVideoGeneration(c)
				}
				require.Equal(t, 1, capacity.markCalls, "the test must reach forwarding after durable create intent, rather than fail during earlier validation")
				require.NotContains(t, response.Body.String(), "isolated-secret")
				require.NotContains(t, response.Body.String(), "file:///")
				require.Zero(t, bindings.writes, "failed creates cannot publish task/session ownership")
				slots.assertReleased(t)
				_, nextErr := h.unfinishedTasks.Reserve(context.Background(), service.GatewayTaskOwner{UserID: 10, APIKeyID: 21}, service.GatewayTaskKindBatchImage)
				if scenario == "sent_timeout" {
					require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
					require.Equal(t, 1, upstream.calls, "an unknown create cannot automatically retry another account")
					require.Zero(t, capacity.ReleaseCalls)
					require.Len(t, capacity.Uncertain, 1)
					require.ErrorIs(t, nextErr, service.ErrGatewayUnfinishedTaskLimit)
				} else {
					expectedStatus := http.StatusBadGateway
					if supplier == "grok" && scenario == "missing_credential" {
						expectedStatus = http.StatusServiceUnavailable
					}
					require.Equal(t, expectedStatus, response.Code, response.Body.String())
					require.Zero(t, upstream.calls, "invalid local preparation cannot send a provider create")
					require.Equal(t, 1, capacity.ReleaseCalls, "explicit no-send evidence releases the retained create intent once")
					require.NoError(t, nextErr, "another key can use the user's capacity when no upstream task exists")
				}
			})
		}
	}
}
