//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
