package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.OAuthCredentialOwnerRepository = (*accountRepository)(nil)

// UpdateOAuthCredentialsIfUnchanged commits authorization material only to its
// captured credential, route and scheduler owner. Recovery cannot erase a newer
// account/model cooldown. Unrelated current metadata is merged in the write.
func (r *accountRepository) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, expected *service.Account, accountType string, credentials, extra map[string]any, recover bool) (bool, error) {
	if expected != nil && expected.Platform == service.PlatformOpenAI && expected.Type != accountType {
		if r == nil || r.client == nil {
			return false, errors.New("OAuth credential repository transaction client is not configured")
		}
		var applied bool
		err := r.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repository service.AccountRepository) error {
			writer, ok := repository.(*accountRepository)
			if !ok {
				return errors.New("oauth credential repository transaction writer is invalid")
			}
			// Match the shared binding-writer order: proxy, then account, then
			// family/group state. The lock and following read are distinct SQL
			// statements so children committed while waiting become visible.
			if err := lockLiveAccountProxy(writeCtx, writer.client, expected.ProxyID); err != nil {
				return err
			}
			if err := writer.LockAccountCredentialFamily(writeCtx, expected.ID); err != nil {
				return err
			}
			shadows, err := writer.ListShadowsByParent(writeCtx, expected.ID)
			if err != nil {
				return err
			}
			if len(shadows) != 0 {
				return infraerrors.BadRequest("SPARK_SHADOW_PARENT_IMMUTABLE_TYPE", "cannot change account type while it has a spark shadow; delete the shadow first")
			}
			applied, err = writer.updateOAuthCredentialsIfUnchanged(writeCtx, expected, accountType, credentials, extra, recover)
			return err
		})
		if err != nil {
			return false, err
		}
		return applied, nil
	}
	return r.updateOAuthCredentialsIfUnchanged(ctx, expected, accountType, credentials, extra, recover)
}

func (r *accountRepository) updateOAuthCredentialsIfUnchanged(ctx context.Context, expected *service.Account, accountType string, credentials, extra map[string]any, recover bool) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("OAuth credential repository SQL executor is not configured")
	}
	if expected == nil || expected.ID <= 0 || !expected.IsOAuth() || expected.IsCredentialShadow() || len(credentials) == 0 ||
		(accountType != service.AccountTypeOAuth && accountType != service.AccountTypeSetupToken) {
		return false, errors.New("OAuth credential owner is invalid")
	}
	expectedJSON, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(credentials)
	if err != nil {
		return false, err
	}
	extraJSON, err := json.Marshal(withoutRuntimeRecoveryGeneration(extra))
	if err != nil {
		return false, err
	}
	previousExtraJSON, err := json.Marshal(normalizeJSONMap(expected.Extra))
	if err != nil {
		return false, err
	}
	executor := r.sql
	contextTx := dbent.TxFromContext(ctx)
	if contextTx != nil {
		executor = contextTx.Client()
	}
	args := []any{
		string(credentialsJSON), string(extraJSON), accountType, recover,
		expected.ID, expected.Platform, expected.Type, string(expectedJSON), expected.ProxyID,
		expected.Status, expected.ErrorMessage, expected.Schedulable, expected.TempUnschedulableUntil, expected.TempUnschedulableReason,
		expected.RateLimitedAt, expected.RateLimitResetAt, expected.OverloadUntil, expected.ExpiresAt, expected.AutoPauseOnExpired, string(previousExtraJSON),
		expected.ParentAccountID, expected.QuotaDimensionOrDefault(),
	}
	proxyPredicate, args, err := cnProxyOwnerPredicate(expected, args)
	if err != nil {
		return false, err
	}
	query := `
		WITH updated AS (
		/* oauth_credential_owner_cas */
		UPDATE accounts AS a
		SET type=$3, credentials=$1::jsonb,
			extra=(CASE WHEN $4::boolean THEN (COALESCE(a.extra,'{}'::jsonb)||$2::jsonb)-'model_rate_limits'-'antigravity_quota_scopes' ELSE COALESCE(a.extra,'{}'::jsonb)||$2::jsonb END) ||
				CASE WHEN $4::boolean AND a.platform='grok' THEN ` + newRuntimeRecoveryGenerationSQL + ` ELSE '{}'::jsonb END,
			status=CASE WHEN $4 THEN 'active' ELSE a.status END,
			error_message=CASE WHEN $4 THEN NULL ELSE a.error_message END,
			temp_unschedulable_until=CASE WHEN $4 THEN NULL ELSE a.temp_unschedulable_until END,
			temp_unschedulable_reason=CASE WHEN $4 THEN NULL ELSE a.temp_unschedulable_reason END,
			rate_limited_at=CASE WHEN $4 THEN NULL ELSE a.rate_limited_at END,
			rate_limit_reset_at=CASE WHEN $4 THEN NULL ELSE a.rate_limit_reset_at END,
			overload_until=CASE WHEN $4 THEN NULL ELSE a.overload_until END,
			updated_at=NOW()
		WHERE a.id=$5 AND a.deleted_at IS NULL
			AND a.platform=$6 AND a.type=$7 AND a.credentials=$8::jsonb
			AND a.proxy_id IS NOT DISTINCT FROM $9
			AND a.status=$10 AND COALESCE(a.error_message,'')=$11 AND a.schedulable=$12
			AND a.temp_unschedulable_until IS NOT DISTINCT FROM $13
			AND COALESCE(a.temp_unschedulable_reason,'')=$14
			AND a.rate_limited_at IS NOT DISTINCT FROM $15
			AND a.rate_limit_reset_at IS NOT DISTINCT FROM $16
			AND a.overload_until IS NOT DISTINCT FROM $17
			AND a.expires_at IS NOT DISTINCT FROM $18
			AND a.auto_pause_on_expired=$19
			AND a.parent_account_id IS NOT DISTINCT FROM $21 AND a.quota_dimension=$22
			AND (a.extra->'runtime_recovery_generation') IS NOT DISTINCT FROM ($20::jsonb->'runtime_recovery_generation')
			AND (NOT $4 OR (
				(a.extra->'model_rate_limits') IS NOT DISTINCT FROM ($20::jsonb->'model_rate_limits')
				AND (a.extra->'antigravity_quota_scopes') IS NOT DISTINCT FROM ($20::jsonb->'antigravity_quota_scopes')
				AND (a.extra->'grok_needs_reauth') IS NOT DISTINCT FROM ($20::jsonb->'grok_needs_reauth')
				AND (a.extra->'grok_needs_reauth_reason') IS NOT DISTINCT FROM ($20::jsonb->'grok_needs_reauth_reason')
				AND (a.extra->'grok_needs_reauth_at') IS NOT DISTINCT FROM ($20::jsonb->'grok_needs_reauth_at')
			))
		` + proxyPredicate + ` RETURNING a.id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT ` + fmt.Sprintf("$%d", len(args)+1) + `,updated.id,NULL,NULL FROM updated
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
