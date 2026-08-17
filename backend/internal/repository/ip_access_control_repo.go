package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type ipAccessControlRepository struct {
	db *sql.DB
}

func NewIPAccessControlRepository(db *sql.DB) service.IPAccessControlRepository {
	return &ipAccessControlRepository{db: db}
}

const ipAccessRuleColumns = `
id, ip_or_cidr, rule_kind, status, reason, failure_count,
first_failed_at, last_failed_at, blocked_at, expires_at, last_seen_at, hit_count,
created_by_user_id, released_by_user_id, released_at, created_at, updated_at`

func scanIPAccessRule(scan func(dest ...any) error) (*service.IPAccessRule, error) {
	rule := &service.IPAccessRule{}
	var kind, status string
	var firstFailedAt, lastFailedAt, blockedAt, expiresAt, lastSeenAt, releasedAt sql.NullTime
	var createdBy, releasedBy sql.NullInt64
	if err := scan(
		&rule.ID, &rule.IPOrCIDR, &kind, &status, &rule.Reason, &rule.FailureCount,
		&firstFailedAt, &lastFailedAt, &blockedAt, &expiresAt, &lastSeenAt, &rule.HitCount,
		&createdBy, &releasedBy, &releasedAt, &rule.CreatedAt, &rule.UpdatedAt,
	); err != nil {
		return nil, err
	}
	rule.RuleKind = service.IPAccessRuleKind(kind)
	rule.Status = service.IPAccessRuleStatus(status)
	rule.FirstFailedAt = nullableTime(firstFailedAt)
	rule.LastFailedAt = nullableTime(lastFailedAt)
	rule.BlockedAt = nullableTime(blockedAt)
	rule.ExpiresAt = nullableTime(expiresAt)
	rule.LastSeenAt = nullableTime(lastSeenAt)
	rule.ReleasedAt = nullableTime(releasedAt)
	if createdBy.Valid {
		value := createdBy.Int64
		rule.CreatedByUserID = &value
	}
	if releasedBy.Valid {
		value := releasedBy.Int64
		rule.ReleasedByUserID = &value
	}
	return rule, nil
}

type ipAccessRuleQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// lockExactIPRuleMutations serializes threshold auto-block and administrator
// exact-IP mutations without turning the rule table into a global bottleneck.
// Hash collisions only cause harmless extra serialization.
func lockExactIPRuleMutations(ctx context.Context, tx *sql.Tx, normalizedIP string) error {
	if tx == nil {
		return fmt.Errorf("nil IP access rule transaction")
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, normalizedIP)
	return err
}

