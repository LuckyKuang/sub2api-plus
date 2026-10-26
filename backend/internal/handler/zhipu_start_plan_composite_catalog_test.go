//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type startCompositeCatalogProbeRepo struct{ service.AccountRepository }

func (*startCompositeCatalogProbeRepo) UpdateCNControlExtraIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error) {
	return true, nil
}

type startCompositeCatalogBalance struct{}

func (*startCompositeCatalogBalance) Do(*http.Request, string, int64, int) (*http.Response, error) {
	const body = `{"code":200,"success":true,"data":{"server_time":2000000000,"plans":[{"user_plan_id":"start-instance","plan_id":"zcode-v3-start-plan","status":"active","starts_at":1900000000,"ends_at":2100000000}],"balances":[{"user_plan_id":"start-instance","capabilities":["model:GLM-5.3"]}]}}`
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *startCompositeCatalogBalance) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

func startCompositeCatalogAccount(t *testing.T, edits ...func(*service.Account)) (*service.Account, *service.ZhipuStartPlanEntitlement) {
	t.Helper()
	account := &service.Account{ID: 992, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"oauth_provider": "zai", "plan_kind": service.ZhipuPlanStartPlan, "account_mode": service.AccountModeCoding, "zcode_jwt_token": "start-catalog-jwt", "api_key": "start-catalog-jwt",
			service.SettingKeyOutboundIdentity: service.OutboundIdentitySelection{Preset: zcode.Preset, Version: "3.16.2", Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"}}}}
	for _, edit := range edits {
		edit(account)
	}
	// Create an authentic, owner/identity-bound observation through the public
	// production observer. Only the provider HTTP port is scripted; the test
	// does not copy the internal hash algorithm into the catalog expectation.
	observer := service.NewCNProviderQuotaService(&startCompositeCatalogProbeRepo{}, nil, &startCompositeCatalogBalance{}, nil)
	result, err := observer.QueryUsageForAccount(context.Background(), account)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.NotNil(t, result.StartPlan)
	require.Equal(t, "available", result.StartPlan.State)
	require.Equal(t, []string{"GLM-5.3"}, result.StartPlan.Models)
	return account, result.StartPlan
}

func publicCatalogIDsForStartCompositeTest(t *testing.T, h *GatewayHandler, group *service.Group, endpoint string) []string {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
	if endpoint == "models" {
		h.Models(c)
		var result gatewayModelsResponseForTest
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		require.Equal(t, http.StatusOK, recorder.Code)
		return modelIDsForTest(result.Data)
	}
	h.CodexModels(c)
	var result codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, http.StatusOK, recorder.Code)
	return codexModelSlugsForTest(result.Models)
}

func TestZhipuStartPlanCompositeRouteAliasRequiresProvenTargetEntitlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 991
	const alias = "start-route-alias"
	for _, scenario := range []struct {
		name string
		edit func(*service.ZhipuStartPlanEntitlement)
		omit bool
		want bool
	}{
		{name: "no-observation", omit: true},
		{name: "models-unknown", edit: func(s *service.ZhipuStartPlanEntitlement) { s.ModelsKnown = false }},
		{name: "unavailable", edit: func(s *service.ZhipuStartPlanEntitlement) { s.State = "unavailable" }},
		{name: "pending", edit: func(s *service.ZhipuStartPlanEntitlement) { s.State = "pending" }},
		{name: "expired", edit: func(s *service.ZhipuStartPlanEntitlement) { s.ExpiresAt = s.ServerTime - 1 }},
		{name: "stale", edit: func(s *service.ZhipuStartPlanEntitlement) { s.ObservedAt = time.Now().Add(-time.Hour).Unix() }},
		{name: "different-model-authorized", edit: func(s *service.ZhipuStartPlanEntitlement) { s.Models = []string{"GLM-4.7"} }},
		{name: "fresh-target-authorized", want: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, allowlisted := range []bool{false, true} {
				for _, endpoint := range []string{"models", "codex"} {
					name := endpoint
					if allowlisted {
						name += "/allowlist"
					}
					t.Run(name, func(t *testing.T) {
						account, observation := startCompositeCatalogAccount(t)
						if scenario.edit != nil {
							scenario.edit(observation)
						}
						if !scenario.omit {
							account.Extra = map[string]any{"zhipu_start_plan_entitlement": observation}
						}
						repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {*account}}}
						routes := &gatewayModelsRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: groupID, PublicModel: alias, MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformZhipu, UpstreamModel: "GLM-5.3", Enabled: true}}}
						h := newGatewayModelsHandlerForTest(repo, routes)
						group := &service.Group{ID: groupID, Platform: service.PlatformComposite}
						if allowlisted {
							group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{alias}}
						}
						ids := publicCatalogIDsForStartCompositeTest(t, h, group, endpoint)
						if scenario.want {
							require.Contains(t, ids, alias, "fresh proven upstream permission can be exposed through an exact composite alias")
						} else {
							require.NotContains(t, ids, alias, "a route and a group allowlist do not establish upstream Start permission")
							if scenario.name != "different-model-authorized" {
								require.Empty(t, ids, "all-Start groups cannot fall back to unrelated default models")
							}
						}
					})
				}
			}
		})
	}
}

