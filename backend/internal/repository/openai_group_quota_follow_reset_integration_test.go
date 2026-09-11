//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGroupQuotaFollowResetRepository_EndToEnd(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type)
		VALUES ($1, 'openai', 'oauth')
		RETURNING id
	`, fmt.Sprintf("quota-follow-account-%d", suffix)).Scan(&accountID))

	groupIDs := make([]int64, 0, 3)
	for i, config := range []struct {
		monthlyLimit   any
		includeMonthly bool
	}{
		{monthlyLimit: 100.0, includeMonthly: true},
		{monthlyLimit: 100.0, includeMonthly: false},
		{monthlyLimit: nil, includeMonthly: true},
	} {
		var groupID int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO groups (
				name, platform, subscription_type, status,
				monthly_limit_usd, quota_reset_source_account_id,
				quota_reset_source_account_name, quota_reset_include_monthly,
				quota_reset_config_version
			)
			VALUES ($1, 'openai', 'subscription', 'active', $2, $3, $4, $5, 1)
			RETURNING id
		`,
			fmt.Sprintf("quota-follow-group-%d-%d", suffix, i),
			config.monthlyLimit,
			accountID,
			fmt.Sprintf("quota-follow-account-%d", suffix),
			config.includeMonthly,
		).Scan(&groupID))
		groupIDs = append(groupIDs, groupID)
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO account_groups (account_id, group_id) VALUES ($1, $2)`, accountID, groupID)
		require.NoError(t, err)
	}

	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, 'test')
		RETURNING id
	`, fmt.Sprintf("quota-follow-%d@example.test", suffix)).Scan(&userID))

	now := time.Now().UTC().Truncate(time.Second)
	subscriptionIDs := make([]int64, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		var subscriptionID int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO user_subscriptions (
				user_id, group_id, starts_at, expires_at, status,
				daily_usage_usd, weekly_usage_usd, monthly_usage_usd, five_hour_usage_usd
			)
			VALUES ($1, $2, $3, $4, 'active', 11, 22, 33, 44)
			RETURNING id
		`, userID, groupID, now.Add(-time.Hour), now.Add(30*24*time.Hour)).Scan(&subscriptionID))
		subscriptionIDs = append(subscriptionIDs, subscriptionID)
	}

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM group_quota_follow_reset_events WHERE group_id = ANY($1)`, pq.Array(groupIDs))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM openai_oauth_weekly_reset_observations WHERE account_id = $1`, accountID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM user_subscriptions WHERE id = ANY($1)`, pq.Array(subscriptionIDs))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id = ANY($1)`, pq.Array(groupIDs))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	repo := NewOpenAIGroupQuotaFollowResetRepository(integrationEntClient, integrationDB)
	firstResetAt := now.Add(7 * 24 * time.Hour)
	created, err := repo.ObserveWeeklyReset(ctx, accountID, firstResetAt, now)
	require.NoError(t, err)
	require.Zero(t, created, "the first observation only establishes a baseline")

	created, err = repo.ObserveWeeklyReset(ctx, accountID, firstResetAt, now.Add(time.Minute))
	require.NoError(t, err)
	require.Zero(t, created, "the same upstream reset time must be idempotent")

	created, err = repo.ObserveWeeklyReset(ctx, accountID, firstResetAt.Add(time.Minute), now.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, created, "a later next-reset before the current window expires must not reset groups")

	created, err = repo.ObserveWeeklyReset(ctx, accountID, firstResetAt.Add(time.Minute), firstResetAt)
	require.NoError(t, err)
	require.Zero(t, created, "clock skew after the current window expires must not reset groups")

	secondResetAt := firstResetAt.Add(7 * 24 * time.Hour)
	created, err = repo.ObserveWeeklyReset(ctx, accountID, secondResetAt, firstResetAt)
	require.NoError(t, err)
	require.Equal(t, 3, created, "one source observation must create one event per bound group")

	for range groupIDs {
		result, processErr := repo.ProcessNextPending(ctx)
		require.NoError(t, processErr)
		require.NotNil(t, result)
		require.Equal(t, 1, result.AffectedSubscriptions)
	}
	result, err := repo.ProcessNextPending(ctx)
	require.NoError(t, err)
	require.Nil(t, result)

	for i, subscriptionID := range subscriptionIDs {
		var daily, weekly, monthly, fiveHour float64
		var marker int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
			       five_hour_usage_usd, quota_follow_reset_event_id
			FROM user_subscriptions WHERE id = $1
		`, subscriptionID).Scan(&daily, &weekly, &monthly, &fiveHour, &marker))
		require.Zero(t, daily)
		require.Zero(t, weekly)
		require.Zero(t, fiveHour)
		require.Positive(t, marker)
		if i == 0 {
			require.Zero(t, monthly, "monthly usage resets only when enabled and a limit exists")
		} else {
			require.Equal(t, 33.0, monthly)
		}
	}

	created, err = repo.ObserveWeeklyReset(ctx, accountID, secondResetAt, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, created)

	thirdResetAt := secondResetAt.Add(7 * 24 * time.Hour)
	created, err = repo.ObserveWeeklyReset(ctx, accountID, thirdResetAt, secondResetAt)
	require.NoError(t, err)
	require.Equal(t, 3, created)

	// The request that reveals the new upstream window must apply the pending
	// reset before recording its own cost. A later worker pass must not erase it.
	userSubscriptionRepo := NewUserSubscriptionRepository(integrationEntClient)
	require.NoError(t, userSubscriptionRepo.IncrementUsage(ctx, subscriptionIDs[0], 1.25))
	for range groupIDs {
		result, processErr := repo.ProcessNextPending(ctx)
		require.NoError(t, processErr)
		require.NotNil(t, result)
	}
	var daily, weekly, monthly, fiveHour float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd, five_hour_usage_usd
		FROM user_subscriptions WHERE id = $1
	`, subscriptionIDs[0]).Scan(&daily, &weekly, &monthly, &fiveHour))
	require.Equal(t, 1.25, daily)
	require.Equal(t, 1.25, weekly)
	require.Equal(t, 1.25, monthly)
	require.Equal(t, 1.25, fiveHour)

	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, accountID)
	require.NoError(t, err)
	created, err = repo.ObserveWeeklyReset(ctx, accountID, thirdResetAt.Add(7*24*time.Hour), now.Add(4*time.Hour))
	require.NoError(t, err)
	require.Zero(t, created, "a deleted source cannot emit reset events")

	groupRepo := NewGroupRepository(integrationEntClient, integrationDB)
	group, err := groupRepo.GetByIDLite(ctx, groupIDs[0])
	require.NoError(t, err)
	require.Equal(t, accountID, *group.QuotaResetSourceAccountID)
	require.False(t, group.QuotaResetSourceValid, "deleted sources remain configured but are reported invalid")
}

type quotaFollowFixture struct {
	accountID, groupID, subscriptionID int64
	now, baseline                      time.Time
	repo                               service.OpenAIGroupQuotaFollowResetRepository
}

func newQuotaFollowFixture(t *testing.T) quotaFollowFixture {
	t.Helper()
	ctx := context.Background()
	f := quotaFollowFixture{now: time.Now().UTC().Truncate(time.Second)}
	f.baseline = f.now.Add(7 * 24 * time.Hour)
	f.repo = NewOpenAIGroupQuotaFollowResetRepository(integrationEntClient, integrationDB)
	var userID int64
	suffix := time.Now().UnixNano()
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name, platform, type) VALUES ($1,'openai','oauth') RETURNING id`, fmt.Sprintf("quota-race-%d", suffix)).Scan(&f.accountID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO groups(name, platform, subscription_type, monthly_limit_usd, quota_reset_source_account_id, quota_reset_config_version, quota_reset_include_monthly) VALUES ($1,'openai','subscription',100,$2,1,true) RETURNING id`, fmt.Sprintf("quota-race-%d", suffix), f.accountID).Scan(&f.groupID))
	require.NoError(t, func() error {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO account_groups (account_id, group_id) VALUES ($1, $2)`, f.accountID, f.groupID)
		return err
	}())
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES ($1,'test') RETURNING id`, fmt.Sprintf("quota-race-%d@example.test", suffix)).Scan(&userID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,five_hour_usage_usd,five_hour_window_start) VALUES ($1,$2,$3,$4,'active',11,11,11,11,$5) RETURNING id`, userID, f.groupID, f.now.Add(-time.Hour), f.now.Add(30*24*time.Hour), f.now).Scan(&f.subscriptionID))
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM group_quota_follow_reset_events WHERE group_id=$1`, f.groupID)
		_, _ = integrationDB.Exec(`DELETE FROM openai_oauth_weekly_reset_observations WHERE account_id=$1`, f.accountID)
		_, _ = integrationDB.Exec(`DELETE FROM user_subscriptions WHERE id=$1`, f.subscriptionID)
		_, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=$1`, f.groupID)
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, f.accountID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})
	created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, f.baseline, f.now)
	require.NoError(t, err)
	require.Zero(t, created)
	return f
}

