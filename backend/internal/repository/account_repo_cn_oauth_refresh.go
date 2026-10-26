package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.CNOAuthCredentialSnapshotRepository = (*accountRepository)(nil)
var _ service.CNOAuthLoginSnapshotRepository = (*accountRepository)(nil)

// UpdateCNOAuthCredentialsIfUnchanged commits a native OAuth refresh only while
// the complete credential owner still matches the login that was refreshed.
// A concurrent reauthorization wins over a late old-login provider response.
func (r *accountRepository) UpdateCNOAuthCredentialsIfUnchanged(
	ctx context.Context,
	expected *service.Account,
	credentials map[string]any,
) (bool, error) {
	return r.updateCNOAuthCredentialsIfUnchanged(ctx, expected, credentials, false)
}

// CompleteCNOAuthLoginIfUnchanged atomically replaces an existing login and
// recovers the runtime failures captured when authorization started. A newer
// failure or a manual scheduling change requires restarting authorization.
func (r *accountRepository) CompleteCNOAuthLoginIfUnchanged(ctx context.Context, expected *service.Account, credentials map[string]any) (bool, error) {
	return r.updateCNOAuthCredentialsIfUnchanged(ctx, expected, credentials, true)
}

func (r *accountRepository) updateCNOAuthCredentialsIfUnchanged(ctx context.Context, expected *service.Account, credentials map[string]any, completeLogin bool) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	if expected == nil || expected.ID <= 0 || len(credentials) == 0 {
		return false, errors.New("native OAuth credential owner is invalid")
	}
	if (!completeLogin && !expected.IsDomesticOAuth()) || (completeLogin && (expected.Type != service.AccountTypeOAuth || !cnoauth.Supported(expected.Platform))) {
		return false, errors.New("native OAuth credential owner is invalid")
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
	setRecovery, recoveryPredicate := "", ""
	marker := "cn_oauth_refresh_cas"
	if completeLogin {
		marker = "cn_oauth_login_cas"
		extraJSON, err := json.Marshal(normalizeJSONMap(expected.Extra))
		if err != nil {
			return false, err
		}
		first := len(args) + 1
		args = append(args, expected.Status, expected.ErrorMessage, expected.Schedulable,
			expected.RateLimitedAt, expected.RateLimitResetAt, expected.OverloadUntil,
			expected.TempUnschedulableUntil, expected.TempUnschedulableReason, string(extraJSON))
		setRecovery = `,
			status = 'active', error_message = '',
			rate_limited_at = NULL, rate_limit_reset_at = NULL, overload_until = NULL,
			temp_unschedulable_until = NULL, temp_unschedulable_reason = NULL,
			extra = COALESCE(a.extra, '{}'::jsonb) - 'model_rate_limits' - 'antigravity_quota_scopes'`
		recoveryPredicate = fmt.Sprintf(`
			AND a.status = $%d AND COALESCE(a.error_message, '') = $%d AND a.schedulable = $%d
			AND a.rate_limited_at IS NOT DISTINCT FROM $%d
			AND a.rate_limit_reset_at IS NOT DISTINCT FROM $%d
			AND a.overload_until IS NOT DISTINCT FROM $%d
			AND a.temp_unschedulable_until IS NOT DISTINCT FROM $%d
			AND COALESCE(a.temp_unschedulable_reason, '') = $%d
			AND (a.extra->'model_rate_limits') IS NOT DISTINCT FROM ($%d::jsonb->'model_rate_limits')
			AND (a.extra->'antigravity_quota_scopes') IS NOT DISTINCT FROM ($%d::jsonb->'antigravity_quota_scopes')
		`, first, first+1, first+2, first+3, first+4, first+5, first+6, first+7, first+8, first+8)
	}
	query := `
		WITH updated AS (
		/* ` + marker + ` */
		UPDATE accounts AS a
		SET credentials = $1::jsonb,
			updated_at = NOW()` + setRecovery + `
		WHERE a.id = $2
			AND a.deleted_at IS NULL
			AND a.platform = $3
			AND a.type = $4
			AND a.credentials = $5::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $6
		` + proxyPredicate + recoveryPredicate + ` RETURNING a.id
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