func TestZhipuStartPlanCompositeRouteFilteringKeepsOtherPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 993
	account, _ := startCompositeCatalogAccount(t)
	regular := service.Account{ID: 994, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "regular-provider-key", "model_mapping": map[string]any{"gpt-5": "gpt-5"}}}
	repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {*account, regular}}}
	routes := &gatewayModelsRouteRepoStub{routes: []service.CompositeModelRoute{
		{ID: 1, GroupID: groupID, PublicModel: "unproven-start-alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformZhipu, UpstreamModel: "GLM-5.3", Enabled: true},
		{ID: 2, GroupID: groupID, PublicModel: "regular-route-alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformOpenAI, UpstreamModel: "gpt-5", Enabled: true},
	}}
	for _, endpoint := range []string{"models", "codex"} {
		t.Run(endpoint, func(t *testing.T) {
			h := newGatewayModelsHandlerForTest(repo, routes)
			ids := publicCatalogIDsForStartCompositeTest(t, h, &service.Group{ID: groupID, Platform: service.PlatformComposite}, endpoint)
			require.NotContains(t, ids, "unproven-start-alias")
			require.Contains(t, ids, "regular-route-alias", "unrelated non-Start routes retain their existing catalog behavior")
			require.Contains(t, ids, "gpt-5")
		})
	}
}

func TestZhipuStartPlanCompositeCatalogUsesWinningRouteAndActualTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 995
	for _, scenario := range []struct {
		name       string
		model      string
		routes     []service.CompositeModelRoute
		mapping    map[string]any
		wantModels bool
		wantCodex  bool
		wantEmpty  bool
	}{
		{
			name: "route-overrides-account-published-model", model: "GLM-5.3", wantEmpty: true,
			routes: []service.CompositeModelRoute{{ID: 1, PublicModel: "GLM-5.3", UpstreamModel: "GLM-4.7"}},
		},
		{
			name: "lower-priority-proof-cannot-authorize-winner", model: "priority-alias",
			routes: []service.CompositeModelRoute{{ID: 1, PublicModel: "priority-alias", UpstreamModel: "GLM-4.7", Priority: 1}, {ID: 2, PublicModel: "priority-alias", UpstreamModel: "GLM-5.3", Priority: 100}},
		},
		{
			name: "winning-priority-has-proof", model: "priority-alias", wantModels: true, wantCodex: true,
			routes: []service.CompositeModelRoute{{ID: 1, PublicModel: "priority-alias", UpstreamModel: "GLM-5.3", Priority: 1}, {ID: 2, PublicModel: "priority-alias", UpstreamModel: "GLM-4.7", Priority: 100}},
		},
		{
			name: "responses-specific-unproven-target", model: "endpoint-alias", wantModels: true,
			routes: []service.CompositeModelRoute{{ID: 1, PublicModel: "endpoint-alias", UpstreamModel: "GLM-5.3"}, {ID: 2, PublicModel: "endpoint-alias", UpstreamModel: "GLM-4.7", Endpoint: service.CompositeRouteEndpointResponses}},
		},
		{
			name: "responses-specific-proven-target", model: "endpoint-alias", wantModels: true, wantCodex: true,
			routes: []service.CompositeModelRoute{{ID: 1, PublicModel: "endpoint-alias", UpstreamModel: "GLM-4.7"}, {ID: 2, PublicModel: "endpoint-alias", UpstreamModel: "GLM-5.3", Endpoint: service.CompositeRouteEndpointResponses}},
		},
		{
			name: "account-mapping-resolves-actual-model", model: "mapped-route-alias", wantModels: true, wantCodex: true,
			mapping: map[string]any{"account-input": "GLM-5.3"},
			routes:  []service.CompositeModelRoute{{ID: 1, PublicModel: "mapped-route-alias", UpstreamModel: "account-input"}},
		},
		{
			name: "empty-upstream-passes-alias-through-account-mapping", model: "account-input", wantModels: true, wantCodex: true,
			mapping: map[string]any{"account-input": "GLM-5.3"},
			routes:  []service.CompositeModelRoute{{ID: 1, PublicModel: "account-input"}},
		},
		{
			name: "account-mapping-cannot-be-bypassed-by-raw-target", model: "raw-target-alias",
			mapping: map[string]any{"account-input": "GLM-5.3"},
			routes:  []service.CompositeModelRoute{{ID: 1, PublicModel: "raw-target-alias", UpstreamModel: "GLM-5.3"}},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, endpoint := range []string{"models", "codex"} {
				t.Run(endpoint, func(t *testing.T) {
					account, observation := startCompositeCatalogAccount(t, func(account *service.Account) {
						if scenario.mapping != nil {
							account.Credentials["model_mapping"] = scenario.mapping
						}
					})
					account.Extra = map[string]any{"zhipu_start_plan_entitlement": observation}
					routes := append([]service.CompositeModelRoute(nil), scenario.routes...)
					for i := range routes {
						routes[i].GroupID, routes[i].MatchType, routes[i].TargetPlatform, routes[i].Enabled = groupID, service.CompositeRouteMatchExact, service.PlatformZhipu, true
					}
					repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {*account}}}
					h := newGatewayModelsHandlerForTest(repo, &gatewayModelsRouteRepoStub{routes: routes})
					ids := publicCatalogIDsForStartCompositeTest(t, h, &service.Group{ID: groupID, Platform: service.PlatformComposite}, endpoint)
					want := scenario.wantModels
					if endpoint == "codex" {
						want = scenario.wantCodex
					}
					if want {
						require.Contains(t, ids, scenario.model)
					} else {
						require.NotContains(t, ids, scenario.model)
					}
					if scenario.wantEmpty {
						require.Empty(t, ids, "filtering an account-derived name must not resurrect platform defaults")
					}
				})
			}
		})
	}
}

