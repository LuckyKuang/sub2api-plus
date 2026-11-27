//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeImageGroups struct {
	service.GroupRepository
	group service.Group
}

func (r nativeImageGroups) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{r.group}, nil
}

type nativeImageAccounts struct {
	service.AccountRepository
	accounts []service.Account
}

func (r nativeImageAccounts) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return r.accounts, nil
}

type nativeImageRoutes struct {
	service.CompositeModelRouteRepository
	routes []service.CompositeModelRoute
}

func (r nativeImageRoutes) ListByGroup(context.Context, int64, bool) ([]service.CompositeModelRoute, error) {
	return r.routes, nil
}

func TestNativeAccessHTTPAdvertisesUsableImageEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, platform, public, mapped, endpoint string
		composite, allowed                       bool
	}{
		{"gpt_image", service.PlatformOpenAI, "gpt-image-1", "gpt-image-1", "/v1/images/generations", false, true},
		{"grok_image", service.PlatformGrok, "grok-imagine-image", "grok-imagine-image", "/v1/images/generations", false, true},
		{"gemini_alias", service.PlatformGemini, "public-art", "gemini-3-pro-image-preview", "/v1beta", false, true},
		{"composite_route_alias", service.PlatformOpenAI, "public-art", "gpt-image-1", "/v1/images/generations", true, true},
		{"image_disabled", service.PlatformOpenAI, "gpt-image-1", "gpt-image-1", "", false, false},
		{"edit_only", service.PlatformGrok, "grok-imagine-edit", "grok-imagine-edit", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := service.Group{ID: 7, Platform: tc.platform, Status: service.StatusActive, AllowImageGeneration: tc.allowed,
				ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{tc.public}}}
			mapping := map[string]any{tc.public: tc.mapped}
			var routes []service.CompositeModelRoute
			if tc.composite {
				group.Platform = service.PlatformComposite
				mapping = map[string]any{tc.mapped: tc.mapped}
				routes = []service.CompositeModelRoute{{ID: 1, GroupID: 7, PublicModel: tc.public,
					MatchType: service.CompositeRouteMatchExact, Endpoint: service.CompositeRouteEndpointImages,
					TargetPlatform: tc.platform, UpstreamModel: tc.mapped, Enabled: true}}
			}
			groups := nativeImageGroups{group: group}
			accounts := nativeImageAccounts{accounts: []service.Account{{ID: 8, Platform: tc.platform, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Credentials: map[string]any{"model_mapping": mapping}}}}
			users := &userHandlerRepoStub{user: &service.User{ID: 42}}
			keys := service.NewAPIKeyService(nil, users, groups, nativeAccessSubscriptionRepo{}, nil, nil, nil)
			plaza := service.NewModelPlazaService(&catalogChannelRepository{}, nil, nil, nil, nil)
			h := NewAPIKeyHandler(keys, service.NewAccountService(accounts, groups), plaza,
				service.NewCompositeRouteResolver(nativeImageRoutes{routes: routes}), &service.OpenAIGatewayService{})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/groups/7/native-access", nil)
			c.Params = gin.Params{{Key: "id", Value: "7"}}
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			h.GetNativeAccess(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body struct {
				Data struct {
					Endpoints []string            `json:"endpoints"`
					Models    []nativeAccessModel `json:"models"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if tc.endpoint == "" {
				require.Empty(t, body.Data.Models)
				require.Empty(t, body.Data.Endpoints)
				return
			}
			require.Equal(t, []string{tc.endpoint}, body.Data.Endpoints)
			require.Equal(t, []nativeAccessModel{{Name: tc.public, Platform: tc.platform,
				Endpoints: []string{tc.endpoint}, ConversionEndpoints: []string{}, IsImage: true}}, body.Data.Models)
			require.NotContains(t, w.Body.String(), "credentials")
			require.NotContains(t, w.Body.String(), "account_id")
		})
	}
}

type nativeAccessGroupRepo struct{ service.GroupRepository }

func (nativeAccessGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{{ID: 99, Platform: service.PlatformComposite, IsExclusive: true}}, nil
}

type nativeAccessSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (nativeAccessSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

// Capability discovery must authorize the group before reading its accounts,
// catalog or upstream metadata. Nil downstream dependencies catch any bypass.
func TestNativeAccessAuthorizationBeforeAccountAndCatalogReads(t *testing.T) {
	userRepo := &userHandlerRepoStub{user: &service.User{ID: 42}}
	h := NewAPIKeyHandler(service.NewAPIKeyService(nil, userRepo, nativeAccessGroupRepo{}, nativeAccessSubscriptionRepo{}, nil, nil, nil), nil, nil, nil, nil)
	for _, tc := range []struct {
		id     string
		auth   bool
		status int
	}{
		{"99", false, http.StatusUnauthorized},
		{"invalid", true, http.StatusBadRequest},
		{"-1", true, http.StatusBadRequest},
		{"99", true, http.StatusForbidden},
		{"100", true, http.StatusForbidden},
	} {
		t.Run(tc.id+http.StatusText(tc.status), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/groups/"+tc.id+"/native-access", nil)
			c.Params = gin.Params{{Key: "id", Value: tc.id}}
			if tc.auth {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			}
			h.GetNativeAccess(c)
			require.Equal(t, tc.status, w.Code)
			require.NotContains(t, w.Body.String(), "api_key")
		})
	}
}

// A price directory is optional for key creation. Exact account and route IDs
// must be discoverable, while disabled accounts and disallowed models stay out.
func TestNativeAccessCandidatesWithoutPriceDirectory(t *testing.T) {
	accounts := []service.Account{
		{Platform: service.PlatformOpenAI, Status: service.StatusActive, Credentials: map[string]any{"model_mapping": map[string]any{"public-gpt": "gpt-native"}}},
		{Platform: service.PlatformAnthropic, Status: service.StatusActive},
		{Platform: service.PlatformGemini, Status: service.StatusDisabled, Credentials: map[string]any{"model_mapping": map[string]any{"private-model": "gemini-native"}}},
	}
	group := &service.Group{Platform: service.PlatformComposite}
	candidates := nativeAccessCandidates(group, accounts, nil, []string{"route-alias"})
	var names []string
	for _, candidate := range candidates {
		names = append(names, candidate.Name)
	}
	require.Contains(t, names, "public-gpt")
	require.Contains(t, names, "route-alias")
	require.Contains(t, names, "claude-opus-5-5")
	require.NotContains(t, names, "private-model")
	group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"public-gpt"}}
	filtered := nativeAccessCandidates(group, accounts, nil, []string{"route-alias"})
	require.Equal(t, []nativeAccessModel{{Name: "public-gpt", Platform: service.PlatformOpenAI}}, filtered)
}

func TestCompositeConversionEndpointsMustCoverEveryModel(t *testing.T) {
	responses := "/v1/responses"
	messages := "/v1/messages"
	models := []nativeAccessModel{
		{Name: "gpt-6.1-sol", Endpoints: []string{responses}, ConversionEndpoints: []string{responses, messages}},
		{Name: "claude-opus-5-5", Endpoints: []string{messages}, ConversionEndpoints: []string{responses, messages}},
	}
	require.ElementsMatch(t, []string{responses, messages}, compositeConversionEndpoints(models))
	models = append(models, nativeAccessModel{Name: "systemone", Endpoints: []string{"/v1/systemone"}})
	require.Empty(t, compositeConversionEndpoints(models), "dedicated non-chat APIs must not be falsely advertised as convertible")
	require.Empty(t, compositeConversionEndpoints(nil))
}

// The shared Zhipu catalog is a candidate list, not proof that a
// pay-as-you-go key can invoke a coding-plan SKU.
func TestNativeAccessCandidatesHideGLM54WithoutCodingPlan(t *testing.T) {
	group := &service.Group{Platform: service.PlatformComposite}
	payg := service.Account{Platform: service.PlatformZhipu, Status: service.StatusActive, Credentials: map[string]any{"account_mode": service.AccountModePayG}}

	names := map[string]bool{}
	for _, candidate := range nativeAccessCandidates(group, []service.Account{payg}, nil, nil) {
		names[candidate.Name] = true
	}
	require.True(t, names["glm-5.3"])
	require.False(t, names["glm-5.4"])

	coding := service.Account{Platform: service.PlatformZhipu, Status: service.StatusActive, Credentials: map[string]any{"account_mode": service.AccountModeCoding}}
	names = map[string]bool{}
	for _, candidate := range nativeAccessCandidates(group, []service.Account{payg, coding}, nil, nil) {
		names[candidate.Name] = true
	}
	require.True(t, names["glm-5.4"])
}