func findEffectiveBlockRule(ctx context.Context, queryer ipAccessRuleQueryer, normalizedIP string) (*service.IPAccessRule, error) {
	return scanIPAccessRule(queryer.QueryRowContext(ctx, `SELECT `+ipAccessRuleColumns+`
FROM ip_access_rules candidate
WHERE candidate.status = 'active'
AND candidate.rule_kind IN ('manual_block', 'auto_block')
AND (candidate.expires_at IS NULL OR candidate.expires_at > NOW())
AND $1::inet <<= candidate.ip_or_cidr::cidr
AND NOT EXISTS (
	SELECT 1 FROM ip_access_rules allow_rule
	WHERE allow_rule.status = 'active'
	AND allow_rule.rule_kind = 'allow'
	AND (allow_rule.expires_at IS NULL OR allow_rule.expires_at > NOW())
	AND $1::inet <<= allow_rule.ip_or_cidr::cidr
)
ORDER BY masklen(candidate.ip_or_cidr::cidr) DESC, candidate.created_at DESC
LIMIT 1`, normalizedIP).Scan)
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func (r *ipAccessControlRepository) ListIPAccessRules(ctx context.Context, filter service.IPAccessRuleFilter) (result *service.IPAccessRuleList, err error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil IP access control repository")
	}
	// Rule status is advanced only on management reads, rather than on the
	// request hot path. Active-rule matching already filters expires_at.
	if _, err := r.db.ExecContext(ctx, `UPDATE ip_access_rules
SET status = 'expired', updated_at = NOW()
WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= NOW()`); err != nil {
		return nil, err
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	clauses := []string{"1=1"}
	args := make([]any, 0, 3)
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		clauses = append(clauses, fmt.Sprintf("(ip_or_cidr ILIKE $%d OR reason ILIKE $%d)", len(args), len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ip_access_rules WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := "SELECT " + ipAccessRuleColumns + " FROM ip_access_rules WHERE " + where +
		fmt.Sprintf(" ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			result = nil
			err = closeErr
		}
	}()
	items := make([]*service.IPAccessRule, 0)
	for rows.Next() {
		rule, scanErr := scanIPAccessRule(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.IPAccessRuleList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *ipAccessControlRepository) ListActiveIPAccessRules(ctx context.Context) (rules []*service.IPAccessRule, err error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil IP access control repository")
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+ipAccessRuleColumns+" FROM ip_access_rules WHERE status = 'active' AND (expires_at IS NULL OR expires_at > NOW())")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			rules = nil
			err = closeErr
		}
	}()
	rules = make([]*service.IPAccessRule, 0)
	for rows.Next() {
		rule, scanErr := scanIPAccessRule(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *ipAccessControlRepository) CreateManualIPAccessRule(ctx context.Context, rule *service.IPAccessRule) (*service.IPAccessRule, error) {
	if r == nil || r.db == nil || rule == nil {
		return nil, fmt.Errorf("invalid IP access rule repository input")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if !strings.Contains(rule.IPOrCIDR, "/") {
		if err := lockExactIPRuleMutations(ctx, tx, rule.IPOrCIDR); err != nil {
			return nil, err
		}
	}
	// Move an expired active row out of the partial unique index first, so
	// re-adding a rule creates a new history record instead of rewriting it.
	if _, err := tx.ExecContext(ctx, `UPDATE ip_access_rules
SET status = 'expired', updated_at = NOW()
WHERE ip_or_cidr = $1 AND rule_kind = $2 AND status = 'active'
AND expires_at IS NOT NULL AND expires_at <= NOW()`, rule.IPOrCIDR, string(rule.RuleKind)); err != nil {
		return nil, err
	}
	var createdBy any
	if rule.CreatedByUserID != nil && *rule.CreatedByUserID > 0 {
		createdBy = *rule.CreatedByUserID
	}
	var blockedAt any
	if rule.RuleKind == service.IPAccessRuleKindManualBlock {
		blockedAt = time.Now().UTC()
	}
	query := `INSERT INTO ip_access_rules (
ip_or_cidr, rule_kind, status, reason, blocked_at, expires_at, created_by_user_id, created_at, updated_at)
VALUES ($1, $2, 'active', $3, $4, $5, $6, NOW(), NOW())
ON CONFLICT (ip_or_cidr, rule_kind) WHERE status = 'active'
DO UPDATE SET reason = EXCLUDED.reason, expires_at = EXCLUDED.expires_at,
created_by_user_id = EXCLUDED.created_by_user_id, blocked_at = EXCLUDED.blocked_at, updated_at = NOW()
RETURNING ` + ipAccessRuleColumns
	created, err := scanIPAccessRule(tx.QueryRowContext(ctx, query, rule.IPOrCIDR, string(rule.RuleKind), rule.Reason, blockedAt, rule.ExpiresAt, createdBy).Scan)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *ipAccessControlRepository) CreateManualIPBlockForFailureState(
	ctx context.Context,
	normalizedIP, reason string,
	expiresAt time.Time,
	actorUserID int64,
) (*service.IPFailureStateBlockRepositoryResult, error) {
	if r == nil || r.db == nil || normalizedIP == "" || actorUserID <= 0 || expiresAt.IsZero() {
		return nil, fmt.Errorf("invalid failure-state manual block repository input")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockExactIPRuleMutations(ctx, tx, normalizedIP); err != nil {
		return nil, err
	}

	var suppressedByAllow bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
	SELECT 1 FROM ip_access_rules allow_rule
	WHERE allow_rule.status = 'active'
	AND allow_rule.rule_kind = 'allow'
	AND (allow_rule.expires_at IS NULL OR allow_rule.expires_at > NOW())
	AND $1::inet <<= allow_rule.ip_or_cidr::cidr
)`, normalizedIP).Scan(&suppressedByAllow); err != nil {
		return nil, err
	}
	if suppressedByAllow {
		return nil, service.ErrIPBlockSuppressedByAllow
	}

	existing, err := findEffectiveBlockRule(ctx, tx, normalizedIP)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.IPFailureStateBlockRepositoryResult{Rule: existing, AlreadyBlocked: true}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Free an expired exact-IP manual row from the active partial unique index.
	if _, err := tx.ExecContext(ctx, `UPDATE ip_access_rules
SET status = 'expired', updated_at = NOW()
WHERE ip_or_cidr = $1 AND rule_kind = 'manual_block' AND status = 'active'
AND expires_at IS NOT NULL AND expires_at <= NOW()`, normalizedIP); err != nil {
		return nil, err
	}
	created, err := scanIPAccessRule(tx.QueryRowContext(ctx, `INSERT INTO ip_access_rules (
ip_or_cidr, rule_kind, status, reason, blocked_at, expires_at, created_by_user_id, created_at, updated_at)
VALUES ($1, 'manual_block', 'active', $2, NOW(), $3, $4, NOW(), NOW())
RETURNING `+ipAccessRuleColumns, normalizedIP, reason, expiresAt, actorUserID).Scan)
	if err != nil {
		return nil, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		confirmCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		confirmed, confirmErr := findEffectiveBlockRule(confirmCtx, r.db, normalizedIP)
		cancel()
		if confirmErr == nil && confirmed != nil {
			return &service.IPFailureStateBlockRepositoryResult{
				Rule:           confirmed,
				AlreadyBlocked: confirmed.ID != created.ID,
			}, nil
		}
		return nil, commitErr
	}
	return &service.IPFailureStateBlockRepositoryResult{Rule: created}, nil
}

func (r *ipAccessControlRepository) ReleaseIPAccessRuleAndReset(ctx context.Context, id, actorUserID int64) (*service.IPAccessRule, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil IP access control repository")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rule, err := scanIPAccessRule(tx.QueryRowContext(ctx, `UPDATE ip_access_rules
SET status = 'released', released_by_user_id = $2, released_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'active'
RETURNING `+ipAccessRuleColumns, id, actorUserID).Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrIPAccessRuleNotFound
		}
		return nil, err
	}
	// Removing an allow rule must not silently grant the source a fresh set of
	// login attempts. Failure state is reset only when a block is released.
	if rule.RuleKind != service.IPAccessRuleKindAllow && strings.Contains(rule.IPOrCIDR, "/") {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ip_login_failure_states
WHERE normalized_ip::inet <<= $1::cidr`, rule.IPOrCIDR); err != nil {
			return nil, err
		}
	} else if rule.RuleKind != service.IPAccessRuleKindAllow {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ip_login_failure_states WHERE normalized_ip = $1`, rule.IPOrCIDR); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rule, nil
}

func (r *ipAccessControlRepository) ListIPLoginFailureStates(
	ctx context.Context,
	filter service.IPLoginFailureStateFilter,
	window time.Duration,
) (result *service.IPLoginFailureStateList, err error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil IP access control repository")
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	windowSeconds := int64(window.Seconds())
	if windowSeconds < 1 {
		return nil, fmt.Errorf("invalid IP login failure window")
	}

	args := []any{windowSeconds}
	clauses := []string{
		"window_started_at > NOW() - make_interval(secs => $1::double precision)",
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		clauses = append(clauses, fmt.Sprintf("normalized_ip ILIKE $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")

	var total int64
	if err := r.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM ip_login_failure_states WHERE "+where,
		args...,
	).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := `SELECT
s.normalized_ip,
s.failure_count,
s.window_started_at,
s.last_failed_at,
s.window_started_at + make_interval(secs => $1::double precision) AS window_expires_at,
(block_rule.id IS NOT NULL) AS active_block_rule,
block_rule.id AS block_rule_id,
block_rule.rule_kind AS block_rule_kind,
block_rule.ip_or_cidr AS block_rule_ip_or_cidr,
block_rule.blocked_at,
block_rule.expires_at AS block_expires_at,
(EXISTS (
	SELECT 1 FROM ip_access_rules allow_rule
	WHERE allow_rule.status = 'active'
	AND allow_rule.rule_kind = 'allow'
	AND (allow_rule.expires_at IS NULL OR allow_rule.expires_at > NOW())
	AND s.normalized_ip::inet <<= allow_rule.ip_or_cidr::cidr
)) AS suppressed_by_allow_rule
FROM ip_login_failure_states s
LEFT JOIN LATERAL (
	SELECT candidate.id, candidate.rule_kind, candidate.ip_or_cidr, candidate.blocked_at, candidate.expires_at
	FROM ip_access_rules candidate
	WHERE candidate.status = 'active'
	AND candidate.rule_kind IN ('manual_block', 'auto_block')
	AND (candidate.expires_at IS NULL OR candidate.expires_at > NOW())
	AND s.normalized_ip::inet <<= candidate.ip_or_cidr::cidr
	ORDER BY masklen(candidate.ip_or_cidr::cidr) DESC, candidate.created_at DESC, candidate.id DESC
	LIMIT 1
) block_rule ON TRUE
WHERE ` + where + fmt.Sprintf(
		" ORDER BY s.last_failed_at DESC LIMIT $%d OFFSET $%d",
		len(args)-1,
		len(args),
	)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			result = nil
			err = closeErr
		}
	}()

	items := make([]*service.IPLoginFailureState, 0)
	for rows.Next() {
		state := &service.IPLoginFailureState{}
		var blockRuleID sql.NullInt64
		var blockRuleKind sql.NullString
		var blockRuleIPOrCIDR sql.NullString
		var blockedAt, blockExpiresAt sql.NullTime
		if err := rows.Scan(
			&state.NormalizedIP,
			&state.FailureCount,
			&state.WindowStartedAt,
			&state.LastFailedAt,
			&state.WindowExpiresAt,
			&state.ActiveBlockRule,
			&blockRuleID,
			&blockRuleKind,
			&blockRuleIPOrCIDR,
			&blockedAt,
			&blockExpiresAt,
			&state.SuppressedByAllowRule,
		); err != nil {
			return nil, err
		}
		state.WindowStartedAt = state.WindowStartedAt.UTC()
		state.LastFailedAt = state.LastFailedAt.UTC()
		state.WindowExpiresAt = state.WindowExpiresAt.UTC()
		if blockRuleID.Valid {
			value := blockRuleID.Int64
			state.BlockRuleID = &value
		}
		if blockRuleKind.Valid {
			value := service.IPAccessRuleKind(blockRuleKind.String)
			state.BlockRuleKind = &value
		}
		if blockRuleIPOrCIDR.Valid {
			value := blockRuleIPOrCIDR.String
			state.BlockRuleIPOrCIDR = &value
		}
		state.BlockedAt = nullableTime(blockedAt)
		state.BlockExpiresAt = nullableTime(blockExpiresAt)
		items = append(items, state)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.IPLoginFailureStateList{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	}, nil
}

func (r *ipAccessControlRepository) ResetIPLoginFailureState(ctx context.Context, normalizedIP string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil IP access control repository")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM ip_login_failure_states WHERE normalized_ip = $1`, normalizedIP)
	return err
}

// CleanupExpiredIPLoginFailureStates removes only stale rolling counters. It
// deliberately leaves block-rule history intact for operator auditability.
func (r *ipAccessControlRepository) CleanupExpiredIPLoginFailureStates(ctx context.Context, before time.Time, limit int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil IP access control repository")
	}
	if limit < 1 || limit > 10000 {
		limit = 1000
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM ip_login_failure_states
WHERE normalized_ip IN (
    SELECT normalized_ip
    FROM ip_login_failure_states
    WHERE last_failed_at <= $1
    ORDER BY last_failed_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)`, before.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RecordIPAccessRuleHit updates observability fields for every active block
// rule that matches the denied source. Callers invoke it only after allow-rule
// precedence has already been evaluated.
func (r *ipAccessControlRepository) RecordIPAccessRuleHit(ctx context.Context, normalizedIP string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil IP access control repository")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE ip_access_rules
SET last_seen_at = NOW(), hit_count = hit_count + 1
WHERE status = 'active'
AND rule_kind IN ('manual_block', 'auto_block')
AND (expires_at IS NULL OR expires_at > NOW())
AND $1::inet <<= ip_or_cidr::cidr`, normalizedIP)
	return err
}

func (r *ipAccessControlRepository) RecordFailedLogin(ctx context.Context, normalizedIP string, threshold int, window, blockFor time.Duration) (*service.LoginFailureRecordResult, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil IP access control repository")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockExactIPRuleMutations(ctx, tx, normalizedIP); err != nil {
		return nil, err
	}
	// Expired bans are first released from the active unique index. The failure
	// counter's rolling window remains independent and naturally restarts.
	if _, err := tx.ExecContext(ctx, `UPDATE ip_access_rules
SET status = 'expired', updated_at = NOW()
WHERE ip_or_cidr = $1 AND rule_kind = 'auto_block' AND status = 'active'
AND expires_at IS NOT NULL AND expires_at <= NOW()`, normalizedIP); err != nil {
		return nil, err
	}
	windowSeconds := int64(window.Seconds())
	var (
		count           int
		windowStartedAt time.Time
	)
	query := `INSERT INTO ip_login_failure_states (
normalized_ip, failure_count, window_started_at, last_failed_at, created_at, updated_at)
VALUES ($1, 1, NOW(), NOW(), NOW(), NOW())
ON CONFLICT (normalized_ip) DO UPDATE SET
failure_count = CASE WHEN ip_login_failure_states.window_started_at <= NOW() - make_interval(secs => $2::double precision)
THEN 1 ELSE ip_login_failure_states.failure_count + 1 END,
window_started_at = CASE WHEN ip_login_failure_states.window_started_at <= NOW() - make_interval(secs => $2::double precision)
THEN NOW() ELSE ip_login_failure_states.window_started_at END,
last_failed_at = NOW(), updated_at = NOW()
RETURNING failure_count, window_started_at`
	if err := tx.QueryRowContext(ctx, query, normalizedIP, windowSeconds).Scan(&count, &windowStartedAt); err != nil {
		return nil, err
	}
	result := &service.LoginFailureRecordResult{FailureCount: count}
	if count >= threshold {
		rule, err := scanIPAccessRule(tx.QueryRowContext(ctx, `INSERT INTO ip_access_rules (
ip_or_cidr, rule_kind, status, reason, failure_count, first_failed_at, last_failed_at, blocked_at, expires_at, created_at, updated_at)
SELECT $1, 'auto_block', 'active', 'automatic login failure threshold reached', $2, $4, NOW(), NOW(),
NOW() + make_interval(secs => $3::double precision), NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM ip_access_rules allow_rule
    WHERE allow_rule.status = 'active'
    AND allow_rule.rule_kind = 'allow'
    AND (allow_rule.expires_at IS NULL OR allow_rule.expires_at > NOW())
    AND $1::inet <<= allow_rule.ip_or_cidr::cidr
)
AND NOT EXISTS (
    SELECT 1 FROM ip_access_rules block_rule
    WHERE block_rule.status = 'active'
    AND block_rule.rule_kind IN ('manual_block', 'auto_block')
    AND (block_rule.expires_at IS NULL OR block_rule.expires_at > NOW())
    AND $1::inet <<= block_rule.ip_or_cidr::cidr
)
ON CONFLICT (ip_or_cidr, rule_kind) WHERE status = 'active'
DO UPDATE SET failure_count = EXCLUDED.failure_count,
first_failed_at = EXCLUDED.first_failed_at,
last_failed_at = EXCLUDED.last_failed_at,
blocked_at = EXCLUDED.blocked_at,
expires_at = EXCLUDED.expires_at,
updated_at = NOW()
RETURNING `+ipAccessRuleColumns, normalizedIP, count, int64(blockFor.Seconds()), windowStartedAt).Scan)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == sql.ErrNoRows {
			rule, err = findEffectiveBlockRule(ctx, tx, normalizedIP)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		if rule != nil {
			result.Blocked = true
			result.Rule = rule
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		if result.Blocked {
			confirmCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			confirmed, confirmErr := findEffectiveBlockRule(confirmCtx, r.db, normalizedIP)
			cancel()
			if confirmErr == nil && confirmed != nil {
				result.Rule = confirmed
				result.Blocked = true
				return result, nil
			}
		}
		return nil, commitErr
	}
	return result, nil
}
