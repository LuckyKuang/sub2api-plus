package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.CNBalanceScheduleRepository = (*accountRepository)(nil)

// UpdateCNBalanceScheduleIfUnchanged applies a wallet decision only while its
// exact native login or API key and previous scheduler state own the account row.
// The outbox belongs to the same statement, including within caller-owned Tx.
func (r *accountRepository) UpdateCNBalanceScheduleIfUnchanged(ctx context.Context, expected *service.Account, until *time.Time, reason string) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	validOwner := expected != nil && expected.ID > 0 &&
		((expected.Platform == service.PlatformDeepseek && expected.IsDomesticOAuth()) ||
			(expected.Type == service.AccountTypeAPIKey && (expected.Platform == service.PlatformDeepseek || expected.Platform == service.PlatformKimi) &&
				!expected.IsCodingPlan() && strings.TrimSpace(expected.GetCNAPIKey()) != ""))
	if !validOwner {
		return false, errors.New("wallet credential owner is invalid")
	}
	if (until != nil && !strings.HasPrefix(reason, "cn_balance_low:")) ||
		(until == nil && (reason != "" || !strings.HasPrefix(expected.TempUnschedulableReason, "cn_balance_low:"))) {
		return false, errors.New("wallet scheduling reason is invalid")
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	args := []any{until, reason, expected.ID, expected.Status, expected.Platform, expected.Type, string(credentials), expected.ProxyID,
		expected.TempUnschedulableUntil, expected.TempUnschedulableReason, expected.Schedulable}
	proxyPredicate, args, err := cnProxyOwnerPredicate(expected, args)
	if err != nil {
		return false, err
	}
	query := `
		WITH updated AS (
		/* cn_balance_schedule_cas */
		UPDATE accounts AS a
		SET temp_unschedulable_until = $1,
			temp_unschedulable_reason = NULLIF($2, ''),
			updated_at = NOW()
		WHERE a.id = $3
			AND a.deleted_at IS NULL
			AND a.status = $4
			AND a.platform = $5
			AND a.type = $6
			AND a.credentials = $7::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $8
			AND a.temp_unschedulable_until IS NOT DISTINCT FROM $9
			AND COALESCE(a.temp_unschedulable_reason, '') = $10
			AND a.schedulable = $11
			AND ($1::timestamptz IS NULL OR (
				a.status = 'active' AND a.schedulable IS TRUE
				AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())
				AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
				AND (a.overload_until IS NULL OR a.overload_until <= NOW())
				AND (a.auto_pause_on_expired IS NOT TRUE OR a.expires_at IS NULL OR a.expires_at > NOW())
			))
		` + proxyPredicate + ` RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT ` + fmt.Sprintf("$%d", len(args)+1) + `, updated.id, NULL, NULL FROM updated
	`
	args = append(args, service.SchedulerOutboxEventAccountChanged)
	result, err := executor.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	if contextTx != nil || r.transactionAccountIDs != nil {
		r.markTransactionAccountChanged(expected.ID)
	} else {
		r.deleteSchedulerAccountSnapshotDetached(ctx, expected.ID)
	}
	return true, nil
}
