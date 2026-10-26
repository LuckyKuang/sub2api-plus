package repository

import (
	"context"
	"encoding/json"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// UpdateOAuthRefreshCredentialsIfUnchanged commits an OAuth token rotation only for
// the exact active grant and proxy sent to the provider. Scheduler invalidation
// is atomic with the update, so reauthorization cannot be overwritten.
func (r *accountRepository) UpdateOAuthRefreshCredentialsIfUnchanged(ctx context.Context, expectedOwner *service.Account, credentials map[string]any) (bool, error) {
	if err := validateCredentialMutationOwner(expectedOwner); err != nil {
		return false, err
	}
	if expectedOwner.Type != service.AccountTypeOAuth {
		return false, errors.New("credential refresh requires an OAuth owner")
	}

	id := expectedOwner.ID
	platform := expectedOwner.Platform
	expectedCredentials := expectedOwner.Credentials
	expectedProxyID := expectedOwner.ProxyID
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	expected, err := json.Marshal(normalizeJSONMap(expectedCredentials))
	if err != nil {
		return false, err
	}
	replacement, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	args := []any{string(replacement), id, platform, service.AccountTypeOAuth, string(expected), expectedProxyID, service.StatusActive, service.SchedulerOutboxEventAccountChanged}
	proxyPredicate, args, err := credentialMutationProxyPredicate(expectedOwner.ProxyID, expectedOwner.Proxy, 6, args)
	if err != nil {
		return false, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	result, err := executor.ExecContext(ctx, `
		WITH updated AS (
		UPDATE accounts AS a
		SET credentials = $1::jsonb, updated_at = NOW()
		WHERE a.id = $2 AND a.deleted_at IS NULL AND a.platform = $3 AND a.type = $4
			AND a.credentials = $5::jsonb AND a.proxy_id IS NOT DISTINCT FROM $6
			AND a.status = $7
		AND `+proxyPredicate+`
		RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $8, updated.id, NULL, NULL FROM updated
	`, args...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	if contextTx != nil || r.transactionAccountIDs != nil {
		r.markTransactionAccountChanged(id)
	} else {
		r.deleteSchedulerAccountSnapshotDetached(ctx, id)
	}
	return true, nil
}
