//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestQuotaFollowReset_UsageCacheRejectsLateSnapshots(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	repo := NewAccountRepository(integrationEntClient, integrationDB, nil)
	now := f.now.Truncate(time.Second)
	newer := map[string]any{
		"codex_usage_updated_at":       now.Add(900 * time.Millisecond).Format(time.RFC3339Nano),
		"codex_7d_reset_at":            f.baseline.Add(24 * time.Hour).Format(time.RFC3339),
		"codex_7d_used_percent":        7.0,
		"codex_secondary_used_percent": 7.0,
	}
	require.NoError(t, repo.UpdateExtra(ctx, f.accountID, newer))
	for _, timestamp := range []string{
		now.Add(100 * time.Millisecond).Format(time.RFC3339Nano),
		now.Add(900 * time.Millisecond).In(time.FixedZone("test", 8*60*60)).Format(time.RFC3339Nano),
	} {
		older := map[string]any{
			"codex_usage_updated_at":       timestamp,
			"codex_7d_reset_at":            f.baseline.Format(time.RFC3339),
			"codex_7d_used_percent":        80.0,
			"codex_secondary_used_percent": 80.0,
			"mixed_scheduling":             true,
		}
		require.NoError(t, repo.UpdateExtra(ctx, f.accountID, older))
		account, err := repo.GetByID(ctx, f.accountID)
		require.NoError(t, err)
		for key, value := range newer {
			require.Equal(t, value, account.Extra[key], key)
		}
		require.Equal(t, true, account.Extra["mixed_scheduling"], "unrelated settings must still merge")
		require.Equal(t, 80.0, older["codex_7d_used_percent"], "do not mutate the caller's snapshot")
	}
	_, err := integrationDB.Exec(`UPDATE accounts SET extra=extra || '{"codex_usage_updated_at":"legacy-invalid"}'::jsonb WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateExtra(ctx, f.accountID, newer), "invalid legacy timestamps must not prevent fresh writes")
	require.ErrorIs(t, repo.UpdateExtra(ctx, -1, newer), service.ErrAccountNotFound)
}

func TestQuotaFollowReset_UsageCacheRechecksAfterConcurrentWrite(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	newer, err := json.Marshal(map[string]any{
		"codex_usage_updated_at": f.now.Add(time.Second).Format(time.RFC3339Nano),
		"codex_7d_used_percent":  7.0,
	})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb) || $2::jsonb WHERE id=$1`, f.accountID, string(newer))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- NewAccountRepository(integrationEntClient, integrationDB, nil).UpdateExtra(ctx, f.accountID, map[string]any{
			"codex_usage_updated_at": f.now.Format(time.RFC3339Nano),
			"codex_7d_used_percent":  80.0,
		})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%accounts%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("late cache write did not finish")
	}
	var used float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT (extra->>'codex_7d_used_percent')::double precision FROM accounts WHERE id=$1`, f.accountID).Scan(&used))
	require.Equal(t, 7.0, used, "a waiting old writer must recheck the newly committed snapshot")
}

func TestQuotaFollowReset_MigrationPreservesDeployedConfigurations(t *testing.T) {
	tx, err := integrationDB.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := fmt.Sprintf("quota_follow_upgrade_%d", time.Now().UnixNano())
	_, err = tx.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	_, err = tx.Exec(`SET LOCAL search_path TO ` + schema)
	require.NoError(t, err)
	_, err = tx.Exec(`
		CREATE TABLE groups(id BIGSERIAL PRIMARY KEY, deleted_at TIMESTAMPTZ);
		CREATE TABLE user_subscriptions(id BIGSERIAL PRIMARY KEY, group_id BIGINT, deleted_at TIMESTAMPTZ, weekly_usage_usd NUMERIC);
	`)
	require.NoError(t, err)
	old, err := migrations.FS.ReadFile("258_openai_group_quota_follow_reset.sql")
	require.NoError(t, err)
	_, err = tx.Exec(string(old))
	require.NoError(t, err)
	_, err = tx.Exec(`
		INSERT INTO groups(id, quota_reset_source_account_id, quota_reset_source_reset_at, quota_reset_config_version)
		VALUES (1, 42, '2026-09-18T00:00:00Z', 3), (2, NULL, NULL, 0), (3, NULL, '2026-09-17T00:00:00Z', 7);
		INSERT INTO user_subscriptions(group_id, weekly_usage_usd) VALUES (1, 44.45), (2, 44.45), (3, 44.45);
		INSERT INTO openai_oauth_weekly_reset_observations(account_id, reset_at, observed_at)
		VALUES (42, '2026-09-18T00:00:00Z', '2026-09-11T00:00:00Z');
		INSERT INTO group_quota_follow_reset_events(group_id, source_account_id, config_version, upstream_reset_at, effective_at)
		VALUES (1, 42, 3, '2026-09-18T00:00:00Z', '2026-09-11T00:00:00Z');
	`)
	require.NoError(t, err)
	update, err := migrations.FS.ReadFile("265_openai_weekly_reset_observations.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.Exec(string(update))
		require.NoError(t, err, "forward migration must be safe to reapply")
	}
	var unchanged int
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM user_subscriptions WHERE weekly_usage_usd=44.45`).Scan(&unchanged))
	require.Equal(t, 3, unchanged, "upgrade must not reset active, absent or previously disabled configurations")
	var version int64
	var baseline time.Time
	require.NoError(t, tx.QueryRow(`SELECT quota_reset_config_version, quota_reset_source_reset_at FROM groups WHERE id=1`).Scan(&version, &baseline))
	require.Equal(t, int64(3), version)
	require.Equal(t, "2026-09-18T00:00:00Z", baseline.UTC().Format(time.RFC3339))
	var events int
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM group_quota_follow_reset_events WHERE status='pending' AND reset_sequence=0`).Scan(&events))
	require.Equal(t, 1, events, "upgrade must preserve durable pending events")
	_, err = tx.Exec(`INSERT INTO group_quota_follow_reset_events(group_id, source_account_id, config_version, upstream_reset_at, effective_at, reset_sequence)
		VALUES (1,42,3,'2026-09-18T00:00:00Z','2026-09-12T00:00:00Z',1)`)
	require.NoError(t, err, "a newly confirmed reset with the same deadline has a distinct sequence")
}

func TestQuotaFollowReset_ReenableWaitsForCurrentWindow(t *testing.T) {
	for _, previousBinding := range []bool{false, true} {
		t.Run(map[bool]string{false: "first enable with historical account observation", true: "re-enable with old pending event"}[previousBinding], func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			ctx := context.Background()
			if previousBinding {
				f.observe(t, 1)
			}
			groups := NewGroupRepository(integrationEntClient, integrationDB)
			group, err := groups.GetByIDLite(ctx, f.groupID)
			require.NoError(t, err)
			group.QuotaResetSourceChanged = true
			group.QuotaResetConfigVersion++
			group.QuotaResetSourceAccountID = nil
			require.NoError(t, groups.Update(ctx, group))
			require.Nil(t, group.QuotaResetSourceResetAt)
			// Simulate an old client restoring the deadline from the earlier activation.
			group.QuotaResetSourceAccountID = &f.accountID
			group.QuotaResetSourceResetAt = &f.baseline
			group.QuotaResetSourceChanged = true
			group.QuotaResetConfigVersion++
			require.NoError(t, groups.Update(ctx, group))
			require.Nil(t, group.QuotaResetSourceResetAt)
			if previousBinding {
				result, err := f.repo.ProcessNextPending(ctx)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.Skipped)
			}
			at := f.baseline.Add(time.Hour)
			deadline := at.Add(6*24*time.Hour + 22*time.Hour)
			for _, sampleAt := range []time.Time{at, at.Add(time.Minute)} {
				created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(deadline, sampleAt, 7, true))
				require.NoError(t, err)
				require.Zero(t, created, "first confirmed window after activation must not reset")
			}
			group, err = groups.GetByIDLite(ctx, f.groupID)
			require.NoError(t, err)
			require.NotNil(t, group.QuotaResetSourceResetAt)
			require.WithinDuration(t, deadline, *group.QuotaResetSourceResetAt, time.Microsecond)
			var used float64
			require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&used))
			require.Equal(t, 11.0, used)
			// A later reset is followed normally, without backfilling A/U costs.
			created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(deadline, at.Add(2*time.Minute), 0, true))
			require.NoError(t, err)
			require.Zero(t, created)
			created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(deadline, at.Add(3*time.Minute), 0, true))
			require.NoError(t, err)
			require.Equal(t, 1, created)
			result, err := f.repo.ProcessNextPending(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, result.AffectedSubscriptions)
			require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&used))
			require.Zero(t, used)
		})
	}
}

func TestQuotaFollowReset_TwoResetsAtSameDeadlineAreDistinct(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	for cycle := 0; cycle < 2; cycle++ {
		at := f.now.Add(time.Duration(cycle*10+1) * time.Minute)
		_, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, at, 80, true))
		require.NoError(t, err)
		created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, at.Add(time.Minute), 7, true))
		require.NoError(t, err)
		require.Zero(t, created)
		created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, at.Add(2*time.Minute), 7, true))
		require.NoError(t, err)
		require.Equal(t, 1, created)
		require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 1.25))
		result, err := f.repo.ProcessNextPending(ctx)
		require.NoError(t, err)
		require.NotNil(t, result)
		created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, at.Add(3*time.Minute), 8, true))
		require.NoError(t, err)
		require.Zero(t, created)
		var used float64
		require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&used))
		require.Equal(t, 1.25, used, "post-confirmation billing must not be cleared by the worker")
	}
	var events int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM group_quota_follow_reset_events WHERE group_id=$1`, f.groupID).Scan(&events))
	require.Equal(t, 2, events)
}

