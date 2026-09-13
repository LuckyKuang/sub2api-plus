package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
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

func (r *openAIGroupQuotaFollowResetRepository) ObserveWeeklyReset(ctx context.Context, accountID int64, observation service.OpenAIWeeklyQuotaObservation) (_ int, err error) {
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

	var previous openAIWeeklyObservationState
	err = tx.QueryRowContext(ctx, `
		SELECT reset_at, observed_at, used_percent, reset_sequence, pending_reset_at, pending_observed_at
		FROM openai_oauth_weekly_reset_observations
		WHERE account_id = $1 FOR UPDATE
	`, accountID).Scan(&previous.resetAt, &previous.observedAt, &previous.usedPercent, &previous.sequence, &previous.pendingResetAt, &previous.pendingObservedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next, accepted, reset := advanceOpenAIWeeklyObservation(previous, observation)
	if !accepted {
		return 0, nil
	}
	resetAt, observedAt := next.resetAt.Time, observation.ObservedAt.UTC()
	effectiveAt := observation.ReceivedAt.UTC()
	if effectiveAt.IsZero() {
		effectiveAt = observedAt
	}
	if next != previous {
		_, err = tx.ExecContext(ctx, `
		INSERT INTO openai_oauth_weekly_reset_observations
		    (account_id, reset_at, observed_at, used_percent, reset_sequence, pending_reset_at, pending_observed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (account_id) DO UPDATE
		SET reset_at = EXCLUDED.reset_at, observed_at = EXCLUDED.observed_at,
		    used_percent = EXCLUDED.used_percent, reset_sequence = EXCLUDED.reset_sequence,
		    pending_reset_at = EXCLUDED.pending_reset_at, pending_observed_at = EXCLUDED.pending_observed_at,
		    updated_at = NOW()
	`, accountID, resetAt, next.observedAt, next.usedPercent, next.sequence, next.pendingResetAt, next.pendingObservedAt)
		if err != nil {
			return 0, err
		}
	}
	if next.pendingResetAt.Valid {
		// Do not initialize a new/re-enabled group from the old accepted
		// deadline while the current official window is awaiting confirmation.
		return 0, tx.Commit()
	}

	// An account observation may predate a group's membership or configuration.
	// A newly confirmed same-deadline reset must include already synchronized
	// groups. Repeated observations only reconcile missing/different baselines.
	rows, err := tx.QueryContext(ctx, `
		SELECT g.id, g.quota_reset_source_reset_at, g.quota_reset_config_version, g.quota_reset_include_monthly, g.updated_at
		FROM groups g
		WHERE g.quota_reset_source_account_id = $1
		  AND g.platform = $2
		  AND g.subscription_type = $3
		  AND g.deleted_at IS NULL
		  AND ($5 OR g.quota_reset_source_reset_at IS DISTINCT FROM $4)
		  AND EXISTS (
		      SELECT 1 FROM account_groups ag
		      WHERE ag.group_id = g.id AND ag.account_id = $1
		  )
		ORDER BY g.id FOR UPDATE OF g
	`, accountID, service.PlatformOpenAI, service.SubscriptionTypeSubscription, resetAt, reset)
	if err != nil {
		return 0, err
	}
	type target struct {
		id             int64
		baseline       sql.NullTime
		version        int64
		includeMonthly bool
		updatedAt      time.Time
	}
	var targets []target
	for rows.Next() {
		var target target
		if err := rows.Scan(&target.id, &target.baseline, &target.version, &target.includeMonthly, &target.updatedAt); err != nil {
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
			if !observation.QuotaQuery || !observedAt.After(target.updatedAt) {
				// Traffic headers do not carry their request start time. Only a
				// quota query started after activation can establish its baseline.
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE groups SET quota_reset_source_reset_at = $2, updated_at = NOW() WHERE id = $1`, target.id, resetAt); err != nil {
				return 0, err
			}
			continue
		}
		if !reset && !openAIWeeklyResetAdvanced(target.baseline.Time, resetAt, observedAt) {
			continue
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO group_quota_follow_reset_events
				(group_id, source_account_id, config_version, upstream_reset_at, effective_at, include_monthly, reset_sequence)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (group_id, config_version, upstream_reset_at, reset_sequence) DO NOTHING
		`, target.id, accountID, target.version, resetAt, effectiveAt, target.includeMonthly, next.sequence)
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

const openAIWeeklyResetClockTolerance = 5 * time.Minute

func openAIWeeklyResetAdvanced(previous, next, observedAt time.Time) bool {
	if previous.IsZero() || next.IsZero() || observedAt.IsZero() {
		return false
	}
	return !observedAt.Before(previous) && next.Sub(previous) > openAIWeeklyResetClockTolerance
}

type openAIWeeklyObservationState struct {
	resetAt           sql.NullTime
	observedAt        time.Time
	usedPercent       sql.NullFloat64
	sequence          int64
	pendingResetAt    sql.NullTime
	pendingObservedAt sql.NullTime
}

func advanceOpenAIWeeklyObservation(previous openAIWeeklyObservationState, observation service.OpenAIWeeklyQuotaObservation) (openAIWeeklyObservationState, bool, bool) {
	next := previous
	resetAt, observedAt := observation.ResetAt.UTC(), observation.ObservedAt.UTC()
	if resetAt.IsZero() || observedAt.IsZero() || !resetAt.After(observedAt) || resetAt.Sub(observedAt) > 7*24*time.Hour+openAIWeeklyResetClockTolerance {
		return previous, false, false
	}
	used := sql.NullFloat64{}
	if value := observation.UsedPercent; value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 100 {
		used = sql.NullFloat64{Float64: *value, Valid: true}
	}
	if !previous.resetAt.Valid {
		next.resetAt = sql.NullTime{Time: resetAt, Valid: true}
		next.observedAt, next.usedPercent = observedAt, used
		return next, true, false
	}
	if !observedAt.After(previous.observedAt) {
		return previous, false, false
	}
	delta := resetAt.Sub(previous.resetAt.Time)
	changedDeadline := delta > openAIWeeklyResetClockTolerance || delta < -openAIWeeklyResetClockTolerance
	droppedUsage := used.Valid && previous.usedPercent.Valid && previous.usedPercent.Float64-used.Float64 >= 1
	reset := openAIWeeklyResetAdvanced(previous.resetAt.Time, resetAt, observedAt)
	if !reset && (changedDeadline || droppedUsage) {
		// A separate, later GET /wham/usage must corroborate off-schedule
		// evidence. A repeated cached snapshot cannot confirm itself.
		pendingDelta := resetAt.Sub(previous.pendingResetAt.Time)
		reset = observation.QuotaQuery && previous.pendingResetAt.Valid && previous.pendingObservedAt.Valid &&
			observedAt.Sub(previous.pendingObservedAt.Time) >= time.Second &&
			observedAt.Sub(previous.pendingObservedAt.Time) <= 10*time.Minute &&
			pendingDelta <= openAIWeeklyResetClockTolerance && pendingDelta >= -openAIWeeklyResetClockTolerance
		if !reset {
			if previous.pendingResetAt.Valid && previous.pendingObservedAt.Valid &&
				pendingDelta <= openAIWeeklyResetClockTolerance && pendingDelta >= -openAIWeeklyResetClockTolerance &&
				observedAt.Sub(previous.pendingObservedAt.Time) <= 10*time.Minute {
				return previous, true, false
			}
			next.pendingResetAt = sql.NullTime{Time: resetAt, Valid: true}
			next.pendingObservedAt = sql.NullTime{Time: observedAt, Valid: true}
			next.observedAt = observedAt
			return next, true, false
		}
	}
	if !reset && !observation.QuotaQuery && previous.pendingResetAt.Valid {
		return previous, true, false // Only a fresh query can dismiss pending evidence.
	}
	if reset {
		if changedDeadline {
			next.resetAt = sql.NullTime{Time: resetAt, Valid: true}
		}
		next.sequence++
		next.usedPercent = used
	} else if observation.QuotaQuery || !previous.usedPercent.Valid {
		// Late traffic responses must not restore a pre-reset high watermark.
		if used.Valid {
			next.usedPercent = used
		}
	}
	if reset || observation.QuotaQuery {
		next.observedAt = observedAt
	}
	next.pendingResetAt, next.pendingObservedAt = sql.NullTime{}, sql.NullTime{}
	return next, true, reset
}

// ClaimWeeklyResetSources uses a database lease so replicas do not all poll the
// same source. It never selects unbound, API-key, shadow or deleted accounts.
func (r *openAIGroupQuotaFollowResetRepository) ClaimWeeklyResetSources(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		INSERT INTO openai_oauth_weekly_reset_observations (account_id, observed_at, next_poll_at)
		SELECT a.id, NOW(), NOW() + INTERVAL '2 minutes'
		FROM accounts a
		LEFT JOIN openai_oauth_weekly_reset_observations o ON o.account_id = a.id
		WHERE a.platform = 'openai' AND a.type = 'oauth' AND a.parent_account_id IS NULL AND a.deleted_at IS NULL
		  AND (o.next_poll_at IS NULL OR o.next_poll_at <= NOW())
		  AND EXISTS (
		      SELECT 1 FROM groups g JOIN account_groups ag ON ag.group_id = g.id AND ag.account_id = a.id
		      WHERE g.quota_reset_source_account_id = a.id AND g.platform = 'openai'
		        AND g.subscription_type = 'subscription' AND g.deleted_at IS NULL
		  )
		ORDER BY COALESCE(o.next_poll_at, '-infinity'::timestamptz), a.id
		LIMIT $1
		ON CONFLICT (account_id) DO UPDATE SET next_poll_at = EXCLUDED.next_poll_at
		WHERE openai_oauth_weekly_reset_observations.next_poll_at <= NOW()
		RETURNING account_id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
