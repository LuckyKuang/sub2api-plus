package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.CNControlSnapshotRepository = (*accountRepository)(nil)

// UpdateCNControlExtraIfUnchanged attaches a provider observation only to the
// exact credential owner that issued its control request. PostgreSQL rechecks
// the predicate after concurrent row updates; a relink cannot slip between an
// application-side comparison and the Extra merge. Scheduler invalidation is
// committed in the same statement so a failed outbox write rolls back Extra.
func (r *accountRepository) UpdateCNControlExtraIfUnchanged(
	ctx context.Context,
	expected *service.Account,
	updates map[string]any,
) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	if expected == nil || expected.ID <= 0 || expected.Platform == "" || expected.Type == "" {
		return false, errors.New("control snapshot credential owner is invalid")
	}
	if len(updates) == 0 {
		return false, nil
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	query := `/* cn_control_snapshot_cas */
		UPDATE accounts AS a
		SET extra = COALESCE(a.extra, '{}'::jsonb) || $1::jsonb,
			updated_at = NOW()
		WHERE a.id = $2
			AND a.deleted_at IS NULL
			AND a.platform = $3
			AND a.type = $4
			AND a.credentials = $5::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $6
	`
	args := []any{string(payload), expected.ID, expected.Platform, expected.Type, string(credentials), expected.ProxyID}
	proxyPredicate, args, err := cnProxyOwnerPredicate(expected, args)
	if err != nil {
		return false, err
	}
	query += proxyPredicate
	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		query = "WITH updated AS (" + query + ` RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT ` + fmt.Sprintf("$%d", len(args)+1) + `, updated.id, NULL, NULL FROM updated
		`
		args = append(args, service.SchedulerOutboxEventAccountChanged)
	}
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
		r.syncSchedulerAccountSnapshotDetached(ctx, expected.ID)
	}
	return true, nil
}
