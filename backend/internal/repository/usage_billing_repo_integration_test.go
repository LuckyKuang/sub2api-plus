//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func TestUsageBillingRepositoryApply_DeduplicatesBalanceBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-" + uuid.NewString(),
		Name:   "billing",
		Quota:  1,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         service.AccountTypeAPIKey,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result1)
	require.True(t, result1.Applied)
	require.True(t, result1.APIKeyQuotaExhausted)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id = $1", apiKey.ID).Scan(&quotaUsed))
	require.InDelta(t, 1.25, quotaUsed, 0.000001)

	var usage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT usage_5h FROM api_keys WHERE id = $1", apiKey.ID).Scan(&usage5h))
	require.InDelta(t, 1.25, usage5h, 0.000001)

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id = $1", apiKey.ID).Scan(&status))
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_PersistsUsageInBillingTransaction(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-atomic-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash", Balance: 100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-usage-atomic-" + uuid.NewString(), Name: "atomic",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-atomic-account-" + uuid.NewString(), Type: service.AccountTypeAPIKey,
	})
	requestID := uuid.NewString()
	cleanupPersistedUsageLogFixture(t, ctx, user.ID, apiKey.ID, account.ID, requestID)
	completed := true
	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: requestID, APIKeyID: apiKey.ID, UserID: user.ID,
		AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: 1.25,
		UsageLog: &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			RequestID: requestID, Model: "atomic-model", ActualCost: 1.25,
			IsComplete: &completed,
		},
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogPersisted)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)
	var status, source string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT completion_status, usage_source FROM usage_logs
WHERE request_id = $1 AND api_key_id = $2`, requestID, apiKey.ID).Scan(&status, &source))
	require.Equal(t, service.UsageCompletionCompleted, status)
	require.Equal(t, service.UsageSourceUpstreamExact, source)
}

func TestUsageBillingRepositoryApply_RollsBackBillingWhenUsageInsertFails(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-rollback-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash", Balance: 100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-usage-rollback-" + uuid.NewString(), Name: "rollback",
	})
	requestID := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: requestID, APIKeyID: apiKey.ID, UserID: user.ID, BalanceCost: 1.25,
		UsageLog: &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: 0,
			RequestID: requestID, Model: "",
		},
	})
	require.Error(t, err)
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 100.0, balance, 0.000001)
	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2`, requestID, apiKey.ID).Scan(&dedupCount))
	require.Zero(t, dedupCount)
}

func TestUsageBillingRepositoryCaptureBatchImage_PersistsUsageAtomically(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("batch-capture-atomic-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Balance: 100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-batch-capture-" + uuid.NewString(), Name: "batch-capture",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "batch-capture-account-" + uuid.NewString(), Type: service.AccountTypeAPIKey,
	})
	batchID := "imgbatch_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID: service.BatchImageHoldRequestID(batchID), APIKeyID: apiKey.ID,
		UserID: user.ID, BatchID: batchID, HoldAmount: 2,
	})
	require.NoError(t, err)
	completed := true
	requestID := service.BatchImageCaptureRequestID(batchID)
	cleanupPersistedUsageLogFixture(t, ctx, user.ID, apiKey.ID, account.ID, requestID)
	result, err := repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID: requestID, APIKeyID: apiKey.ID, UserID: user.ID, BatchID: batchID,
		HoldAmount: 2, ActualAmount: 1.25,
		UsageLog: &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			RequestID: requestID, Model: "imagen-batch", ActualCost: 1.25,
			IsComplete: &completed,
		},
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogPersisted)

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 98.75, balance, 0.000001)
	require.InDelta(t, 0, frozen, 0.000001)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2`, requestID, apiKey.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestUsageBillingRepositoryCaptureBatchImage_RollsBackWhenUsageInsertFails(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("batch-capture-rollback-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Balance: 100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-batch-rollback-" + uuid.NewString(), Name: "batch-rollback",
	})
	batchID := "imgbatch_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID: service.BatchImageHoldRequestID(batchID), APIKeyID: apiKey.ID,
		UserID: user.ID, BatchID: batchID, HoldAmount: 2,
	})
	require.NoError(t, err)
	requestID := service.BatchImageCaptureRequestID(batchID)
	_, err = repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID: requestID, APIKeyID: apiKey.ID, UserID: user.ID, BatchID: batchID,
		HoldAmount: 2, ActualAmount: 1.25,
		UsageLog: &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: 0,
			RequestID: requestID, Model: "invalid-account", ActualCost: 1.25,
		},
	})
	require.Error(t, err)

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 98, balance, 0.000001, "the original hold remains reserved")
	require.InDelta(t, 2, frozen, 0.000001, "failed capture must not release frozen funds")
	var claimCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2`, requestID, apiKey.ID).Scan(&claimCount))
	require.Zero(t, claimCount, "failed usage insert must roll back the capture claim")
}

