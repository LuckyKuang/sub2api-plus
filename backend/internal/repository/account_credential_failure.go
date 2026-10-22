package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// SetCredentialErrorIfUnchanged fences request-path credential quarantine
// against concurrent rotation, reauthorization and account/proxy changes. The
// state transition and scheduler notification commit in the same statement.
func (r *accountRepository) SetCredentialErrorIfUnchanged(ctx context.Context, expectedOwner *service.Account, reason string) (bool, error) {
	if err := validateCredentialMutationOwner(expectedOwner); err != nil {
		return false, err
	}

	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	credentials := expectedOwner.Credentials
	proxyID := expectedOwner.ProxyID
	expected, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	args := []any{service.StatusError, reason, id, service.StatusActive, platform, kind, string(expected), proxyID, service.SchedulerOutboxEventAccountChanged}
	proxyPredicate, args, err := credentialMutationProxyPredicate(expectedOwner.ProxyID, expectedOwner.Proxy, 8, args)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		UPDATE accounts AS a
		SET status = $1, error_message = $2, schedulable = FALSE, updated_at = NOW()
		WHERE a.id = $3 AND a.deleted_at IS NULL AND a.status = $4
			AND a.platform = $5 AND a.type = $6
			AND a.credentials = $7::jsonb AND a.proxy_id IS NOT DISTINCT FROM $8
		AND `+proxyPredicate+`
		RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $9, updated.id, NULL, NULL FROM updated
	`, args...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	r.deleteSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

// A recoverable 401 may cool down only the grant that received it. The active
// status is preserved so background refresh can recover the account. A shorter
// cooldown cannot replace a longer one, and outbox failure rolls back the write.
func (r *accountRepository) SetCredentialTempUnschedulableIfUnchanged(ctx context.Context, expectedOwner *service.Account, until time.Time, reason string) (bool, error) {
	if err := validateCredentialMutationOwner(expectedOwner); err != nil {
		return false, err
	}

	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	credentials := expectedOwner.Credentials
	proxyID := expectedOwner.ProxyID
	expected, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	args := []any{until, reason, id, service.StatusActive, platform, kind, string(expected), proxyID, service.SchedulerOutboxEventAccountChanged}
	proxyPredicate, args, err := credentialMutationProxyPredicate(expectedOwner.ProxyID, expectedOwner.Proxy, 8, args)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		UPDATE accounts AS a
		SET temp_unschedulable_until = $1, temp_unschedulable_reason = $2, updated_at = NOW()
		WHERE a.id = $3 AND a.deleted_at IS NULL AND a.status = $4
			AND a.platform = $5 AND a.type = $6
			AND a.credentials = $7::jsonb AND a.proxy_id IS NOT DISTINCT FROM $8
			AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until < $1)
		AND `+proxyPredicate+`
		RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $9, updated.id, NULL, NULL FROM updated
	`, args...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	r.deleteSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