func TestQuotaFollowReset_PollLeaseAndEligibility(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	ids, err := f.repo.ClaimWeeklyResetSources(ctx, 4)
	require.NoError(t, err)
	require.Contains(t, ids, f.accountID)
	ids, err = f.repo.ClaimWeeklyResetSources(ctx, 4)
	require.NoError(t, err)
	require.NotContains(t, ids, f.accountID, "a second replica cannot claim an active lease")
	_, err = integrationDB.Exec(`UPDATE openai_oauth_weekly_reset_observations SET next_poll_at=NOW()-INTERVAL '1 minute' WHERE account_id=$1`, f.accountID)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE groups SET quota_reset_source_account_id=NULL WHERE id=$1`, f.groupID)
	require.NoError(t, err)
	ids, err = f.repo.ClaimWeeklyResetSources(ctx, 4)
	require.NoError(t, err)
	require.NotContains(t, ids, f.accountID, "disabled sources are not polled")
}

func TestQuotaFollowReset_NewSourceRejectsPreActivationSample(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	_, err := integrationDB.Exec(`DELETE FROM openai_oauth_weekly_reset_observations WHERE account_id=$1`, f.accountID)
	require.NoError(t, err)
	activation := f.now.Add(2 * time.Minute)
	_, err = integrationDB.Exec(`UPDATE groups SET quota_reset_source_reset_at=NULL, updated_at=$2 WHERE id=$1`, f.groupID, activation)
	require.NoError(t, err)
	ids, err := f.repo.ClaimWeeklyResetSources(ctx, 4)
	require.NoError(t, err)
	require.Contains(t, ids, f.accountID, "polling must also claim sources with no observation yet")
	oldSample := weeklyObservation(f.baseline, f.now.Add(time.Minute), 80, true)
	oldSample.ReceivedAt = activation.Add(time.Second)
	created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, oldSample)
	require.NoError(t, err)
	require.Zero(t, created)
	var empty bool
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_reset_source_reset_at IS NULL FROM groups WHERE id=$1`, f.groupID).Scan(&empty))
	require.True(t, empty, "a pre-activation in-flight response cannot establish the baseline")
	created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, activation.Add(time.Second), 80, false))
	require.NoError(t, err)
	require.Zero(t, created)
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_reset_source_reset_at IS NULL FROM groups WHERE id=$1`, f.groupID).Scan(&empty))
	require.True(t, empty, "traffic without a request-start timestamp cannot initialize a new activation")
	for i := 3; i <= 4; i++ {
		created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, f.now.Add(time.Duration(i)*time.Minute), 7, true))
		require.NoError(t, err)
		require.Zero(t, created, "the first confirmed post-activation window only establishes the baseline")
	}
	created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, weeklyObservation(f.baseline, f.now.Add(5*time.Minute), 0, true))
	require.NoError(t, err)
	require.Zero(t, created)
	confirmed := weeklyObservation(f.baseline, f.now.Add(6*time.Minute), 0, true)
	confirmed.ReceivedAt = confirmed.ObservedAt.Add(10 * time.Second)
	created, err = f.repo.ObserveWeeklyReset(ctx, f.accountID, confirmed)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	var effective time.Time
	require.NoError(t, integrationDB.QueryRow(`SELECT effective_at FROM group_quota_follow_reset_events WHERE group_id=$1`, f.groupID).Scan(&effective))
	require.Equal(t, confirmed.ReceivedAt, effective, "subscription reset starts at confirmation, not query start")
}
