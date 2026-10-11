//go:build unit || !integration

package dto

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// PAYMENT.md: ordinary batch editing must reject attempts to alter the frozen
// price, currency, plan or duration, even alongside a permitted notes change.
func TestRedeemOrderSnapshotBatchFieldsRejectEdits(t *testing.T) {
	for _, key := range []string{"generate_order", "order_currency", "order_amount", "order_plan_id", "order_plan_name", "validity_days"} {
		t.Run(key, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"notes": "allowed", key: nil})
			require.NoError(t, err)
			var fields BatchUpdateRedeemCodeFields
			require.ErrorIs(t, json.Unmarshal(raw, &fields), service.ErrRedeemOrderSnapshotImmutable)
			require.Nil(t, fields.Notes, "a rejected request must not apply its permitted sibling")
		})
	}
	var fields BatchUpdateRedeemCodeFields
	require.NoError(t, json.Unmarshal([]byte(`{"notes":"allowed","expires_at":null,"status":"unused"}`), &fields))
	require.Equal(t, "allowed", *fields.Notes)
	require.True(t, fields.ExpiresAt.Set)
	require.Nil(t, fields.ExpiresAt.Value)
}
