//go:build unit

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestZhipuStartPlanSchedulerProjectionKeepsPermissionWithoutJWT(t *testing.T) {
	a := service.Account{ID: 772, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"oauth_provider": "zai", "plan_kind": service.ZhipuPlanStartPlan, "zcode_jwt_token": "private-start-jwt", "api_key": "private-start-jwt"}, Extra: map[string]any{}}
	a.Extra["zhipu_start_plan_entitlement"] = &service.ZhipuStartPlanEntitlement{State: "available", ModelsKnown: true, Models: []string{"GLM-5.3"}, ObservedAt: time.Now().Unix(), ServerTime: time.Now().Unix(), OwnerGeneration: service.CredentialStateGeneration(&a)}
	metadata := buildSchedulerMetadataAccount(a)
	require.Equal(t, service.ZhipuPlanStartPlan, metadata.GetCredential("plan_kind"))
	require.True(t, metadata.IsModelSupported("GLM-5.3"))
	require.False(t, metadata.IsModelSupported("GLM-4.7"))
	body, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private-start-jwt")
	full, slim, err := marshalSchedulerCacheAccount(a)
	require.NoError(t, err)
	require.Contains(t, string(full), "private-start-jwt")
	require.NotContains(t, string(slim), "private-start-jwt")
	a.Credentials["zcode_jwt_token"] = "relinked-start-jwt"
	a.Credentials["api_key"] = "relinked-start-jwt"
	updated := buildSchedulerMetadataAccount(a)
	require.True(t, updated.IsModelSupported("GLM-4.7"), "old whitelist cannot become the new grant's permission; the new grant keeps its prior upstream-verdict policy")
}
