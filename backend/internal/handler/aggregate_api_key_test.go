//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAggregateResourceGroupHintDoesNotGrantCacheOwnership(t *testing.T) {
	for _, mode := range []string{service.GroupBindingAll, service.GroupBindingCustom} {
		t.Run(mode, func(t *testing.T) {
			repo := &geminiCachedContentHandlerRepoStub{}
			seedGeminiCachedContent(repo, "own", 7, time.Now().Add(time.Hour))
			seedGeminiCachedContent(repo, "foreign-key", 8, time.Now().Add(time.Hour))
			foreignGroup := seedGeminiCachedContent(repo, "foreign-group", 7, time.Now().Add(time.Hour))
			foreignGroup.GroupID = 99
			h := newGeminiCachedContentHandlerForTest(repo)
			key := geminiCachedContentTestAPIKey(service.PlatformGemini)
			key.GroupBindingMode = mode
			key.AggregateGroupIDs = []int64{42, 99}
			for _, resource := range []string{"own", "foreign-key", "foreign-group"} {
				ctx, result := newGeminiCachedContentTestContext(http.MethodGet, "/v1beta/cachedContents/"+resource, key)
				ctx.Params = gin.Params{{Key: "cacheID", Value: resource}}
				h.GeminiCachedContentsGet(ctx)
				if resource == "own" {
					require.Equal(t, 200, result.Code, result.Body.String())
				} else {
					require.Equal(t, 403, result.Code, result.Body.String())
				}
				require.NotContains(t, result.Body.String(), "upstream")
			}
		})
	}
}

func (r *aggregateAuditKeys) GetByID(context.Context, int64) (*service.APIKey, error) {
	copy := *r.key
	return &copy, nil
}

func TestAggregateNativeAccessOwnershipAndPrivacy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, path, mode string
		user             int64
		status           int
		target           int64
	}{
		{"owner", "/api/v1/keys/8/native-access", service.GroupBindingAll, 9, 200, 0},
		{"foreign-owner", "/api/v1/keys/8/native-access", service.GroupBindingAll, 10, 404, 0},
		{"unauthenticated", "/api/v1/keys/8/native-access", service.GroupBindingAll, 0, 401, 0},
		{"bad-id", "/api/v1/keys/no/native-access", service.GroupBindingAll, 9, 400, 0},
		{"ordinary-key", "/api/v1/keys/8/native-access", service.GroupBindingSingle, 9, 400, 0},
		{"support-target-owner", "/api/v1/keys/8/native-access", service.GroupBindingAll, 1, 200, 9},
		{"support-target-foreign", "/api/v1/keys/8/native-access", service.GroupBindingAll, 9, 404, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys := &aggregateAuditKeys{key: &service.APIKey{ID: 8, UserID: 9, Key: "private-key-value", GroupBindingMode: test.mode, Status: service.StatusActive}}
			accounts := aggregateAuditAccounts{accounts: []service.Account{{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-provider-value", "model_mapping": map[string]any{"public-model": "gpt-5.4"}}}}}
			svc := service.ProvideAPIKeyService(accounts, nil, keys, aggregateAuditUsers{}, aggregateAuditGroups{}, nil, nil, nil, &config.Config{}, nil, nil)
			h := &APIKeyHandler{apiKeyService: svc}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if test.user > 0 {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: test.user})
				}
				if test.target > 0 {
					c.Set(string(middleware.ContextKeyUserRole), "admin")
					c.Set(middleware.SupportReadTargetKey, middleware.SupportReadTarget{Subject: middleware.AuthSubject{UserID: test.target}, Role: "user"})
				}
				c.Next()
			})
			router.GET("/api/v1/keys/:id/native-access", h.GetAggregateNativeAccess)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, test.path, nil))
			require.Equal(t, test.status, result.Code, result.Body.String())
			require.NotContains(t, result.Body.String(), "private-")
			require.NotContains(t, result.Body.String(), "account_id")
			require.NotContains(t, result.Body.String(), "credentials")
			if test.status == 200 {
				require.Equal(t, "public-model", gjson.Get(result.Body.String(), "data.models.0.name").String())
				require.EqualValues(t, 1827, gjson.Get(result.Body.String(), "data.models.0.group_ids.0").Int())
				require.Equal(t, int64(0), gjson.Get(result.Body.String(), "data.conversion_endpoints.#").Int())
			} else {
				require.NotContains(t, result.Body.String(), "public-model")
			}
		})
	}
}
