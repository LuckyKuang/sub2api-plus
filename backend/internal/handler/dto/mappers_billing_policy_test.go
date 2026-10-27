//go:build unit

package dto

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogBillingPolicyPublicEvidenceDoesNotExposePrivateData(t *testing.T) {
	entry := &service.UsageLog{BillingPolicyVersion: 7, BillingRateSnapshot: map[string]any{
		"platform": "openai", "model": "public-code", "effective_multiplier": 1.5,
		"platform_multiplier": 1.2, "model_multiplier": 1.25, "rate_calculation": "product_v2", "peak_scope": "all_meters",
		"policy_version": int64(7), "credentials": "private-secret",
		"account_cost": .01, "upstream_model": "private-alias", "account_id": 999,
		"source":     map[string]any{"token": "nested-secret"},
		"components": []map[string]any{{"billing_unit": "request", "effective_multiplier": 3.0, "credentials": "component-secret", "components": []any{map[string]any{"token": "recursive-secret"}}}},
	}}
	user := UsageLogFromService(entry)
	require.EqualValues(t, 7, user.BillingPolicyVersion)
	require.Equal(t, "public-code", user.BillingRateSnapshot["model"])
	require.Equal(t, 1.5, user.BillingRateSnapshot["effective_multiplier"])
	require.Equal(t, 1.2, user.BillingRateSnapshot["platform_multiplier"])
	require.Equal(t, 1.25, user.BillingRateSnapshot["model_multiplier"])
	require.Equal(t, "product_v2", user.BillingRateSnapshot["rate_calculation"])
	require.Equal(t, "all_meters", user.BillingRateSnapshot["peak_scope"])
	require.Equal(t, []map[string]any{{"billing_unit": "request", "effective_multiplier": 3.0}}, user.BillingRateSnapshot["components"])
	data, err := json.Marshal(user.BillingRateSnapshot)
	require.NoError(t, err)
	for _, private := range []string{"private-secret", "account_cost", "upstream_model", "account_id", "nested-secret", "component-secret", "recursive-secret"} {
		require.NotContains(t, string(data), private)
	}
	entry.BillingRateSnapshot["effective_multiplier"] = 99
	require.Equal(t, 1.5, user.BillingRateSnapshot["effective_multiplier"])
}
