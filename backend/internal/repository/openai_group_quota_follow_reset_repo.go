package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type openAIGroupQuotaFollowResetRepository struct {
	db *sql.DB
}

func NewOpenAIGroupQuotaFollowResetRepository(_ *dbent.Client, db *sql.DB) service.OpenAIGroupQuotaFollowResetRepository {
	return &openAIGroupQuotaFollowResetRepository{db: db}
}

func (r *openAIGroupQuotaFollowResetRepository) ObserveWeeklyReset(ctx context.Context, accountID int64, resetAt, observedAt time.Time) (_ int, err error) {
	if r == nil || r.db == nil {
		return 0, errors.New("openai group quota follow reset repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var eligible bool
	err = tx.QueryRowContext(ctx, `
		SELECT platform = $2 AND type = $3 AND parent_account_id IS NULL
		FROM accounts
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, accountID, service.PlatformOpenAI, service.AccountTypeOAuth).Scan(&eligible)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !eligible {
		return 0, nil
	}

	var previous time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT reset_at FROM openai_oauth_weekly_reset_observations
		WHERE account_id = $1 FOR UPDATE
	`, accountID).Scan(&previous)
	firstObservation := errors.Is(err, sql.ErrNoRows)
	if err != nil && !firstObservation {
		return 0, err
	}
	repeatedObservation := !firstObservation && resetAt.Equal(previous)
	if !firstObservation && !repeatedObservation && !openAIWeeklyResetAdvanced(previous, resetAt, observedAt) {
		return 0, nil
	}
	if !repeatedObservation {
		_, err = tx.ExecContext(ctx, `
		INSERT INTO openai_oauth_weekly_reset_observations (account_id, reset_at, observed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (account_id) DO UPDATE
		SET reset_at = EXCLUDED.reset_at, observed_at = EXCLUDED.observed_at, updated_at = NOW()
	`, accountID, resetAt, observedAt)
		if err != nil {
			return 0, err
		}
	}

	// An account observation may predate a group's membership or configuration.
	// Reconcile groups even for a repeated accepted timestamp, without rewriting
	// the account observation or relocking already synchronized group rows.
	rows, err := tx.QueryContext(ctx, `
		SELECT g.id, g.quota_reset_source_reset_at, g.quota_reset_config_version, g.quota_reset_include_monthly
		FROM groups g
		WHERE g.quota_reset_source_account_id = $1
		  AND g.platform = $2
		  AND g.subscription_type = $3
		  AND g.deleted_at IS NULL
		  AND g.quota_reset_source_reset_at IS DISTINCT FROM $4
		  AND EXISTS (
		      SELECT 1 FROM account_groups ag
		      WHERE ag.group_id = g.id AND ag.account_id = $1
		  )
		ORDER BY g.id FOR UPDATE OF g
	`, accountID, service.PlatformOpenAI, service.SubscriptionTypeSubscription, resetAt)
	if err != nil {
		return 0, err
	}
	type target struct {
		id             int64
		baseline       sql.NullTime
		version        int64
		includeMonthly bool
	}
	var targets []target
	for rows.Next() {
		var target target
		if err := rows.Scan(&target.id, &target.baseline, &target.version, &target.includeMonthly); err != nil {
			_ = rows.Close()
			return 0, err
		}
		targets = append(targets, target)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	created := 0
	for _, target := range targets {
		// Refresh membership after acquiring the group lock: the locking query
		// may have started before a concurrent membership removal committed.
		var bound bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM account_groups WHERE group_id = $1 AND account_id = $2)
		`, target.id, accountID).Scan(&bound); err != nil {
			return 0, err
		}
		if !bound {
			continue
		}
		if !target.baseline.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE groups SET quota_reset_source_reset_at = $2, updated_at = NOW() WHERE id = $1`, target.id, resetAt); err != nil {
				return 0, err
			}
			continue
		}
		if !openAIWeeklyResetAdvanced(target.baseline.Time, resetAt, observedAt) {
			continue
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO group_quota_follow_reset_events
				(group_id, source_account_id, config_version, upstream_reset_at, effective_at, include_monthly)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (group_id, config_version, upstream_reset_at) DO NOTHING
		`, target.id, accountID, target.version, resetAt, observedAt, target.includeMonthly)
		if err != nil {
			return 0, err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
			created += int(affected)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE groups SET quota_reset_source_reset_at = $2, updated_at = NOW() WHERE id = $1`, target.id, resetAt); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return created, nil
}

const openAIWeeklyResetMinAdvance = (7 * 24 * time.Hour) / 2

func openAIWeeklyResetAdvanced(previous, next, observedAt time.Time) bool {
	if previous.IsZero() || next.IsZero() || observedAt.IsZero() {
		return false
	}
	// Require the previously announced window to have ended, and the new
	// next-reset to look like another weekly window rather than clock skew.
	return !observedAt.Before(previous) && next.Sub(previous) >= openAIWeeklyResetMinAdvance
}

func (r *openAIGroupQuotaFollowResetRepository) ProcessNextPending(ctx context.Context) (_ *service.GroupQuotaFollowResetResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("openai group quota follow reset repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var eventID, groupID, sourceID, configVersion int64
	var effectiveAt time.Time
	var includeMonthly bool
	err = tx.QueryRowContext(ctx, `
		SELECT e.id, e.group_id, e.source_account_id, e.config_version, e.effective_at, e.include_monthly
		FROM group_quota_follow_reset_events e
		WHERE e.status = 'pending'
		  AND NOT EXISTS (
		      SELECT 1 FROM group_quota_follow_reset_events older
		      WHERE older.group_id = e.group_id AND older.status = 'pending' AND older.id < e.id
		  )
		ORDER BY e.id
		FOR UPDATE OF e SKIP LOCKED
		LIMIT 1
	`).Scan(&eventID, &groupID, &sourceID, &configVersion, &effectiveAt, &includeMonthly)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var groupMonthlyLimit sql.NullFloat64
	var valid bool
	// Membership edits hold the group lock. Acquire it in a separate statement
	// so validation uses a fresh READ COMMITTED snapshot after any lock wait;
	// a join/EXISTS in the locking statement can still see pre-edit membership.
	var lockedGroupID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM groups WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
	`, groupID).Scan(&lockedGroupID)
	if err == nil {
		err = tx.QueryRowContext(ctx, `
		SELECT g.platform = $4
		   AND g.subscription_type = $5
		   AND g.quota_reset_source_account_id IS NOT DISTINCT FROM $2
		   AND g.quota_reset_config_version = $3
		   AND a.platform = $4
		   AND a.type = $6
		   AND a.parent_account_id IS NULL
		   AND EXISTS (
		       SELECT 1 FROM account_groups ag
		       WHERE ag.group_id = g.id AND ag.account_id = a.id
		   ),
		   g.monthly_limit_usd
		FROM groups g
		JOIN accounts a ON a.id = $2 AND a.deleted_at IS NULL
		WHERE g.id = $1 AND g.deleted_at IS NULL
	`, groupID, sourceID, configVersion, service.PlatformOpenAI, service.SubscriptionTypeSubscription, service.AccountTypeOAuth).Scan(&valid, &groupMonthlyLimit)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, sql.ErrNoRows) || !valid {
		_, updateErr := tx.ExecContext(ctx, `UPDATE group_quota_follow_reset_events SET status='skipped', processed_at=NOW(), updated_at=NOW(), last_error='source configuration is no longer valid' WHERE id=$1`, eventID)
		if updateErr != nil {
			return nil, updateErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.GroupQuotaFollowResetResult{EventID: eventID, GroupID: groupID, Skipped: true}, nil
	}
	resetMonthly := includeMonthly && groupMonthlyLimit.Valid && groupMonthlyLimit.Float64 > 0
	rows, err := tx.QueryContext(ctx, `
		UPDATE user_subscriptions
		SET daily_usage_usd = 0,
		    weekly_usage_usd = 0,
		    five_hour_usage_usd = 0,
		    monthly_usage_usd = CASE WHEN $3 THEN 0 ELSE monthly_usage_usd END,
		    daily_window_start = $2,
		    weekly_window_start = $2,
		    five_hour_window_start = $2,
		    monthly_window_start = CASE WHEN $3 THEN $2 ELSE monthly_window_start END,
		    quota_follow_reset_event_id = $1,
		    updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
		WHERE group_id = $4
		  AND quota_follow_reset_event_id < $1
		  AND deleted_at IS NULL
		  AND status = $5
		  AND starts_at <= $2
		  AND expires_at > $2
		RETURNING user_id
	`, eventID, effectiveAt, resetMonthly, groupID, service.SubscriptionStatusActive)
	if err != nil {
		return nil, err
	}
	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE group_quota_follow_reset_events
		SET status='completed', processed_at=NOW(), affected_subscriptions=$2, last_error='', updated_at=NOW()
		WHERE id=$1
	`, eventID, len(userIDs)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.GroupQuotaFollowResetResult{EventID: eventID, GroupID: groupID, UserIDs: userIDs, AffectedSubscriptions: len(userIDs)}, nil
}
