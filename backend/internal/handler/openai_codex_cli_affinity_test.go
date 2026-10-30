//go:build unit

package handler

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type codexCLIConversationStore struct {
	service.OpenAISessionRouteStore
	reads  atomic.Int32
	writes atomic.Int32
}

func (s *codexCLIConversationStore) Get(context.Context, string, string) (service.OpenAISessionRoute, error) {
	s.reads.Add(1)
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteStoreUnavailable
}

func (s *codexCLIConversationStore) CreateIfAbsent(context.Context, service.OpenAISessionRoute) (service.OpenAISessionRoute, error) {
	s.writes.Add(1)
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteStoreUnavailable
}

func (s *codexCLIConversationStore) BeginMigration(context.Context, string, string, int64, int64, string, string, time.Duration) (service.OpenAISessionRoute, error) {
	s.writes.Add(1)
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteStoreUnavailable
}

func (s *codexCLIConversationStore) CommitOwner(context.Context, string, string, string, int64) (service.OpenAISessionRoute, error) {
	s.writes.Add(1)
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteStoreUnavailable
}

func (s *codexCLIConversationStore) AbandonMigration(context.Context, string, string, string) error {
	s.writes.Add(1)
	return service.ErrOpenAISessionRouteStoreUnavailable
}

type codexCLIConversationCache struct {
	service.GatewayCache
	reads  atomic.Int32
	writes atomic.Int32
}

func (s *codexCLIConversationCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	s.reads.Add(1)
	return 0, service.ErrStickySessionNotFound
}

func (s *codexCLIConversationCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	s.writes.Add(1)
	return nil
}

func (s *codexCLIConversationCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	s.writes.Add(1)
	return nil
}

func (s *codexCLIConversationCache) DeleteSessionAccountID(context.Context, int64, string) error {
	s.writes.Add(1)
	return nil
}

// Independent exec tasks have no conversation continuation. Caller session
// headers and content-derived affinity must never borrow or alter a Responses
// or WebSocket owner's durable route or legacy cache, including during outages.
// The ordinary Responses control must still fail closed on a route-store error.
func TestCodexCLIResponsesDoesNotReadOrWriteConversationAffinity(t *testing.T) {
	for _, header := range []string{"session-id", "session_id", "conversation_id", "x-session-affinity", ""} {
		for _, durable := range []bool{true, false} {
			name := header
			if name == "" {
				name = "content-fallback"
			}
			if durable {
				name += "/durable"
			} else {
				name += "/cache-only"
			}
			t.Run(name, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				slots := &concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}
				concurrency := service.NewConcurrencyService(slots)
				repository := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
					{ID: 1813, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture-access"}},
				}}
				cache, routes := &codexCLIConversationCache{}, &codexCLIConversationStore{}
				var routeStore service.OpenAISessionRouteStore
				if durable {
					routeStore = routes
				}
				wire := &openAIClientProfileFallbackUpstream{}
				gateway := service.NewOpenAIGatewayService(repository, nil, nil, nil, nil, nil, cache, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, routeStore)
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
				c, response := codexCLIIngressContext(`{"model":"gpt-5.1","input":"independent task"}`, true)
				ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				if header != "" {
					c.Request.Header.Set(header, "existing-conversation")
				}
				h.CodexCLIResponses(c)
				require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "cli_account_unavailable", "an unrelated conversation-store outage must not replace CLI account admission")
				require.Zero(t, routes.reads.Load())
				require.Zero(t, routes.writes.Load(), "no lease can be created or renewed for an independent task")
				require.Zero(t, cache.reads.Load())
				require.Zero(t, cache.writes.Load(), "CLI selection cannot poison legacy conversation affinity")
				require.Equal(t, int32(1), atomic.LoadInt32(&slots.releaseAccountCalled), "selection must reach and release the ineligible CLI account")
				require.Empty(t, wire.calls())

				if durable {
					ordinary, ordinaryResponse := codexCLIIngressContext(`{"model":"gpt-5.1","input":"ordinary turn"}`, true)
					ordinary.Request.Header.Set("session-id", "existing-conversation")
					h.Responses(ordinary)
					require.Equal(t, http.StatusServiceUnavailable, ordinaryResponse.Code, ordinaryResponse.Body.String())
					require.Contains(t, ordinaryResponse.Body.String(), "session_route_unavailable")
					require.Positive(t, routes.reads.Load(), "ordinary Responses must retain durable conversation fencing")
				}
			})
		}
	}
}
