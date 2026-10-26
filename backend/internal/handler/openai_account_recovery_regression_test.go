//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/tidwall/gjson"
)

type recoveryHandlerRepo struct {
	*openAIWSFailoverHandlerAccountRepoStub
	quarantined []int64
}

func (r *recoveryHandlerRepo) SetCredentialErrorIfUnchanged(_ context.Context, expectedOwner *service.Account, reason string) (bool, error) {
	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	expected := expectedOwner.Credentials
	proxy := expectedOwner.ProxyID
	want, _ := json.Marshal(expected)
	for i := range r.accounts {
		a := &r.accounts[i]
		got, _ := json.Marshal(a.Credentials)
		if a.ID == id && a.Platform == platform && a.Type == kind && a.Status == service.StatusActive && string(want) == string(got) && handlerGrokCredentialProxyIDsEqual(a.ProxyID, proxy) {
			a.Status, a.Schedulable, a.ErrorMessage = service.StatusError, false, reason
			r.quarantined = append(r.quarantined, id)
			return true, nil
		}
	}
	return false, nil
}

type recoveryHandlerRouteStore struct{ handlerClientPolicyRouteStore }

func (s *recoveryHandlerRouteStore) BeginMigration(_ context.Context, _, _ string, generation, candidate int64, lease, reason string, ttl time.Duration) (service.OpenAISessionRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.Generation != generation || s.route.LeaseID != "" {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	s.migrations++
	s.route.CandidateAccountID, s.route.LeaseID, s.route.LeaseExpiresAt, s.route.MigrationReason = candidate, lease, time.Now().Add(ttl), reason
	return s.route, nil
}

func (s *recoveryHandlerRouteStore) CommitOwner(_ context.Context, _, _, lease string, candidate int64) (service.OpenAISessionRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.LeaseID != lease || s.route.CandidateAccountID != candidate {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	s.route.OwnerAccountID, s.route.CandidateAccountID, s.route.LeaseID = candidate, 0, ""
	s.route.Generation++
	return s.route, nil
}

func (s *recoveryHandlerRouteStore) AbandonMigration(_ context.Context, _, _, lease string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.LeaseID == lease {
		s.route.LeaseID, s.route.CandidateAccountID = "", 0
	}
	return nil
}

type recoveryHandlerWire struct {
	service.HTTPUpstream
	mu       sync.Mutex
	scenario string
	ids      []int64
	auth     []string
}

func (w *recoveryHandlerWire) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	w.mu.Lock()
	w.ids = append(w.ids, id)
	w.auth = append(w.auth, req.Header.Get("Authorization"))
	w.mu.Unlock()
	if id == 101 {
		switch w.scenario {
		case "permanent-401":
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_api_key","message":"credential rejected"}}`))}, nil
		case "transport":
			return nil, errors.New("upstream connection reset before headers")
		case "gateway-timeout":
			return &http.Response{StatusCode: 504, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"upstream request timeout"}}`))}, nil
		case "shared-capacity":
			return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"server_is_overloaded","message":"Selected model is at capacity"}}`))}, nil
		case "after-output":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_partial\",\"model\":\"gpt-5.1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
					"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_partial\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"in_progress\",\"content\":[]}}\n\n" +
					"data: {\"type\":\"response.content_part.added\",\"item_id\":\"msg_partial\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\",\"annotations\":[]}}\n\n" +
					"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_partial\",\"delta\":\"already delivered\",\"output_index\":0,\"content_index\":0}\n\n" +
					"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"upstream failed\"}}}\n\n"))}, nil
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_healthy\",\"status\":\"completed\",\"model\":\"gpt-5.1\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"accepted\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}, nil
}

func (w *recoveryHandlerWire) calls() []int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]int64(nil), w.ids...)
}