func (f quotaFollowFixture) observe(t *testing.T, window int) {
	t.Helper()
	next := f.baseline.Add(time.Duration(window) * 7 * 24 * time.Hour)
	created, err := f.repo.ObserveWeeklyReset(context.Background(), f.accountID, next, f.baseline.Add(time.Duration(window-1)*7*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, created)
}

func TestQuotaFollowReset_SavePreservesCurrentBaseline(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	repo := NewGroupRepository(integrationEntClient, integrationDB)
	stale, err := repo.GetByIDLite(ctx, f.groupID)
	require.NoError(t, err)
	f.observe(t, 1)
	stale.Description = "ordinary edit from an older snapshot"
	require.NoError(t, repo.Update(ctx, stale))
	require.Equal(t, f.baseline.Add(7*24*time.Hour), *stale.QuotaResetSourceResetAt)
	created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, f.baseline.Add(7*24*time.Hour), f.now.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, created)

	// The new binding must refresh its baseline inside the save transaction,
	// even if the service previously read an older value or no observation.
	stale.QuotaResetSourceChanged = true
	stale.QuotaResetConfigVersion++
	stale.QuotaResetSourceResetAt = nil
	require.NoError(t, repo.Update(ctx, stale))
	require.Equal(t, f.baseline.Add(7*24*time.Hour), *stale.QuotaResetSourceResetAt)
	result, err := f.repo.ProcessNextPending(ctx)
	require.NoError(t, err)
	require.True(t, result.Skipped, "old-configuration events must not apply after rebinding")

	stale.QuotaResetConfigVersion--
	require.Error(t, repo.Update(ctx, stale), "stale configuration cannot overwrite a newer binding")
}

