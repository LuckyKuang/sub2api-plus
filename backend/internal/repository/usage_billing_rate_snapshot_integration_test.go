//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageLogBillingRateSnapshotSQLRoundTrip(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	ctx := context.Background()
	user := mustCreateUser(t, client, &service.User{Email: "billing-evidence@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-billing-evidence", Name: "evidence"})
	account := mustCreateAccount(t, client, &service.Account{Name: "billing-evidence"})
	for _, version := range []int64{0, 7} {
		entry := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
			RequestID: uuid.NewString(), Model: "public-code", BillingPolicyVersion: version,
			TotalCost: 2, ActualCost: 3, RateMultiplier: 1.5}
		if version != 0 {
			entry.BillingRateSnapshot = map[string]any{"platform": "openai", "model": "public-code", "effective_multiplier": 1.5, "policy_version": 7}
		}
		created, err := repo.Create(ctx, entry)
		require.NoError(t, err)
		require.True(t, created)
		stored, err := repo.GetByID(ctx, entry.ID)
		require.NoError(t, err)
		require.Equal(t, version, stored.BillingPolicyVersion)
		require.Equal(t, 3.0, stored.ActualCost)
		if version == 0 {
			require.Empty(t, stored.BillingRateSnapshot)
		} else {
			require.Equal(t, "openai", stored.BillingRateSnapshot["platform"])
			require.Equal(t, "public-code", stored.BillingRateSnapshot["model"])
			require.Equal(t, 1.5, stored.BillingRateSnapshot["effective_multiplier"])
		}
		// A retry of this same billed request cannot replace historical evidence.
		entry.BillingPolicyVersion = 99
		entry.BillingRateSnapshot = map[string]any{"effective_multiplier": 99}
		created, err = repo.Create(ctx, entry)
		require.NoError(t, err)
		require.False(t, created)
		stored, err = repo.GetByID(ctx, entry.ID)
		require.NoError(t, err)
		require.Equal(t, version, stored.BillingPolicyVersion)
	}
}
