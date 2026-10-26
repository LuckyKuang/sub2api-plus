package repository

import (
	"context"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.AccountRecoveryRepository = (*accountRepository)(nil)

func (r *accountRepository) ClearAccountRecoveryState(ctx context.Context, accountID int64) error {
	if r == nil || r.sql == nil {
		return errors.New("account recovery repository SQL executor is not configured")
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	result, err := executor.ExecContext(ctx, `
		WITH updated AS (
		/* account_recovery_atomic */
		UPDATE accounts
		SET status='active', error_message=NULL,
			rate_limited_at=NULL, rate_limit_reset_at=NULL, overload_until=NULL,
			temp_unschedulable_until=NULL, temp_unschedulable_reason=NULL,
			extra=(COALESCE(extra,'{}'::jsonb)-'model_rate_limits'-'antigravity_quota_scopes') ||
				CASE WHEN platform='grok' OR (platform='openai' AND type='apikey') THEN `+newRuntimeRecoveryGenerationSQL+` ELSE '{}'::jsonb END,
			updated_at=NOW()
		WHERE id=$1 AND deleted_at IS NULL
		RETURNING id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $2,updated.id,NULL,NULL FROM updated
	`, accountID, service.SchedulerOutboxEventAccountChanged)
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
