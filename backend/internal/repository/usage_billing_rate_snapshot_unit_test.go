//go:build unit

package repository

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogBillingRateSnapshotInsertEvidence(t *testing.T) {
	// The statement must persist the accepted version and the exact public rate
	// evidence for both atomic and queued usage paths, not recalculate a quote.
	entry := &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "rate-policy-evidence",
		BillingPolicyVersion: 9, RateMultiplier: 1.2,
		BillingRateSnapshot: map[string]any{"platform": "openai", "model": "public-code", "base_multiplier": .8, "policy_multiplier": 1.5, "effective_multiplier": 1.2},
	}
	prepared := prepareUsageLogInsert(entry)
	require.Equal(t, int64(9), prepared.args[len(prepared.args)-2])
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(prepared.args[len(prepared.args)-1].(string)), &stored))
	require.Equal(t, 1.2, stored["effective_multiplier"])
	require.Equal(t, "public-code", stored["model"])
	require.NotContains(t, stored, "upstream_model")
	require.NotContains(t, stored, "credentials")
	entry.BillingRateSnapshot["effective_multiplier"] = 99
	require.Equal(t, 1.2, stored["effective_multiplier"])
	legacy := prepareUsageLogInsert(&service.UsageLog{})
	require.Equal(t, int64(0), legacy.args[len(legacy.args)-2])
	require.Nil(t, legacy.args[len(legacy.args)-1])
}