func TestZhipuStartPlanCompositeCatalogPreservesTypedAndPersistedIdentityOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 996
	for _, representation := range []string{"typed-selection", "persisted-json"} {
		t.Run(representation, func(t *testing.T) {
			account, observation := startCompositeCatalogAccount(t)
			if representation == "persisted-json" {
				payload, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				var hydrated map[string]any
				require.NoError(t, json.Unmarshal(payload, &hydrated))
				account.Credentials = hydrated
			}
			account.Extra = map[string]any{"zhipu_start_plan_entitlement": observation}
			repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {*account}}}
			routes := &gatewayModelsRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: groupID, PublicModel: "authorized-owner-alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformZhipu, UpstreamModel: "GLM-5.3", Enabled: true}}}
			for _, endpoint := range []string{"models", "codex"} {
				h := newGatewayModelsHandlerForTest(repo, routes)
				ids := publicCatalogIDsForStartCompositeTest(t, h, &service.Group{ID: groupID, Platform: service.PlatformComposite}, endpoint)
				require.Contains(t, ids, "GLM-5.3", "JSON hydration cannot discard an authorized owner's native model")
				require.Contains(t, ids, "authorized-owner-alias", "equivalent typed/persisted identity must preserve the proven route target")
			}
			account.Credentials["zcode_jwt_token"] = "different-owner-jwt"
			repo.byGroup[groupID] = []service.Account{*account}
			for _, endpoint := range []string{"models", "codex"} {
				h := newGatewayModelsHandlerForTest(repo, routes)
				ids := publicCatalogIDsForStartCompositeTest(t, h, &service.Group{ID: groupID, Platform: service.PlatformComposite}, endpoint)
				require.Empty(t, ids, "canonicalizing identity representation cannot transfer permission to another JWT owner")
			}
		})
	}
}