// The user-facing account contract and OPENAI_RESPONSES.md require A -> B -> B
// after an unrecoverable account grant or an explicitly retryable connection
// failure, while shared capacity and actual output never authorize pool scans.
// These exercise real selection, credential acquisition, HTTP forwarding,
// migration acceptance and the following turn instead of a failover predicate.
func TestOpenAIAccountRecoveryAcrossHTTPHandlersRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"responses", "responses-passthrough", "chat/completions", "messages"} {
		for _, scenario := range []string{"missing-key", "expired-oauth", "permanent-401", "transport", "gateway-timeout", "shared-capacity", "after-output"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				requestEndpoint := strings.TrimSuffix(endpoint, "-passthrough")
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier, cfg.Gateway.MaxAccountSwitches = 1, 3
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				cache := &concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}
				concurrency := service.NewConcurrencyService(cache)
				accounts := []service.Account{
					{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, Credentials: map[string]any{"api_key": "fixture-a"}},
					{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, Credentials: map[string]any{"api_key": "fixture-b"}},
				}
				if endpoint == "responses-passthrough" {
					for index := range accounts {
						accounts[index].Extra = map[string]any{"openai_passthrough": true}
					}
				}
				if scenario == "missing-key" {
					delete(accounts[0].Credentials, "api_key")
				}
				if scenario == "expired-oauth" {
					accounts[0].Type = service.AccountTypeOAuth
					accounts[0].Credentials = map[string]any{"access_token": "must-not-send-expired", "expires_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
				}
				repo := &recoveryHandlerRepo{openAIWSFailoverHandlerAccountRepoStub: &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}}
				store := &recoveryHandlerRouteStore{}
				wire := &recoveryHandlerWire{scenario: scenario}
				provider := service.NewOpenAITokenProvider(repo, nil, nil)
				rl := service.NewRateLimitService(repo, nil, cfg, nil, nil)
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), rl, billing, wire, &service.DeferredService{}, provider, nil, nil, nil, nil, nil, nil, store)
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
				request := func() *httptest.ResponseRecorder {
					response := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(response)
					body := `{"model":"gpt-5.1","instructions":"test","input":"hello","stream":false}`
					if requestEndpoint != "responses" {
						body = `{"model":"gpt-5.1","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`
					}
					if scenario == "after-output" {
						body = strings.Replace(body, `"stream":false`, `"stream":true`, 1)
					}
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+requestEndpoint, strings.NewReader(body))
					c.Request.Header.Set("session-id", "stable-session")
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, User: &service.User{ID: 1, Status: service.StatusActive}})
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
					switch requestEndpoint {
					case "responses":
						h.Responses(c)
					case "chat/completions":
						h.ChatCompletions(c)
					default:
						h.Messages(c)
					}
					return response
				}
				response := request()
				if scenario == "shared-capacity" || scenario == "after-output" {
					require.Equal(t, []int64{101}, wire.ids, response.Body.String())
					require.EqualValues(t, 101, store.route.OwnerAccountID)
					require.Zero(t, store.migrations)
					require.Empty(t, repo.quarantined)
					if scenario == "after-output" {
						require.Contains(t, response.Body.String(), "already delivered")
					}
					if scenario == "shared-capacity" {
						require.Equal(t, http.StatusServiceUnavailable, response.Code)
					}
					return
				}
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "accepted")
				require.EqualValues(t, 102, store.route.OwnerAccountID)
				require.Equal(t, 1, store.migrations)
				response = request()
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.EqualValues(t, 102, wire.ids[len(wire.ids)-1])
				if scenario == "missing-key" || scenario == "expired-oauth" {
					require.Equal(t, []int64{102, 102}, wire.ids, "invalid credentials must never be sent upstream")
					require.Equal(t, []int64{101}, repo.quarantined)
				} else if scenario == "permanent-401" {
					require.Equal(t, []int64{101, 102, 102}, wire.ids)
					require.Equal(t, []int64{101}, repo.quarantined, "a rejected grant is quarantined before the replacement becomes sticky")
				} else {
					require.Equal(t, []int64{101, 102, 102}, wire.ids)
					require.Empty(t, repo.quarantined, "transport errors cannot revoke a grant")
				}
			})
		}
	}
}

// A WS bridge has the same pre-output credential/transport recovery contract as
// HTTP. Reconnecting with the original session must retain the accepted owner.
func TestOpenAIAccountRecoveryWebSocketBridgeRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"missing-key", "expired-oauth", "transport", "gateway-timeout"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier, cfg.Gateway.MaxAccountSwitches = 1, 3
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
				{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, Credentials: map[string]any{"api_key": "fixture-a"}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}},
				{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, Credentials: map[string]any{"api_key": "fixture-b"}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}},
			}
			if scenario == "missing-key" {
				delete(accounts[0].Credentials, "api_key")
			}
			if scenario == "expired-oauth" {
				accounts[0].Type = service.AccountTypeOAuth
				accounts[0].Credentials = map[string]any{"access_token": "must-not-send-expired", "expires_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
				accounts[0].Extra = map[string]any{"openai_oauth_responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}
			}
			repo := &recoveryHandlerRepo{openAIWSFailoverHandlerAccountRepoStub: &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}}
			store := &recoveryHandlerRouteStore{}
			wire := &recoveryHandlerWire{scenario: scenario}
			provider := service.NewOpenAITokenProvider(repo, nil, nil)
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, wire, &service.DeferredService{}, provider, nil, nil, nil, nil, nil, nil, store)
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
			for turn := 0; turn < 2; turn++ {
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: http.Header{"Session-Id": {"stable-session"}}})
					require.NoError(t, err)
					defer func() { _ = conn.CloseNow() }()
					require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)))
					for {
						_, event, readErr := conn.Read(ctx)
						require.NoError(t, readErr)
						if gjson.GetBytes(event, "type").String() == "response.completed" {
							require.Contains(t, string(event), "accepted")
							return
						}
					}
				}()
			}
			if scenario == "missing-key" || scenario == "expired-oauth" {
				require.Equal(t, []int64{102, 102}, wire.calls(), "invalid credentials must never reach the bridge transport")
			} else {
				require.Equal(t, []int64{101, 102, 102}, wire.calls())
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			require.EqualValues(t, 102, store.route.OwnerAccountID)
			require.Equal(t, 1, store.migrations)
		})
	}
}
