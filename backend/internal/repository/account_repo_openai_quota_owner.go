package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.OpenAIQuotaCredentialSnapshotRepository = (*accountRepository)(nil)

// UpdateOpenAIQuotaCredentialsIfUnchanged publishes a quota refresh or Agent
// task only for the complete credential owner and route that authorized it.
// PostgreSQL rechecks both predicates after concurrent account/proxy writers;
// the credentials and scheduler event commit or roll back together.
func (r *accountRepository) UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *service.Account, credentials map[string]any) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	if expected == nil || expected.ID <= 0 || !expected.IsOpenAIOAuth() || expected.IsCredentialShadow() || len(credentials) == 0 {
		return false, errors.New("quota credential owner is invalid")
	}
	expectedJSON, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(credentials)
	if err != nil {
		return false, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	args := []any{string(credentialsJSON), expected.ID, expected.Platform, expected.Type, string(expectedJSON), expected.ProxyID}
	proxyPredicate, args, err := cnProxyOwnerPredicate(expected, args)
	if err != nil {
		return false, err
	}
	query := `WITH updated AS (
		/* openai_quota_owner_cas */
		UPDATE accounts AS a
		SET credentials = $1::jsonb, updated_at = NOW()
		WHERE a.id = $2
			AND a.deleted_at IS NULL AND a.parent_account_id IS NULL
			AND a.platform = $3 AND a.type = $4
			AND a.credentials = $5::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $6
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
