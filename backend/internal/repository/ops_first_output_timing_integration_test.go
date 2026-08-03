//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOpsTTFTAggregationUsesStrictTokenSamplesAcrossMixedModalities(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "ops-ttft-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{
		Name:           prefix,
		Platform:       service.PlatformOpenAI,
		RateMultiplier: 1,
	})
	user := mustCreateUser(t, client, &service.User{Email: prefix + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-" + prefix,
		GroupID: &group.ID,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     prefix,
		Platform: service.PlatformOpenAI,
	})

	// Keep the test in an installed usage_logs partition and away from ordinary
	// current-time fixtures. The integration schema creates next month's partition.
	bucketStart := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Hour)
	bucketEnd := bucketStart.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ops_metrics_hourly WHERE bucket_start = $1", bucketStart)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM usage_logs WHERE request_id LIKE $1", prefix+"%")
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	usageRepo := newUsageLogRepositoryWithSQL(client, integrationDB)
	createUsage := func(suffix string, firstTokenMs, firstOutputMs *int, firstOutputKind *string) {
		t.Helper()
		_, err := usageRepo.Create(ctx, &service.UsageLog{
			UserID:          user.ID,
			APIKeyID:        apiKey.ID,
			AccountID:       account.ID,
			RequestID:       prefix + suffix,
			Model:           "gpt-test",
			GroupID:         &group.ID,
			InputTokens:     1,
			OutputTokens:    1,
			Stream:          true,
			FirstTokenMs:    firstTokenMs,
			FirstOutputMs:   firstOutputMs,
			FirstOutputKind: firstOutputKind,
			CreatedAt:       bucketStart.Add(10 * time.Minute),
		})
		require.NoError(t, err)
	}

	legacyFirstToken := 5
	imageFirstOutput := 10
	mixedFirstToken := 40
	textFirstToken := 20
	imageKind := "image"
	textKind := "text"
	createUsage("-legacy", &legacyFirstToken, nil, nil)
	createUsage("-image", nil, &imageFirstOutput, &imageKind)
	createUsage("-mixed", &mixedFirstToken, &imageFirstOutput, &imageKind)
	createUsage("-text", &textFirstToken, &textFirstToken, &textKind)

	opsRepo := NewOpsRepository(integrationDB).(*opsRepository)
	filter := &service.OpsDashboardFilter{
		StartTime: bucketStart,
		EndTime:   bucketEnd,
		Platform:  service.PlatformOpenAI,
		GroupID:   &group.ID,
	}
	_, rawTTFT, rawSampleCount, err := opsRepo.queryUsageLatency(ctx, filter, bucketStart, bucketEnd)
	require.NoError(t, err)
	require.EqualValues(t, 2, rawSampleCount)
	require.NotNil(t, rawTTFT.P50)
	require.Equal(t, 30, *rawTTFT.P50)
	require.NotNil(t, rawTTFT.Avg)
	require.Equal(t, 30, *rawTTFT.Avg)

	require.NoError(t, opsRepo.UpsertHourlyMetrics(ctx, bucketStart, bucketEnd))
	var preaggSampleCount int64
	var preaggP50 sql.NullInt64
	var preaggAvg sql.NullFloat64
	err = integrationDB.QueryRowContext(ctx, `
SELECT ttft_sample_count, ttft_p50_ms, ttft_avg_ms
FROM ops_metrics_hourly
WHERE bucket_start = $1 AND platform = $2 AND group_id = $3`,
		bucketStart,
		service.PlatformOpenAI,
		group.ID,
	).Scan(&preaggSampleCount, &preaggP50, &preaggAvg)
	require.NoError(t, err)
	require.EqualValues(t, 2, preaggSampleCount)
	require.True(t, preaggP50.Valid)
	require.EqualValues(t, 30, preaggP50.Int64)
	require.True(t, preaggAvg.Valid)
	require.Equal(t, 30.0, preaggAvg.Float64)
}
