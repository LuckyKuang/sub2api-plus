//go:build unit || !integration

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type liveIdentityCache struct {
	service.LiveRequestCache
	values []string
}

func (c liveIdentityCache) List(context.Context, int64, time.Time) ([]string, bool, error) {
	return c.values, false, nil
}
func (c liveIdentityCache) UsageBindings(context.Context, []string) ([]string, error) {
	return nil, nil
}

// Requirement: repeated rows for one user and another user are enriched in one
// bounded identity-only query. The handler exposes email+ID without secrets.
func TestAdminLiveRequestsBatchUserIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	values := []string{}
	for i, userID := range []int64{7, 7, 8} {
		row := service.LiveRequest{ID: []string{"one", "two", "three"}[i], UserID: userID, Model: "model", StartedAt: now, UpdatedAt: now, Status: "requesting"}
		raw, err := json.Marshal(row)
		require.NoError(t, err)
		values = append(values, string(raw))
	}
	admin := &stubAdminService{users: []service.User{{ID: 7, Email: "seven@example.test"}, {ID: 8, Email: "eight@example.test"}}}
	handler := NewUsageHandler(nil, nil, admin, nil)
	handler.SetLiveRequests(service.NewLiveRequestService(liveIdentityCache{values: values}))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/v1/admin/usage/live", nil)
	handler.LiveRequests(c)
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, 1, admin.lastListUsers.calls)
	require.ElementsMatch(t, []int64{7, 8}, admin.lastListUsers.filters.IDs)
	require.True(t, admin.lastListUsers.filters.IdentityOnly)
	var response struct {
		Data service.LiveRequestSnapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 3)
	for _, row := range response.Data.Items {
		if row.UserID == 7 {
			require.Equal(t, "seven@example.test", row.UserEmail)
		} else {
			require.Equal(t, "eight@example.test", row.UserEmail)
		}
	}
}
