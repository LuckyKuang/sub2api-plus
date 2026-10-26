package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.GrokRequestOutcomeRepository = (*accountRepository)(nil)

// ApplyGrokRequestOutcomeIfUnchanged publishes a request result only while its
// credential owner, route and previous scheduling decision remain current.
// Extra, scheduling state and invalidation commit as one PostgreSQL statement.
func (r *accountRepository) ApplyGrokRequestOutcomeIfUnchanged(ctx context.Context, expected *service.Account, outcome service.GrokRequestOutcome) (service.GrokRequestOutcomeResult, error) {
	if r == nil || r.sql == nil {
		return service.GrokRequestOutcomeResult{}, errors.New("account repository SQL executor is not configured")
	}
	if expected == nil || expected.ID <= 0 || expected.Platform != service.PlatformGrok ||
		(expected.Type != service.AccountTypeAPIKey && expected.Type != service.AccountTypeOAuth) {
		return service.GrokRequestOutcomeResult{}, errors.New("grok request outcome credential owner is invalid")
	}
	if outcome.TempUnschedulableUntil != nil && strings.TrimSpace(outcome.TempUnschedulableReason) == "" {
		return service.GrokRequestOutcomeResult{}, errors.New("grok request outcome cooldown reason is empty")
	}
	if outcome.ClearRateLimit && outcome.RateLimitResetAt != nil {
		return service.GrokRequestOutcomeResult{}, errors.New("grok request outcome cannot clear and set a rate limit together")
	}
	if _, exists := outcome.Extra["model_rate_limits"]; exists {
		return service.GrokRequestOutcomeResult{}, errors.New("grok request outcome model limits require their dedicated field")
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	payload, err := json.Marshal(withoutRuntimeRecoveryGeneration(outcome.Extra))
	if err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	modelLimits, err := json.Marshal(normalizeJSONMap(outcome.ModelRateLimits))
	if err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	args := []any{
		string(payload), outcome.TempUnschedulableUntil, outcome.TempUnschedulableReason, outcome.RateLimitResetAt,
		expected.ID, expected.Status, expected.Platform, expected.Type, string(credentials), expected.ProxyID,
		expected.Schedulable, expected.TempUnschedulableUntil, expected.TempUnschedulableReason,
		expected.RateLimitedAt, expected.RateLimitResetAt, expected.OverloadUntil, expected.ExpiresAt, expected.AutoPauseOnExpired,
		outcome.ClearRateLimit, string(modelLimits),
	}
	proxyPredicate, args, err := cnProxyOwnerPredicate(expected, args)
	if err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	query := `
		WITH updated AS (
		/* grok_request_outcome_cas */
		UPDATE accounts AS a
		SET extra = CASE WHEN $20::jsonb = '{}'::jsonb THEN COALESCE(a.extra, '{}'::jsonb) || $1::jsonb ELSE
			jsonb_set(COALESCE(a.extra, '{}'::jsonb) || $1::jsonb, '{model_rate_limits}',
				(CASE WHEN jsonb_typeof(a.extra->'model_rate_limits') = 'object' THEN a.extra->'model_rate_limits' ELSE '{}'::jsonb END) || $20::jsonb, TRUE) END,
			temp_unschedulable_until = CASE WHEN $2::timestamptz IS NULL THEN a.temp_unschedulable_until ELSE GREATEST(a.temp_unschedulable_until, $2) END,
			temp_unschedulable_reason = CASE WHEN $2::timestamptz IS NULL THEN a.temp_unschedulable_reason ELSE $3 END,
			rate_limited_at = CASE WHEN $19::boolean THEN NULL WHEN $4::timestamptz IS NOT NULL AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at < $4) THEN NOW() ELSE a.rate_limited_at END,
			rate_limit_reset_at = CASE WHEN $19::boolean THEN NULL WHEN $4::timestamptz IS NULL THEN a.rate_limit_reset_at ELSE GREATEST(a.rate_limit_reset_at, $4) END,
			updated_at = NOW()
		WHERE a.id = $5
			AND a.deleted_at IS NULL
			AND a.status = $6
			AND a.platform = $7
			AND a.type = $8
			AND a.credentials = $9::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $10
			AND a.schedulable = $11
			AND a.temp_unschedulable_until IS NOT DISTINCT FROM $12
			AND COALESCE(a.temp_unschedulable_reason, '') = $13
			AND a.rate_limited_at IS NOT DISTINCT FROM $14
			AND a.rate_limit_reset_at IS NOT DISTINCT FROM $15
			AND a.overload_until IS NOT DISTINCT FROM $16
			AND a.expires_at IS NOT DISTINCT FROM $17
			AND a.auto_pause_on_expired = $18
		` + proxyPredicate + ` RETURNING a.id, COALESCE(a.extra->>'runtime_recovery_generation','') AS recovery_generation
		), published AS (
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT ` + fmt.Sprintf("$%d", len(args)+1) + `, updated.id, NULL, NULL FROM updated
		RETURNING account_id
		)
		SELECT updated.recovery_generation FROM updated JOIN published ON published.account_id=updated.id
	`
	args = append(args, service.SchedulerOutboxEventAccountChanged)
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return service.GrokRequestOutcomeResult{}, rows.Err()
	}
	var generation string
	if err := rows.Scan(&generation); err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	if err := rows.Close(); err != nil {
		return service.GrokRequestOutcomeResult{}, err
	}
	if contextTx != nil || r.transactionAccountIDs != nil {
		r.markTransactionAccountChanged(expected.ID)
	} else {
		r.deleteSchedulerAccountSnapshotDetached(ctx, expected.ID)
	}
	return service.GrokRequestOutcomeResult{Applied: true, RecoveryGeneration: generation}, nil
}