func TestUsageBillingRepositoryApply_DeduplicatesSubscriptionBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-sub-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-sub-" + uuid.NewString(),
		Name:    "billing-sub",
	})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:  user.ID,
		GroupID: group.ID,
	})
	cleanupUsageBillingSubscriptionFixture(t, ctx, user.ID, group.ID, apiKey.ID, subscription.ID)

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        0,
		SubscriptionID:   &subscription.ID,
		SubscriptionCost: 2.5,
	}

	beforeCharge := time.Now().UTC()
	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var dailyUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&dailyUsage))
	require.InDelta(t, 2.5, dailyUsage, 0.000001)

	var fiveHourUsage float64
	var fiveHourWindowStart time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT five_hour_usage_usd, five_hour_window_start FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&fiveHourUsage, &fiveHourWindowStart))
	require.InDelta(t, 2.5, fiveHourUsage, 0.000001)
	require.WithinDuration(t, beforeCharge, fiveHourWindowStart, 5*time.Second)
}

func TestUsageBillingRepositoryApply_ResetsExpiredFiveHourSubscriptionWindow(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-five-hour-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-five-hour-group-" + uuid.NewString(),
		Platform:         service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-five-hour-" + uuid.NewString(),
		Name:    "billing-five-hour",
	})
	expiredStart := time.Now().UTC().Add(-6 * time.Hour)
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:              user.ID,
		GroupID:             group.ID,
		FiveHourUsageUSD:    9.5,
		FiveHourWindowStart: &expiredStart,
	})
	cleanupUsageBillingSubscriptionFixture(t, ctx, user.ID, group.ID, apiKey.ID, subscription.ID)

	beforeCharge := time.Now().UTC()
	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		SubscriptionID:   &subscription.ID,
		SubscriptionCost: 2.5,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)

	var fiveHourUsage float64
	var fiveHourWindowStart time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT five_hour_usage_usd, five_hour_window_start FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&fiveHourUsage, &fiveHourWindowStart))
	require.InDelta(t, 2.5, fiveHourUsage, 0.000001)
	require.WithinDuration(t, beforeCharge, fiveHourWindowStart, 5*time.Second)
}

func cleanupPersistedUsageLogFixture(t *testing.T, ctx context.Context, userID, apiKeyID, accountID int64, requestID string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{"DELETE FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", []any{requestID, apiKeyID}},
			{"DELETE FROM usage_billing_dedup_archive WHERE api_key_id = $1", []any{apiKeyID}},
			{"DELETE FROM usage_billing_dedup WHERE api_key_id = $1", []any{apiKeyID}},
			{"DELETE FROM api_keys WHERE id = $1", []any{apiKeyID}},
			{"DELETE FROM accounts WHERE id = $1", []any{accountID}},
			{"DELETE FROM users WHERE id = $1", []any{userID}},
		} {
			_, err := integrationDB.ExecContext(cleanupCtx, statement.query, statement.args...)
			require.NoError(t, err, "cleanup persisted usage log fixture")
		}
	})
}

func cleanupUsageBillingSubscriptionFixture(t *testing.T, ctx context.Context, userID, groupID, apiKeyID, subscriptionID int64) {
	t.Helper()
	t.Cleanup(func() {
		for _, statement := range []struct {
			query string
			id    int64
		}{
			{"DELETE FROM usage_billing_dedup_archive WHERE api_key_id = $1", apiKeyID},
			{"DELETE FROM usage_billing_dedup WHERE api_key_id = $1", apiKeyID},
			{"DELETE FROM user_subscriptions WHERE id = $1", subscriptionID},
			{"DELETE FROM api_keys WHERE id = $1", apiKeyID},
			{"DELETE FROM users WHERE id = $1", userID},
			{"DELETE FROM groups WHERE id = $1", groupID},
		} {
			_, err := integrationDB.ExecContext(ctx, statement.query, statement.id)
			require.NoError(t, err, "cleanup usage billing subscription fixture")
		}
	})
}

