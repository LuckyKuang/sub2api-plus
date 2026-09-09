package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupQuotaFollowResetFieldsAreAdminOnly(t *testing.T) {
	accountID := int64(42)
	baseline := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	group := &service.Group{
		ID:                          7,
		Platform:                    service.PlatformOpenAI,
		SubscriptionType:            service.SubscriptionTypeSubscription,
		QuotaResetSourceAccountID:   &accountID,
		QuotaResetSourceAccountName: "Primary OAuth",
		QuotaResetSourceResetAt:     &baseline,
		QuotaResetIncludeMonthly:    true,
		QuotaResetSourceValid:       true,
	}

	adminPayload, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(adminPayload), `"quota_reset_source_status":"active"`)
	require.Contains(t, string(adminPayload), `"quota_reset_source_account_id":42`)

	userPayload, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(userPayload), "quota_reset_source")
}

func TestGroupQuotaFollowResetStatus(t *testing.T) {
	accountID := int64(42)
	baseline := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		group *service.Group
		want  string
	}{
		{name: "disabled", group: &service.Group{}, want: "disabled"},
		{name: "invalid", group: &service.Group{QuotaResetSourceAccountID: &accountID}, want: "invalid"},
		{name: "waiting", group: &service.Group{QuotaResetSourceAccountID: &accountID, QuotaResetSourceValid: true}, want: "waiting"},
		{name: "active", group: &service.Group{QuotaResetSourceAccountID: &accountID, QuotaResetSourceValid: true, QuotaResetSourceResetAt: &baseline}, want: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, quotaResetSourceStatus(tt.group))
		})
	}
}
