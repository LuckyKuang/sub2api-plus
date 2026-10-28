//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Real Redis and PostgreSQL jointly enforce recovery: an expired processing
// owner may finish late, but both attempts share one durable billing identity.
func TestMediaBillingExpiredOwnerAndRetryChargeOnceAcrossInstances(t *testing.T) {
	ctx := context.Background()
	redisClient := testRedis(t)
	a, b := NewGatewayCache(redisClient), NewGatewayCache(redisClient)
	client := testEntClient(t)
	ledger := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("media-recovery-%s@example.test", uuid.NewString()), PasswordHash: "hash", Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-media-recovery-" + uuid.NewString(), Name: "media-recovery", Quota: 100})
	account := mustCreateAccount(t, client, &service.Account{Name: "media-recovery-" + uuid.NewString(), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey})
	taskID := "recovery-" + uuid.NewString()
	requestID := service.StableGrokVideoBillingRequestID(taskID)
	cleanupPersistedUsageLogFixture(t, ctx, user.ID, key.ID, account.ID, requestID)
	command := func() *service.UsageBillingCommand {
		return &service.UsageBillingCommand{RequestID: requestID, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID,
			AccountType: service.AccountTypeAPIKey, Model: "grok-imagine-video", BalanceCost: 12, APIKeyQuotaCost: 12,
			UsageLog: &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID, Model: "grok-imagine-video", TotalCost: 2, ActualCost: 12}}
	}
	first, err := ledger.Apply(ctx, command())
	require.NoError(t, err)
	require.True(t, first.Applied)
	claimKey := fmt.Sprintf("%d:%d:%s", user.ID, key.ID, taskID)
	legacyKey := "grok_video_billed:" + claimKey
	require.NoError(t, redisClient.Set(ctx, legacyKey, "1", 48*time.Hour).Err())
	redisKey := grokVideoBilledPrefix + claimKey
	// The legacy value also represented already committed success. The new
	// namespace must recheck durable SQL evidence without a second charge.
	acquired, err := a.ClaimGrokVideoBilled(ctx, claimKey, "owner-a", 20*time.Millisecond)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Eventually(t, func() bool { return redisClient.Exists(ctx, redisKey).Val() == 0 }, 2*time.Second, time.Millisecond)
	acquired, err = b.ClaimGrokVideoBilled(ctx, claimKey, "owner-b", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	late, err := ledger.Apply(ctx, command())
	require.NoError(t, err)
	require.False(t, late.Applied)
	// Worker A returns after its Redis lease expired. Its stale completion and
	// cleanup cannot overwrite B, whose SQL retry is also idempotent.
	completed, err := a.CompleteGrokVideoBilled(ctx, claimKey, "owner-a", 48*time.Hour)
	require.NoError(t, err)
	require.False(t, completed)
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, claimKey, "owner-a"))
	second, err := ledger.Apply(ctx, command())
	require.NoError(t, err)
	require.False(t, second.Applied)
	completed, err = b.CompleteGrokVideoBilled(ctx, claimKey, "owner-b", 48*time.Hour)
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, claimKey, "owner-a"))
	acquired, err = a.ClaimGrokVideoBilled(ctx, claimKey, "owner-c", 30*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)
	var balance, quota float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id=$1", key.ID).Scan(&quota))
	require.InDelta(t, 88.0, balance, 1e-8, "$2 × platform 2 × model 3 must be charged once")
	require.InDelta(t, 12.0, quota, 1e-8)
	legacyValue, err := redisClient.Get(ctx, legacyKey).Result()
	require.NoError(t, err)
	require.Equal(t, "1", legacyValue, "upgrading the claim generation preserves legacy cache records")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, key.ID).Scan(&count))
	require.Equal(t, 1, count)
}

