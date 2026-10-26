package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// Credential acquisition has its own quarantine entries before inference
// starts. They compare the captured login and the complete route that produced
// the failure, and commit quarantine plus scheduler invalidation together.
func (r *accountRepository) setGrokCredentialFailureIfMatch(ctx context.Context, id int64, snapshot service.GrokCredentialMutationSnapshot, reason string, until *time.Time) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("grok credential failure repository SQL executor is not configured")
	}
	expected := snapshot.Owner
	if expected == nil || expected.ID != id || id <= 0 || !expected.IsGrokOAuth() {
		return false, errors.New("grok credential failure owner is invalid")
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, errors.New("grok credential failure owner credentials are invalid")
	}
	permanent := until == nil
	args := []any{id, string(credentials), expected.ProxyID, until, reason, permanent}
	var proxyPredicate string
	if permanent && reason == string(service.GrokCredentialReasonProxyInvalid) {
		// A missing hydrated edge is the structural failure being quarantined.
		// Lock its configured row even when soft deleted: a concurrent restore
		// must win the recheck instead of an MVCC NOT EXISTS seeing old state.
		if !snapshot.ProxyMissing || expected.ProxyID == nil || *expected.ProxyID <= 0 || expected.Proxy != nil {
			return false, errors.New("missing-proxy quarantine requires explicit missing-route evidence")
		}
		proxyPredicate = `
			AND NOT EXISTS (
				WITH captured_proxy AS MATERIALIZED (
					SELECT p.deleted_at FROM proxies AS p
					WHERE p.id = a.proxy_id
					FOR SHARE
				)
				SELECT 1 FROM captured_proxy WHERE captured_proxy.deleted_at IS NULL
			)
		`
	} else {
		proxyPredicate, args, err = cnProxyOwnerPredicate(expected, args)
		if err != nil {
			return false, err
		}
	}
	query := `
		WITH updated AS (
		/* grok_credential_failure_owner_cas */
		UPDATE accounts AS a
		SET status = CASE WHEN $6::boolean THEN 'error' ELSE a.status END,
			error_message = CASE WHEN $6::boolean THEN $5 ELSE a.error_message END,
			schedulable = CASE WHEN $6::boolean THEN FALSE ELSE a.schedulable END,
			temp_unschedulable_until = CASE WHEN $6::boolean THEN a.temp_unschedulable_until ELSE GREATEST(a.temp_unschedulable_until, $4::timestamptz) END,
			temp_unschedulable_reason = CASE WHEN $6::boolean THEN a.temp_unschedulable_reason ELSE $5 END,
			updated_at = NOW()
		WHERE a.id = $1
			AND a.deleted_at IS NULL
			AND a.status = 'active'
			AND a.platform = 'grok'
			AND a.type = 'oauth'
			AND a.schedulable IS TRUE
			AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())
			AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
			AND (a.overload_until IS NULL OR a.overload_until <= NOW())
			AND (a.auto_pause_on_expired IS NOT TRUE OR a.expires_at IS NULL OR a.expires_at > NOW())
			AND a.credentials = $2::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $3
		` + proxyPredicate + ` RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT ` + fmt.Sprintf("$%d", len(args)+1) + `, updated.id, NULL, NULL FROM updated
	`
	args = append(args, service.SchedulerOutboxEventAccountChanged)
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
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
		r.markTransactionAccountChanged(id)
	} else {
		r.deleteSchedulerAccountSnapshotDetached(ctx, id)
	}
	return true, nil
}
