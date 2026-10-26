package repository

import (
	"context"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.AtomicSchedulingRecoveryRepository = (*accountRepository)(nil)

func (r *accountRepository) ClearAccountSchedulingRecoveryState(ctx context.Context, accountID int64, mode service.SchedulingRecoveryMode) error {
	if r == nil || r.sql == nil {
		return errors.New("account scheduling recovery repository SQL executor is not configured")
	}
	if mode != service.SchedulingRecoveryRates && mode != service.SchedulingRecoveryTemporary {
		return errors.New("account scheduling recovery mode is invalid")
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	result, err := executor.ExecContext(ctx, `
		WITH updated AS (
		/* scheduling_recovery_atomic */
		UPDATE accounts SET
			rate_limited_at=CASE WHEN $2::boolean THEN NULL ELSE rate_limited_at END,
			rate_limit_reset_at=CASE WHEN $2::boolean THEN NULL ELSE rate_limit_reset_at END,
			overload_until=CASE WHEN $2::boolean THEN NULL ELSE overload_until END,
			temp_unschedulable_until=NULL,temp_unschedulable_reason=NULL,
			extra=(CASE WHEN $2::boolean THEN COALESCE(extra,'{}'::jsonb)-'model_rate_limits'-'antigravity_quota_scopes' ELSE COALESCE(extra,'{}'::jsonb)-'model_rate_limits' END) ||
				CASE WHEN platform='grok' OR (platform='openai' AND type='apikey') THEN `+newRuntimeRecoveryGenerationSQL+` ELSE '{}'::jsonb END,
			updated_at=NOW()
		WHERE id=$1 AND deleted_at IS NULL RETURNING id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $3,updated.id,NULL,NULL FROM updated
	`, accountID, mode == service.SchedulingRecoveryRates, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if contextTx != nil || r.transactionAccountIDs != nil {
		r.markTransactionAccountChanged(accountID)
	} else {
		r.deleteSchedulerAccountSnapshotDetached(ctx, accountID)
	}
	return nil
}