func TestUsageBillingRepositoryApply_RequestFingerprintConflict(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-conflict-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-conflict-" + uuid.NewString(),
		Name:   "billing-conflict",
	})

	requestID := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	})
	require.NoError(t, err)

	_, err = repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 2.50,
	})
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestUsageBillingRepositoryApply_UpdatesAccountQuota(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-account-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-account-" + uuid.NewString(),
		Name:   "billing-account",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-quota-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_limit": 100.0,
		},
	})

	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeAPIKey,
		AccountQuotaCost: 3.5,
	})
	require.NoError(t, err)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE((extra->>'quota_used')::numeric, 0) FROM accounts WHERE id = $1", account.ID).Scan(&quotaUsed))
	require.InDelta(t, 3.5, quotaUsed, 0.000001)
}

func TestUsageBillingRepositoryApply_EnqueuesSchedulerOutboxOnQuotaCrossing(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	newFixture := func(t *testing.T, extra map[string]any) (int64, int64) {
		t.Helper()
		user := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("usage-billing-outbox-user-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()),
			PasswordHash: "hash",
		})
		apiKey := mustCreateApiKey(t, client, &service.APIKey{
			UserID: user.ID,
			Key:    "sk-usage-billing-outbox-" + uuid.NewString(),
			Name:   "billing-outbox",
		})
		account := mustCreateAccount(t, client, &service.Account{
			Name:  "usage-billing-outbox-" + uuid.NewString(),
			Type:  service.AccountTypeAPIKey,
			Extra: extra,
		})
		return apiKey.ID, account.ID
	}

	outboxCountFor := func(t *testing.T, accountID int64) int {
		t.Helper()
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1 AND account_id = $2",
			service.SchedulerOutboxEventAccountChanged, accountID,
		).Scan(&count))
		return count
	}

	t.Run("daily_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_daily_limit": 10.0,
		})
		// 第一次低于日限额：不应入队 outbox
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 4,
		})
		require.NoError(t, err)
		require.Equal(t, 0, outboxCountFor(t, accountID), "below limit should not enqueue")

		// 第二次跨越日限额：应入队一次 outbox
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 8,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "crossing daily limit should enqueue once")

		// 再次递增（已超）：不应重复入队
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 2,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "subsequent increments beyond limit should not re-enqueue")
	})

	t.Run("weekly_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_weekly_limit": 10.0,
		})
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 15, // 单次即跨越
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "single-shot crossing weekly limit should enqueue once")
	})
}

func TestDashboardAggregationRepositoryCleanupUsageBillingDedup_BatchDeletesOldRows(t *testing.T) {
	ctx := context.Background()
	repo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	oldRequestID := "dedup-old-" + uuid.NewString()
	newRequestID := "dedup-new-" + uuid.NewString()
	oldCreatedAt := time.Now().UTC().AddDate(0, 0, -400)
	newCreatedAt := time.Now().UTC().Add(-time.Hour)

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, created_at)
		VALUES ($1, 1, $2, $3), ($4, 1, $5, $6)
	`,
		oldRequestID, strings.Repeat("a", 64), oldCreatedAt,
		newRequestID, strings.Repeat("b", 64), newCreatedAt,
	)
	require.NoError(t, err)

	require.NoError(t, repo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	var oldCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", oldRequestID).Scan(&oldCount))
	require.Equal(t, 0, oldCount)

	var newCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", newRequestID).Scan(&newCount))
	require.Equal(t, 1, newCount)

	var archivedCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup_archive WHERE request_id = $1", oldRequestID).Scan(&archivedCount))
	require.Equal(t, 1, archivedCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesAgainstArchivedKey(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	aggRepo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-archive-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-archive-" + uuid.NewString(),
		Name:   "billing-archive",
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_dedup
		SET created_at = $1
		WHERE request_id = $2 AND api_key_id = $3
	`, time.Now().UTC().AddDate(0, 0, -400), requestID, apiKey.ID)
	require.NoError(t, err)
	require.NoError(t, aggRepo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)
}
