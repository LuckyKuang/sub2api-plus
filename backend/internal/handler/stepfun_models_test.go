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

func TestStepFunClientCatalogsRespectDiscoveryAndRestrictions(t *testing.T) {
	for _, kind := range []string{"apikey", "oauth"} {
		for _, scenario := range []string{"defaults", "discovered", "restricted", "group allowlist", "composite", "no accounts"} {
			for _, codex := range []bool{false, true} {
				t.Run(kind+"/"+scenario+map[bool]string{false: "/models", true: "/codex"}[codex], func(t *testing.T) {
					account := service.Account{ID: 42, Platform: service.PlatformStepFun, Type: kind, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{}}
					group := &service.Group{ID: 123, Platform: service.PlatformStepFun}
					if scenario == "composite" {
						group.Platform = service.PlatformComposite
					}
					want := []string{"step-5-preview", "step-3.7-flash", "step-3.5-flash-2603", "step-3.5-flash", "step-router-v1"}
					if scenario == "discovered" || scenario == "restricted" {
						account.Extra = map[string]any{service.UpstreamModelMetadataExtraKey: map[string]any{"models": map[string]any{
							"step-future":  map[string]any{"id": "step-future", "context_window": 256000, "input_modalities": []string{"text", "image"}, "reasoning": true, "supported_reasoning_levels": []string{"low", "high"}},
							"step-another": map[string]any{"id": "step-another"},
						}}}
						want = []string{"step-future", "step-another"}
					}
					if scenario == "restricted" {
						account.Credentials["model_mapping"] = map[string]any{"public-step": "step-future"}
						want = []string{"public-step"}
					}
					if scenario == "group allowlist" {
						group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"step-3.5-*"}}
						want = []string{"step-3.5-flash", "step-3.5-flash-2603"}
					}
					accounts := []service.Account{account}
					if scenario == "no accounts" {
						accounts = nil
					}
					h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{123: accounts}})
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
					c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
					var got []string
					if codex {
						h.CodexModels(c)
						var response codexModelsResponseForTest
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
						for _, model := range response.Models {
							got = append(got, model.Slug)
							if model.Slug == "step-future" || model.Slug == "public-step" {
								require.Equal(t, []string{"text", "image"}, model.InputModalities)
								require.Equal(t, []codexReasoningLevelForTest{{Effort: "low"}, {Effort: "high"}}, model.SupportedReasoningLevels)
							}
						}
					} else {
						h.Models(c)
						var response gatewayModelsResponseForTest
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
						got = modelIDsForTest(response.Data)
						if scenario != "composite" {
							for _, model := range response.Data {
								require.Equal(t, "stepfun", model.OwnedBy)
							}
						}
					}
					require.Equal(t, http.StatusOK, recorder.Code)
					require.ElementsMatch(t, want, got, "must not publish Claude defaults or models outside restrictions")
				})
			}
		}
	}
}

func TestStepFunRetrieveModelUsesFilteredCatalog(t *testing.T) {
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
	for _, model := range []string{"step-5-preview", "step-3.7-flash"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models/"+model, nil)
		c.Params = gin.Params{{Key: "model", Value: model}}
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 1, Platform: service.PlatformStepFun, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"step-5-preview"}}}})
		h.Models(c)
		if model == "step-5-preview" {
			require.Equal(t, http.StatusOK, recorder.Code)
			var item gatewayModelItemForTest
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &item))
			require.Equal(t, model, item.ID)
			require.Equal(t, "stepfun", item.OwnedBy)
		} else {
			require.Equal(t, http.StatusNotFound, recorder.Code)
		}
	}
}
