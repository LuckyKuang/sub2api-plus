//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestZhipuStartPlanUnknownPublicCatalogNeverInventsDefaultPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformZhipu, service.PlatformComposite} {
		for _, allowlisted := range []bool{false, true} {
			for _, endpoint := range []string{"models", "codex"} {
				t.Run(platform+"/"+endpoint+map[bool]string{true: "/allowlist", false: ""}[allowlisted], func(t *testing.T) {
					group := &service.Group{ID: 981, Platform: platform}
					if allowlisted {
						group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"GLM-5.3", "claude-sonnet-4-6"}}
					}
					repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: {{ID: 982, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
						Credentials: map[string]any{"oauth_provider": "zai", "plan_kind": service.ZhipuPlanStartPlan, "account_mode": service.AccountModeCoding, "model_mapping": map[string]any{"claude-sonnet-4-6": "GLM-5.3"}}}}}}
					h := newGatewayModelsHandlerForTest(repo)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
					c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
					if endpoint == "models" {
						prepareGatewayMetadataTest(h, c)
						h.Models(c)
						var result gatewayModelsResponseForTest
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
						require.Empty(t, result.Data)
					} else {
						prepareGatewayMetadataTest(h, c)
						h.CodexModels(c)
						var result codexModelsResponseForTest
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
						require.Empty(t, result.Models)
					}
					require.Equal(t, http.StatusOK, recorder.Code)
				})
			}
		}
	}
}
