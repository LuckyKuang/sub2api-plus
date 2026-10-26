//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// One physical inference failure must contribute one scheduling outcome, even
// when its observed partial usage must still be charged. Configure the public
// sticky-escape policy so one fresh failure retains A, then make B the preferred
// unbound candidate. A duplicate failure report incorrectly ejects A on the next
// turn. This observes actual handler/scheduler/wire/billing behavior without a
// production test hook or computing expected state using the scheduler itself.
func TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			if os.Getenv("SUB2API_PARTIAL_FAILURE_REVIEW_CHILD") != "1" {
				// The scheduler settings cache is process-wide. Isolate the real
				// configuration from preceding tests rather than waiting for TTL
				// or reaching into private service state.
				command := exec.Command(os.Args[0], "-test.run=^TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill$/^"+protocol+"$", "-test.v", "-test.timeout=30s")
				command.Env = append(os.Environ(), "SUB2API_PARTIAL_FAILURE_REVIEW_CHILD=1")
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "--- PASS: TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill/"+protocol+" (")
				require.NotContains(t, string(output), "--- SKIP:")
				return
			}

			gin.SetMode(gin.TestMode)
			const model = "step-review-chat"
			var callsA, callsB atomic.Int64
			var recovered atomic.Bool
			writeStream := func(w http.ResponseWriter, failed bool) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Connection", "close")
				text := "complete answer"
				if failed {
					text = "partial answer already delivered"
				}
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"chat_step_review\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", model, text)
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
				if failed {
					_, _ = io.WriteString(w, "data: {\"error\":{\"type\":\"upstream_unavailable\",\"message\":\"inference failed after output\"}}\n\n")
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}
			serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				callsA.Add(1)
				require.Equal(t, "/v1/chat/completions", request.URL.Path)
				require.Equal(t, "Bearer partial-review-key-a", request.Header.Get("Authorization"))
				writeStream(w, !recovered.Load())
			}))
			t.Cleanup(serverA.Close)
			serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				callsB.Add(1)
				require.Equal(t, "Bearer partial-review-key-b", request.Header.Get("Authorization"))
				writeStream(w, false)
			}))
			t.Cleanup(serverB.Close)
			groupID := int64(7840)
			repo := &grokCredentialHandlerRepo{accounts: []service.Account{
				{ID: 7841, Name: "review-a", Platform: service.PlatformStepFun, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 3, Priority: 0, GroupIDs: []int64{groupID},
					Credentials: map[string]any{"api_key": "partial-review-key-a", "base_url": serverA.URL, "api_protocol": "chat_completions", "model_mapping": map[string]any{model: model}}},
				{ID: 7842, Name: "review-b", Platform: service.PlatformStepFun, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 3, Priority: 10, GroupIDs: []int64{groupID},
					Credentials: map[string]any{"api_key": "partial-review-key-b", "base_url": serverB.URL, "api_protocol": "chat_completions", "model_mapping": map[string]any{model: model}}},
			}}
			bindings := &sevenProviderBindings{bindings: map[sevenProviderSessionKey]int64{}}
			slots := &sevenProviderSlots{grokMediaSlotsCache: &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}}
			concurrency := service.NewConcurrencyService(slots)
			cfg := &config.Config{}
			cfg.Gateway.MaxAccountSwitches = 2
			cfg.Gateway.Scheduling.LoadBatchEnabled = false
			cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
			cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.30
			cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 60000
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Default.RateMultiplier = 1
			settings := service.NewSettingService(&oauthCaptchaSettingRepo{values: map[string]string{
				"openai_advanced_scheduler_enabled":             "true",
				service.SettingKeyOpenAIAdvancedSchedulerLBTopK: "1",
			}}, cfg)
			rateLimits := service.NewRateLimitService(repo, nil, cfg, nil, nil)
			rateLimits.SetSettingService(settings)
			billingCache := service.NewBillingCacheService(&sevenProviderBalanceCache{newHandlerInflightCache(100)}, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			billingRepo := &sevenProviderBillingRepo{}
			inputPrice, outputPrice := 0.000002, 0.000004
			channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
				groupPlatforms: map[int64]string{groupID: service.PlatformStepFun},
				channels: []service.Channel{{ID: 7860, Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []service.ChannelModelPricing{{
					Platform: service.PlatformStepFun, Models: []string{model}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice,
				}}}},
			}, nil, nil, nil, nil)
			billingService := service.NewBillingService(cfg, nil)
			gateway := service.NewOpenAIGatewayService(repo, nil, billingRepo, nil, nil, nil, bindings, cfg, nil, concurrency,
				billingService, rateLimits, billingCache, repository.NewHTTPUpstream(cfg), &service.DeferredService{}, nil, nil,
				service.NewModelPricingResolver(channels, billingService), channels, nil, nil, nil, nil)
			handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			attachDisabledIPAccessControlForWebSocketTest(handler)
			group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformStepFun, Status: service.StatusActive, RateMultiplier: 1.5}
			apiKey := &service.APIKey{ID: 7870, UserID: 7871, GroupID: &groupID, Group: group, Status: service.StatusActive,
				User: &service.User{ID: 7871, Status: service.StatusActive, Balance: 100}}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), apiKey)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 2})
				c.Next()
			})
			path := map[string]string{"chat": "/v1/chat/completions", "responses": "/v1/responses", "messages": "/v1/messages"}[protocol]
			switch protocol {
			case "responses":
				router.POST(path, handler.Responses)
			case "messages":
				router.POST(path, handler.Messages)
			default:
				router.POST(path, handler.ChatCompletions)
			}
			request := func() *httptest.ResponseRecorder {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				body := fmt.Sprintf(`{"model":%q,"stream":true,"max_tokens":64,"messages":[{"role":"user","content":"continue this business conversation"}]}`, model)
				if protocol == "responses" {
					body = fmt.Sprintf(`{"model":%q,"stream":true,"input":"continue this business conversation"}`, model)
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("session_id", "partial-failure-review-"+protocol)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				return recorder
			}
			assertBills := func(count int, complete bool) {
				billingRepo.mu.Lock()
				defer billingRepo.mu.Unlock()
				require.Len(t, billingRepo.commands, count, "one physical response creates one billing command")
				command := billingRepo.commands[count-1]
				require.Equal(t, int64(7841), command.AccountID)
				require.Equal(t, 10, command.InputTokens)
				require.Equal(t, 5, command.OutputTokens)
				require.InDelta(t, 0.00006000, command.BalanceCost, 1e-12)
				require.NotNil(t, command.UsageLog)
				require.NotNil(t, command.UsageLog.IsComplete)
				require.Equal(t, complete, *command.UsageLog.IsComplete)
				wantSource := service.UsageSourcePartial
				if complete {
					wantSource = service.UsageSourceUpstreamExact
				}
				require.Equal(t, wantSource, command.UsageLog.UsageSource)
			}

			first := request()
			require.Equal(t, http.StatusOK, first.Code, first.Body.String())
			require.Contains(t, first.Body.String(), "partial answer already delivered")
			require.NotContains(t, first.Body.String(), `"status":"completed"`)
			require.NotContains(t, first.Body.String(), `"type":"message_stop"`)
			require.Equal(t, int64(1), callsA.Load())
			require.Zero(t, callsB.Load(), "partial output forbids replay on another account")
			assertBills(1, false)
			slots.assertReleased(t)
			require.Equal(t, int64(1), gateway.SnapshotOpenAIAccountSchedulerMetrics().SelectTotal, "the actual advanced scheduler must be enabled")

			recovered.Store(true)
			repo.mu.Lock()
			repo.accounts[0].Priority, repo.accounts[1].Priority = 10, 0
			repo.mu.Unlock()
			second := request()
			require.Equal(t, http.StatusOK, second.Code, second.Body.String())
			require.Contains(t, second.Body.String(), "complete answer")
			if protocol == "messages" {
				require.Contains(t, second.Body.String(), `"type":"message_stop"`)
			} else {
				require.Contains(t, second.Body.String(), "data: [DONE]")
			}
			require.Equal(t, int64(2), callsA.Load(), "one failed attempt must not count twice and eject the sticky owner")
			require.Zero(t, callsB.Load(), "a healthy alternate cannot steal the retained conversation")
			assertBills(2, true)
			billingRepo.mu.Lock()
			require.NotEmpty(t, billingRepo.commands[0].RequestID)
			require.NotEqual(t, billingRepo.commands[0].RequestID, billingRepo.commands[1].RequestID)
			billingRepo.mu.Unlock()
			slots.assertReleased(t)
			require.Equal(t, int64(1), gateway.SnapshotOpenAIAccountSchedulerMetrics().StickySessionHitTotal)
			slots.pairsMu.Lock()
			defer slots.pairsMu.Unlock()
			require.False(t, slots.wrongOwnerRelease)
			require.Len(t, slots.accountAcquired, 2)
			require.ElementsMatch(t, slots.accountAcquired, slots.accountReleased)
			require.ElementsMatch(t, slots.userSlotAcquired, slots.userSlotReleased)
		})
	}
}
