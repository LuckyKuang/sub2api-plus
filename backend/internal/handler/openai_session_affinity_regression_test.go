//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type handlerAffinityRouteStore struct {
	service.OpenAISessionRouteStore
	err error
}

func (s handlerAffinityRouteStore) Get(_ context.Context, domain, digest string) (service.OpenAISessionRoute, error) {
	return service.OpenAISessionRoute{AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 101, Generation: 1, Status: service.OpenAISessionRouteActive}, s.err
}

// V01/V04: run actual HTTP handlers through canonical scheduling and bounded
// concurrency waiting. An idle B must never receive the busy A's conversation;
// route read failures must stop before account concurrency or upstream writes.
func TestCodexCanonicalHandlerForbiddenSideEffectsRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"busy", "store-failure"} {
		for _, endpoint := range []string{"responses", "chat/completions", "messages"} {
			t.Run(scenario+"/"+endpoint, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
				cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
				var acquired []int64
				cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(_ context.Context, id int64, _ int, _ string) (bool, error) {
					acquired = append(acquired, id)
					return id == 102, nil
				}}
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
					{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 100, Credentials: map[string]any{"api_key": "a"}},
					{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, Credentials: map[string]any{"api_key": "b"}},
				}}
				upstream := &openAIClientProfileFallbackUpstream{body: `{"id":"resp_b","output":[{"type":"message"}],"usage":{"input_tokens":1,"output_tokens":1}}`}
				store := handlerAffinityRouteStore{}
				if scenario == "store-failure" {
					store.err = service.ErrOpenAISessionRouteStoreUnavailable
				}
				concurrency := service.NewConcurrencyService(cache)
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, store)
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				body := `{"model":"gpt-5.1","input":"hello","stream":false}`
				if endpoint != "responses" {
					body = `{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"stream":false}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
				c.Request.Header.Set("session-id", "session")
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
				switch endpoint {
				case "responses":
					h.Responses(c)
				case "messages":
					h.Messages(c)
				default:
					h.ChatCompletions(c)
				}
				require.GreaterOrEqual(t, recorder.Code, 400, recorder.Body.String())
				require.Empty(t, upstream.calls())
				if scenario == "store-failure" {
					require.Empty(t, acquired)
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
					require.Contains(t, recorder.Body.String(), "session_route_unavailable")
				} else {
					require.NotEmpty(t, acquired)
					for _, id := range acquired {
						require.EqualValues(t, 101, id)
					}
				}
			})
		}
	}
}

// Admission can lease a candidate after waiting for its slot. A failure before
// Forward (for example credential retrieval) must release that lease too.
type handlerAffinityMigrationStore struct {
	service.OpenAISessionRouteStore
	route     service.OpenAISessionRoute
	abandoned int
}

func (s *handlerAffinityMigrationStore) Get(context.Context, string, string) (service.OpenAISessionRoute, error) {
	return s.route, nil
}
func (s *handlerAffinityMigrationStore) BeginMigration(_ context.Context, _, _ string, generation, candidate int64, lease, reason string, ttl time.Duration) (service.OpenAISessionRoute, error) {
	if generation != s.route.Generation || s.route.LeaseID != "" {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	s.route.CandidateAccountID, s.route.LeaseID, s.route.LeaseExpiresAt = candidate, lease, time.Now().Add(ttl)
	return s.route, nil
}
func (s *handlerAffinityMigrationStore) AbandonMigration(_ context.Context, _, _, lease string) error {
	if s.route.LeaseID == lease {
		s.route.LeaseID = ""
		s.route.CandidateAccountID = 0
		s.abandoned++
	}
	return nil
}
func TestCodexWaitAdmissionReleaseAbandonsMigrationRegression(t *testing.T) {
	for _, acquiresBeforeSuccess := range []int{1, 2} {
		t.Run(string(rune('0'+acquiresBeforeSuccess)), func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.Scheduling.StickySessionWaitTimeout = time.Second
			cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
			cfg.Gateway.Scheduling.FallbackWaitTimeout = time.Second
			cfg.Gateway.Scheduling.FallbackMaxWaiting = 2
			calls := 0
			cache := &concurrencyCacheMock{acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
				calls++
				return calls > acquiresBeforeSuccess, nil
			}}
			concurrency := service.NewConcurrencyService(cache)
			repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
				{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusError},
				{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1},
			}}
			store := &handlerAffinityMigrationStore{route: service.OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: service.OpenAISessionRouteDigest("session"), OwnerAccountID: 101, Generation: 1}}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, store)
			h := NewOpenAIGatewayHandler(gateway, concurrency, nil, nil, nil, nil, nil, nil, cfg)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			selected, _, err := gateway.SelectAccountWithScheduler(c.Request.Context(), nil, "", "session", "gpt-5.1", nil, service.OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.False(t, selected.Acquired)
			var started bool
			release, status := h.acquireOpenAIAccountSlot(c, nil, "session", selected, false, &started, zap.NewNop(), nil)
			require.Equal(t, openAISlotAcquireOK, status)
			require.NotEmpty(t, store.route.LeaseID)
			release()
			require.Equal(t, 1, store.abandoned)
			require.Empty(t, store.route.LeaseID)
			require.EqualValues(t, 101, store.route.OwnerAccountID)
		})
	}
}

type handlerAffinity429Wire struct {
	service.HTTPUpstream
	ids []int64
}

func (w *handlerAffinity429Wire) Do(req *http.Request, _ string, owner int64, _ int) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	w.ids = append(w.ids, owner)
	if len(w.ids) == 1 {
		return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_exceeded","message":"temporary rate limit"}}`))}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_a\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}, nil
}

// V06: a plain native API key gets bounded same-owner recovery for transient
// throttling too. The real rate-limit service may not park it before retry.
func TestCodexAPIKeyTransient429HandlerRetriesOwnerRegression(t *testing.T) {
	for _, endpoint := range []string{"responses", "chat/completions", "messages"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			repo := &grokCredentialHandlerRepo{accounts: []service.Account{
				{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 100, Credentials: map[string]any{"api_key": "a"}},
				{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, Credentials: map[string]any{"api_key": "b"}},
			}}
			concurrency := service.NewConcurrencyService(&concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }})
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			wire := &handlerAffinity429Wire{}
			rate := service.NewRateLimitService(repo, nil, cfg, nil, nil)
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), rate, billing, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, handlerAffinityRouteStore{})
			h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			record := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(record)
			body := `{"model":"gpt-5.1","input":"hello","stream":false}`
			if endpoint != "responses" {
				body = `{"model":"gpt-5.1","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
			c.Request.Header.Set("session-id", "session")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
			switch endpoint {
			case "responses":
				h.Responses(c)
			case "messages":
				h.Messages(c)
			default:
				h.ChatCompletions(c)
			}
			require.Equal(t, 200, record.Code, record.Body.String())
			require.Equal(t, []int64{101, 101}, wire.ids)
			require.Empty(t, repo.rateLimitIDs, "recoverable throttling must not make the pinned retry unschedulable")
			require.Empty(t, repo.setTempIDs)
			require.Empty(t, repo.setErrorIDs)
		})
	}
}
