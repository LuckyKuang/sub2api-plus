//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type handlerScopeUnavailableStore struct {
	service.OpenAISessionRouteStore
	reads int
}

func (s *handlerScopeUnavailableStore) Get(context.Context, string, string) (service.OpenAISessionRoute, error) {
	s.reads++
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteStoreUnavailable
}

// CODEX_CLIENT_PROFILES.md: token counting is an auxiliary request, so a
// stable session signal cannot turn it into a durable owner or migration.
// Exercise both real handlers; CountTokens uses the Chat scheduling API.
func TestSessionRouteScopeTokenCountHandlersRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages/count_tokens", "responses/input_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			slots := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			}
			concurrency := service.NewConcurrencyService(slots)
			repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
				{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://compat.example"}, Extra: map[string]any{"responses_supported": true}},
			}}
			store := &handlerScopeUnavailableStore{}
			wire := &openAIClientProfileFallbackUpstream{body: `{"input_tokens":5}`}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, store)
			h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			body := `{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}]}`
			if endpoint == "responses/input_tokens" {
				body = `{"model":"gpt-5.1","input":"hello"}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
			c.Request.Header.Set("session-id", "stable-session")
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
			if endpoint == "responses/input_tokens" {
				h.ResponsesInputTokens(c)
			} else {
				h.CountTokens(c)
				require.Equal(t, []int64{101}, wire.calls())
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Positive(t, gjson.GetBytes(response.Body.Bytes(), "input_tokens").Int())
			require.Zero(t, store.reads, "auxiliary requests must succeed independently of the conversation route store")
			require.Equal(t, int32(1), atomic.LoadInt32(&slots.releaseUserCalled), "auxiliary requests still acquire and release the global user lease")
		})
	}
}