func TestQuotaFollowReset_InvalidSourceDoesNotApplyPendingEvents(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"deleted", `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`},
		{"apikey", `UPDATE accounts SET type='apikey' WHERE id=$1`},
		{"other_platform", `UPDATE accounts SET platform='anthropic' WHERE id=$1`},
		{"disabled", `UPDATE groups SET quota_reset_source_account_id=NULL WHERE quota_reset_source_account_id=$1`},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			f.observe(t, 1)
			_, err := integrationDB.Exec(change.sql, f.accountID)
			require.NoError(t, err)
			require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(context.Background(), f.subscriptionID, 1.25))
			var daily, monthly float64
			require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&daily, &monthly))
			require.Equal(t, 12.25, daily)
			require.Equal(t, 12.25, monthly)
			result, err := f.repo.ProcessNextPending(context.Background())
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.Skipped)
		})
	}
}

func TestQuotaFollowReset_CreateUsesLatestBaselineWithoutResetting(t *testing.T) {
	f := newQuotaFollowFixture(t)
	f.observe(t, 1)
	ctx := context.Background()
	repo := NewGroupRepository(integrationEntClient, integrationDB)
	copy, err := repo.GetByIDLite(ctx, f.groupID)
	require.NoError(t, err)
	copy.ID = 0
	copy.Name += "-new-binding"
	copy.QuotaResetSourceResetAt = &f.baseline // A stale pre-save snapshot.
	require.NoError(t, repo.Create(ctx, copy))
	t.Cleanup(func() { _, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=$1`, copy.ID) })
	require.Equal(t, f.baseline.Add(7*24*time.Hour), *copy.QuotaResetSourceResetAt)
	var events int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM group_quota_follow_reset_events WHERE group_id=$1`, copy.ID).Scan(&events))
	require.Zero(t, events)
}

func TestQuotaFollowReset_WorkersCannotOvertakeEarlierMonthlyReset(t *testing.T) {
	f := newQuotaFollowFixture(t)
	f.observe(t, 1)
	_, err := integrationDB.Exec(`UPDATE groups SET quota_reset_include_monthly=false WHERE id=$1`, f.groupID)
	require.NoError(t, err)
	f.observe(t, 2)
	tx, err := integrationDB.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var id int64
	require.NoError(t, tx.QueryRow(`SELECT id FROM group_quota_follow_reset_events WHERE group_id=$1 ORDER BY id LIMIT 1 FOR UPDATE`, f.groupID).Scan(&id))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := f.repo.ProcessNextPending(ctx)
	require.NoError(t, err)
	require.Nil(t, result, "another worker cannot overtake the locked event for the same group")
	require.NoError(t, tx.Rollback())
	for range 2 {
		result, err = f.repo.ProcessNextPending(ctx)
		require.NoError(t, err)
		require.NotNil(t, result)
	}
	var monthly float64
	require.NoError(t, integrationDB.QueryRow(`SELECT monthly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&monthly))
	require.Zero(t, monthly)
}

func TestQuotaFollowReset_ConcurrentObservationBillingAndWorkers(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	counts := make(chan int, 8)
	for range 8 {
		wg.Go(func() {
			n, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, f.baseline.Add(7*24*time.Hour), f.now.Add(time.Minute))
			errs <- err
			counts <- n
		})
	}
	wg.Wait()
	close(counts)
	var total int
	for n := range counts {
		total += n
	}
	require.Equal(t, 1, total, "concurrent observations must create exactly one event")
	for range 24 {
		wg.Go(func() {
			errs <- NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 1.25)
		})
	}
	for range 4 {
		wg.Go(func() { _, err := f.repo.ProcessNextPending(ctx); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var daily, weekly, monthly, fiveHour float64
	require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd, five_hour_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&daily, &weekly, &monthly, &fiveHour))
	for _, usage := range []float64{daily, weekly, monthly, fiveHour} {
		require.Equal(t, 30.0, usage, "worker must not erase charges that already consumed the event")
	}
}

func TestQuotaFollowReset_IncrementRespectsCallerTransaction(t *testing.T) {
	f := newQuotaFollowFixture(t)
	f.observe(t, 1)
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx := dbent.NewTxContext(context.Background(), tx)
	require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 1.25))
	var daily float64
	require.NoError(t, scanSingleRow(ctx, tx.Client(), `SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, []any{f.subscriptionID}, &daily))
	require.Equal(t, 1.25, daily)
	require.NoError(t, tx.Rollback())
	require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&daily))
	require.Equal(t, 11.0, daily, "both reset and charge must roll back together")
}
