//go:build unit

package server_test

import (
	"encoding/json"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestUsageInspectionDetailProjectionAndOwnership(t *testing.T) {
	deps := newContractDeps(t)
	upstream := "private-upstream"
	deps.usageRepo.SetUserLogs(1, []service.UsageLog{{ID: 10, UserID: 1, AccountID: 99, Model: "requested", UpstreamModel: &upstream, RequestID: "rid", CreatedAt: deps.now}})
	deps.usageRepo.SetUserLogs(2, []service.UsageLog{{ID: 20, UserID: 2}})
	status, body := doRequest(t, deps.router, http.MethodGet, "/api/v1/usage/10", "", nil)
	require.Equal(t, http.StatusOK, status)
	var user struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &user))
	// Preserve the established public DTO's numeric account ID; account details,
	// upstream mapping and account costs remain administrator-only.
	require.Equal(t, float64(99), user.Data["account_id"])
	require.NotContains(t, user.Data, "account")
	require.NotContains(t, user.Data, "account_stats_cost")
	require.NotContains(t, user.Data, "upstream_model")
	require.Equal(t, "rid", user.Data["request_id"])
	status, _ = doRequest(t, deps.router, http.MethodGet, "/api/v1/usage/20", "", nil)
	require.Equal(t, http.StatusForbidden, status)
	status, body = doRequest(t, deps.router, http.MethodGet, "/api/v1/admin/usage/10", "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"account_id":99`)
	require.Contains(t, body, "private-upstream")
	for _, id := range []string{"0", "-1", "invalid"} {
		status, _ = doRequest(t, deps.router, http.MethodGet, "/api/v1/admin/usage/"+id, "", nil)
		require.Equal(t, http.StatusBadRequest, status)
	}
	status, _ = doRequest(t, deps.router, http.MethodGet, "/api/v1/admin/usage/999", "", nil)
	require.Equal(t, http.StatusNotFound, status)
}

func TestUserOverviewForwardsViewerTimezone(t *testing.T) {
	deps := newContractDeps(t)
	status, _ := doRequest(t, deps.router, http.MethodGet, "/api/v1/usage/dashboard/stats?timezone=Europe%2FAmsterdam", "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "Europe/Amsterdam", deps.usageRepo.dashboardTimezone)
	require.Equal(t, int64(1), deps.usageRepo.dashboardUserID)
}