// A lease can expire during slow SQL work. Two independent billing repositories
// may then settle the same accepted video concurrently, but money and usage
// must commit once, regardless of which processing owner wins the SQL race.
func TestMediaBillingExpiredLeaseConcurrentSettlementChargesOnce(t *testing.T) {
	ctx := context.Background()
	redisClient := testRedis(t)
	a, b := NewGatewayCache(redisClient), NewGatewayCache(redisClient)
	client := testEntClient(t)
	ledgers := []service.UsageBillingRepository{
		NewUsageBillingRepository(client, integrationDB),
		NewUsageBillingRepository(client, integrationDB),
	}
	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("media-concurrent-%s@example.test", uuid.NewString()), PasswordHash: "hash", Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-media-concurrent-" + uuid.NewString(), Name: "media-concurrent", Quota: 100})
	account := mustCreateAccount(t, client, &service.Account{Name: "media-concurrent-" + uuid.NewString(), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey})
	taskID := "concurrent-" + uuid.NewString()
	requestID := service.StableGrokVideoBillingRequestID(taskID)
	cleanupPersistedUsageLogFixture(t, ctx, user.ID, key.ID, account.ID, requestID)
	claimKey := fmt.Sprintf("%d:%d:%s", user.ID, key.ID, taskID)
	redisKey := grokVideoBilledPrefix + claimKey
	acquired, err := a.ClaimGrokVideoBilled(ctx, claimKey, "owner-a", 20*time.Millisecond)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Eventually(t, func() bool { return redisClient.Exists(ctx, redisKey).Val() == 0 }, 2*time.Second, time.Millisecond)
	acquired, err = b.ClaimGrokVideoBilled(ctx, claimKey, "owner-b", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)

	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, ledger := range ledgers {
		go func(ledger service.UsageBillingRepository) {
			// Each attempt owns its command and usage row. Only the stable
			// business identity and independently expected $12 amount match.
			command := &service.UsageBillingCommand{RequestID: requestID, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID,
				AccountType: service.AccountTypeAPIKey, Model: "grok-imagine-video", BalanceCost: 12, APIKeyQuotaCost: 12,
				UsageLog: &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID, Model: "grok-imagine-video", TotalCost: 2, ActualCost: 12}}
			ready.Done()
			<-start
			result, applyErr := ledger.Apply(ctx, command)
			results <- outcome{result: result, err: applyErr}
		}(ledger)
	}
	ready.Wait()
	close(start)
	applied := 0
	for i := 0; i < 2; i++ {
		result := <-results
		require.NoError(t, result.err)
		require.NotNil(t, result.result)
		if result.result.Applied {
			applied++
		}
	}
	require.Equal(t, 1, applied, "exactly one concurrent transaction may apply the accepted task charge")

	completed, err := a.CompleteGrokVideoBilled(ctx, claimKey, "owner-a", 48*time.Hour)
	require.NoError(t, err)
	require.False(t, completed, "SQL success cannot restore an expired Redis owner")
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, claimKey, "owner-a"))
	owner, err := redisClient.Get(ctx, redisKey).Result()
	require.NoError(t, err)
	require.Equal(t, "owner-b", owner, "old completion and cleanup must preserve the successor")
	completed, err = b.CompleteGrokVideoBilled(ctx, claimKey, "owner-b", 48*time.Hour)
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, a.ReleaseGrokVideoBilled(ctx, claimKey, "owner-a"))
	completed, err = a.CompleteGrokVideoBilled(ctx, claimKey, "owner-a", time.Minute)
	require.NoError(t, err)
	require.False(t, completed)
	marker, err := redisClient.Get(ctx, redisKey).Result()
	require.NoError(t, err)
	require.Equal(t, "done", marker, "old cleanup and completion cannot change durable-success suppression")

	var balance, quota float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id=$1", key.ID).Scan(&quota))
	require.InDelta(t, 88.0, balance, 1e-8)
	require.InDelta(t, 12.0, quota, 1e-8)
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, key.ID).Scan(&usageCount))
	require.Equal(t, 1, usageCount)
}
