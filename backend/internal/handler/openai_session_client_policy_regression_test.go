//go:build unit || !integration

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These scenarios never authorize a migration. Count attempted lease writes so
// accepting a new owner or rejecting an established one cannot hide one.
type handlerClientPolicyRouteStore struct {
	service.OpenAISessionRouteStore
	mu            sync.Mutex
	route         service.OpenAISessionRoute
	createdOwners []int64
	migrations    int
}

func (s *handlerClientPolicyRouteStore) Get(_ context.Context, domain, digest string) (service.OpenAISessionRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.OwnerAccountID == 0 {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteNotFound
	}
	route := s.route
	route.AuthzDomain, route.SessionDigest = domain, digest
	return route, nil
}

func (s *handlerClientPolicyRouteStore) CreateIfAbsent(_ context.Context, route service.OpenAISessionRoute) (service.OpenAISessionRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdOwners = append(s.createdOwners, route.OwnerAccountID)
	if s.route.OwnerAccountID == 0 {
		route.Generation, route.Status = 1, service.OpenAISessionRouteActive
		s.route = route
	}
	return s.route, nil
}

func (s *handlerClientPolicyRouteStore) BeginMigration(context.Context, string, string, int64, int64, string, string, time.Duration) (service.OpenAISessionRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.migrations++
	return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
}

type handlerClientPolicyWire struct {
	service.HTTPUpstream
	ids  []int64
	auth []string
}

func (w *handlerClientPolicyWire) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	w.ids = append(w.ids, accountID)
	w.auth = append(w.auth, request.Header.Get("Authorization"))
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_b\",\"status\":\"completed\",\"model\":\"gpt-5.1\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"accepted\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}, nil
}

// CODEX_CLIENT_PROFILES.md: a request-local policy denial cannot establish an
// owner, migrate an established owner, consume a credential or penalize health.
func TestCodexClientPolicyHTTPHandlersRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"responses", "chat/completions", "messages"} {
		for _, scenario := range []string{"new-session", "all-denied", "existing-denied-owner"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				cache := &concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}
				concurrency := service.NewConcurrencyService(cache)
				accounts := []service.Account{
					{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
						Credentials: map[string]any{"access_token": "must-not-send-a"}, Extra: map[string]any{"codex_cli_only": true}},
					{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
						Credentials: map[string]any{"api_key": "fixture-b", "base_url": "https://api.openai.com"}, Extra: map[string]any{"responses_supported": true}},
				}
				if scenario == "all-denied" {
					accounts = accounts[:1]
				}
				repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
				store := &handlerClientPolicyRouteStore{}
				if scenario == "existing-denied-owner" {
					store.route = service.OpenAISessionRoute{OwnerAccountID: 101, Generation: 1, Status: service.OpenAISessionRouteActive}
				}
				wire := &handlerClientPolicyWire{}
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, store)
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				body := `{"model":"gpt-5.1","input":"hello","stream":false}`
				if endpoint != "responses" {
					body = `{"model":"gpt-5.1","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
				c.Request.Header.Set("session-id", "stable-session")
				c.Request.Header.Set("User-Agent", "curl/8.0")
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
				switch endpoint {
				case "responses":
					h.Responses(c)
				case "chat/completions":
					h.ChatCompletions(c)
				default:
					h.Messages(c)
				}
				if scenario == "new-session" {
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), "accepted")
					require.Equal(t, []int64{102}, wire.ids)
					require.Equal(t, []string{"Bearer fixture-b"}, wire.auth)
					require.Equal(t, []int64{102}, store.createdOwners)
					require.EqualValues(t, 102, store.route.OwnerAccountID)
				} else {
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), service.CodexOfficialClientsOnlyMessage)
					require.Empty(t, wire.ids)
					require.Empty(t, store.createdOwners)
					if scenario == "all-denied" {
						require.Zero(t, store.route.OwnerAccountID)
					} else {
						require.EqualValues(t, 101, store.route.OwnerAccountID)
						require.EqualValues(t, 1, store.route.Generation)
					}
				}
				require.Zero(t, store.migrations)
				require.Empty(t, repo.rateLimitedIDs)
			})
		}
	}
}

func TestCodexClientPolicyWebSocketDenialRegression(t *testing.T) {
	for _, scenario := range []string{"all-denied", "existing-denied-owner"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.OAuthEnabled, cfg.Gateway.OpenAIWS.APIKeyEnabled = true, true, true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2, cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true, true
			cfg.Gateway.OpenAIWS.IngressModeDefault = service.OpenAIWSIngressModeHTTPBridge
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			concurrency := service.NewConcurrencyService(&concurrencyCacheMock{
				acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			})
			accounts := []service.Account{
				{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"access_token": "must-not-send-a"}, Extra: map[string]any{"codex_cli_only": true, "openai_oauth_responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}},
				{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "fixture-b"}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}},
			}
			store := &handlerClientPolicyRouteStore{}
			if scenario == "all-denied" {
				accounts = accounts[:1]
			} else {
				store.route = service.OpenAISessionRoute{OwnerAccountID: 101, Generation: 1, Status: service.OpenAISessionRouteActive}
			}
			repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
			wire := &handlerClientPolicyWire{}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, store)
			h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			attachDisabledIPAccessControlForWebSocketTest(h)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
				c.Next()
			})
			router.GET("/v1/responses", h.ResponsesWebSocket)
			server := httptest.NewServer(router)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: http.Header{"Session-Id": {"stable-session"}}})
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)))
			_, _, err = conn.Read(ctx)
			require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err), "%v", err)
			require.Empty(t, wire.ids)
			require.Empty(t, repo.rateLimitedIDs)
			store.mu.Lock()
			defer store.mu.Unlock()
			require.Empty(t, store.createdOwners)
			require.Zero(t, store.migrations)
			if scenario == "all-denied" {
				require.Zero(t, store.route.OwnerAccountID)
			} else {
				require.EqualValues(t, 101, store.route.OwnerAccountID)
				require.EqualValues(t, 1, store.route.Generation)
			}
		})
	}
}
