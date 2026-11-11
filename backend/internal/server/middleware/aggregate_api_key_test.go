//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type aggregateAuthUserRepo struct{ service.UserRepository }

func (aggregateAuthUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 9, Status: service.StatusActive, Balance: 100, Concurrency: 3}, nil
}

type aggregateAuthGroups struct{ service.GroupRepository }

func (aggregateAuthGroups) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{
		{ID: 1, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard},
		{ID: 2, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard},
	}, nil
}

type aggregateAuthAccounts struct{ service.AccountRepository }

func (aggregateAuthAccounts) ListModelAvailabilityCandidates(_ context.Context, id *int64, _ []string, _ bool) ([]service.Account, error) {
	platform, model := service.PlatformOpenAI, "test-openai"
	if *id == 2 {
		platform, model = service.PlatformGemini, "test-gemini"
	}
	return []service.Account{{ID: *id * 10, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{model: model}}}}, nil
}

func TestAggregateAuthNativeScopeBodyAndResourceAdmission(t *testing.T) {
	// docs/AGGREGATE_API_KEYS.md: the body is unchanged, grants/native
	// capability decide the group, and resources never guess across groups.
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, method, path, route, body, hint string
		google                                bool
		status                                int
		group                                 int64
	}{
		{"responses", http.MethodPost, "/v1/responses", "/v1/responses", `{"model":"test-openai","input":"hi","unknown":{"opaque":true}}`, "", false, 200, 1},
		{"gemini-native", http.MethodPost, "/v1beta/models/test-gemini:generateContent", "/v1beta/models/*modelAction", `{"contents":[{"parts":[{"text":"hi"}]}]}`, "", true, 200, 2},
		{"no-conversion", http.MethodPost, "/v1/messages", "/v1/messages", `{"model":"test-openai","messages":[]}`, "", false, 400, 0},
		{"ambiguous-model", http.MethodPost, "/v1/responses", "/v1/responses", `{"model":"test-openai","Model":"test-gemini","input":"hi"}`, "", false, 400, 0},
		{"continuation-needs-group", http.MethodPost, "/v1/responses", "/v1/responses", `{"model":"test-openai","previous_response_id":"resp_1","input":"hi"}`, "", false, 400, 0},
		{"pinned-continuation", http.MethodPost, "/v1/responses", "/v1/responses", `{"model":"test-openai","previous_response_id":"resp_1","input":"hi"}`, "1", false, 200, 1},
		{"ws-needs-group", http.MethodGet, "/v1/responses", "/v1/responses", "", "", false, 400, 0},
		{"pinned-ws", http.MethodGet, "/v1/responses", "/v1/responses", "", "1", false, 200, 1},
		{"foreign-group", http.MethodGet, "/v1/files/file_1", "/v1/files/:file_id", "", "99", false, 400, 0},
		{"bad-group-header", http.MethodGet, "/v1/files/file_1", "/v1/files/:file_id", "", "1,2", false, 400, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := &service.APIKey{ID: 8, UserID: 9, Key: "aggregate-fixture", Status: service.StatusActive, GroupBindingMode: service.GroupBindingAll, User: &service.User{ID: 9, Status: service.StatusActive}}
			touches, handled := 0, false
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copy := *key; return &copy, nil }, updateLastUsed: func(context.Context, int64, time.Time) error { touches++; return nil }}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			svc := service.ProvideAPIKeyService(aggregateAuthAccounts{}, nil, repo, aggregateAuthUserRepo{}, aggregateAuthGroups{}, nil, nil, nil, cfg, nil, nil)
			router := gin.New()
			if test.google {
				router.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
			} else {
				router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
			}
			router.Handle(test.method, test.route, func(c *gin.Context) {
				handled = true
				bound, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.Equal(t, test.group, *bound.GroupID)
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, test.body, string(body))
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer aggregate-fixture")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Composite-Group-ID", test.hint)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			require.Equal(t, test.status, result.Code, result.Body.String())
			require.Equal(t, test.status == 200, handled)
			require.Equal(t, map[bool]int{true: 1, false: 0}[handled], touches, "rejected aggregate requests must not update last-used state")
			require.Nil(t, key.GroupID, "request binding must not mutate the cached key")
		})
	}
}

func TestAggregateAuthDoesNotTouchBeforeRejectedAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	touches := 0
	key := &service.APIKey{ID: 8, UserID: 9, Status: service.StatusActive, GroupBindingMode: service.GroupBindingAll, User: &service.User{ID: 9, Status: service.StatusActive}}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }, updateLastUsed: func(context.Context, int64, time.Time) error { touches++; return nil }}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := service.ProvideAPIKeyService(aggregateAuthAccounts{}, nil, repo, aggregateAuthUserRepo{}, aggregateAuthGroups{}, nil, nil, nil, cfg, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
	router.POST("/v1/responses", func(c *gin.Context) { require.Zero(t, touches); c.AbortWithStatus(http.StatusForbidden) })
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-openai","input":"hi"}`))
	request.Header.Set("Authorization", "Bearer fixture")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	require.Equal(t, http.StatusForbidden, result.Code)
	require.Zero(t, touches)
}

type aggregateUnreadBody struct{ reads int }

func (r *aggregateUnreadBody) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }
func (*aggregateUnreadBody) Close() error               { return nil }

func TestAggregateFileUploadRemainsStreaming(t *testing.T) {
	key := &service.APIKey{ID: 8, UserID: 9, Status: service.StatusActive, GroupBindingMode: service.GroupBindingAll, User: &service.User{ID: 9, Status: service.StatusActive}}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }, updateLastUsed: func(context.Context, int64, time.Time) error { return nil }}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := service.ProvideAPIKeyService(aggregateAuthAccounts{}, nil, repo, aggregateAuthUserRepo{}, aggregateAuthGroups{}, nil, nil, nil, cfg, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
	body := &aggregateUnreadBody{}
	router.POST("/v1/files", func(c *gin.Context) {
		require.Zero(t, body.reads, "scope resolution must not buffer multipart uploads")
		require.Same(t, body, c.Request.Body)
		c.Status(200)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/files", nil)
	request.Body = body
	request.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	request.Header.Set("Authorization", "Bearer fixture")
	request.Header.Set("X-Composite-Group-ID", "1")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	require.Equal(t, 200, result.Code)
	require.Zero(t, body.reads)
}
