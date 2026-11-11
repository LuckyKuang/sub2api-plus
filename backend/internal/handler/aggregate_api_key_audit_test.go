//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

type aggregateAuditKeys struct {
	service.APIKeyRepository
	key     *service.APIKey
	touches int
}

func (r *aggregateAuditKeys) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	out := *r.key
	return &out, nil
}
func (r *aggregateAuditKeys) UpdateLastUsed(context.Context, int64, time.Time) error {
	r.touches++
	return nil
}

type aggregateAuditUsers struct{ service.UserRepository }

func (aggregateAuditUsers) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 9, Status: service.StatusActive, Balance: 100, Concurrency: 1}, nil
}

type aggregateAuditGroups struct{ service.GroupRepository }

func (aggregateAuditGroups) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{{ID: 1827, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1}}, nil
}

type aggregateAuditAccounts struct {
	service.AccountRepository
	accounts []service.Account
}

func (r aggregateAuditAccounts) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return r.accounts, nil
}

func TestAggregateBothRealAuditsPrecedeHTTPAndWSSideEffects(t *testing.T) {
	// docs/SECURITY_AUDIT_CONTENT_COVERAGE.md: original real-shaped payloads
	// reach both engines after real auth/group resolution. Unknown-only valid
	// content passes audit; a fixture slot denial stops it before provider work.
	fixture, err := os.ReadFile("../testdata/official_agents/codex_incremental_exec.json")
	require.NoError(t, err)
	for _, mode := range []string{service.GroupBindingAll, service.GroupBindingCustom} {
		for _, kind := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
			for _, engine := range []string{"moderation", "prompt"} {
				for _, websocket := range []bool{false, true} {
					for _, recognized := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/ws=%t/recognized=%t", mode, kind, engine, websocket, recognized), func(t *testing.T) {
							body := fixture
							scans := 1
							if !recognized {
								body, err = sjson.SetRawBytes(body, "input.5", []byte(`{"type":"future_item","opaque":true}`))
								require.NoError(t, err)
								scans = 0
							}
							coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, scans)
							account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: kind, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture-oauth", "api_key": "fixture-api-key", "model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}}
							h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
							boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { boundary.userSlots.Add(1); return false, nil }
							user, _ := (aggregateAuditUsers{}).GetByID(context.Background(), 9)
							key := &service.APIKey{ID: 8, UserID: 9, Key: "aggregate-audit-fixture", Status: service.StatusActive, GroupBindingMode: mode, User: user}
							if mode == service.GroupBindingCustom {
								key.AggregateGroupIDs = []int64{1827}
							}
							keys := &aggregateAuditKeys{key: key}
							cfg := &config.Config{RunMode: config.RunModeStandard}
							apiKeys := service.ProvideAPIKeyService(aggregateAuditAccounts{accounts: []service.Account{account}}, nil, keys, aggregateAuditUsers{}, aggregateAuditGroups{}, nil, nil, nil, cfg, nil, nil)
							h.apiKeyService = apiKeys
							attachDisabledIPAccessControlForWebSocketTest(h)
							router := gin.New()
							router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeys, nil, cfg)))
							router.POST("/v1/responses", h.Responses)
							router.GET("/v1/responses", h.ResponsesWebSocket)
							if websocket {
								server := httptest.NewServer(router)
								defer server.Close()
								ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
								defer cancel()
								conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer aggregate-audit-fixture"}, "X-Composite-Group-Id": {"1827"}}})
								require.NoError(t, err)
								defer conn.CloseNow()
								body, err = sjson.SetBytes(body, "type", "response.create")
								require.NoError(t, err)
								require.NoError(t, conn.Write(ctx, coderws.MessageText, body))
								for {
									_, _, err = conn.Read(ctx)
									if err != nil {
										break
									}
								}
								require.NotErrorIs(t, err, context.DeadlineExceeded)
							} else {
								request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
								request.Header.Set("Authorization", "Bearer aggregate-audit-fixture")
								request.Header.Set("Content-Type", "application/json")
								result := httptest.NewRecorder()
								router.ServeHTTP(result, request)
								if recognized {
									require.Equal(t, http.StatusForbidden, result.Code, result.Body.String())
								} else {
									require.Equal(t, http.StatusTooManyRequests, result.Code, result.Body.String())
								}
							}
							observed := supplier.snapshot()
							require.Len(t, observed, scans)
							if recognized {
								require.Zero(t, keys.touches, "an audit rejection must not update last-used state")
							}
							if recognized {
								require.Contains(t, observed[0], "audit current request")
								assertCodexCLIIngressNoSideEffects(t, boundary)
							} else {
								require.EqualValues(t, 1, boundary.userSlots.Load())
								require.Zero(t, boundary.repositoryReads.Load())
								require.Zero(t, boundary.accountSlots.Load())
								require.Empty(t, boundary.wire.calls())
							}
						})
					}
				}
			}
		}
	}
}
