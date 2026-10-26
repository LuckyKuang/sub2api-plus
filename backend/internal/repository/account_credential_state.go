package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.CredentialStateMutationRepository = (*accountRepository)(nil)

func (r *accountRepository) ApplyCredentialStateMutation(ctx context.Context, owner *service.Account, mutation service.CredentialStateMutation) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("credential state mutation requires an owner snapshot")
	}
	if err := validateCredentialMutationOwner(owner); err != nil {
		return false, err
	}
	if mutation.OpenAIPlanType != "" && (owner.Platform != service.PlatformOpenAI || owner.Type != service.AccountTypeOAuth || owner.Status != service.StatusActive) {
		return false, errors.New("OpenAI plan metadata requires an OpenAI owner")
	}
	if mutation.ClearRateLimit && (mutation.RateLimitUntil != nil || mutation.ObservedLimitedAt == nil || mutation.ObservedResetAt == nil) {
		return false, errors.New("credential rate-limit clear requires exact observed state")
	}
	if mutation.ClearTempUnschedulable && (mutation.TempUnschedulableUntil != nil || mutation.ObservedTempUntil == nil) {
		return false, errors.New("credential temporary cooldown clear requires exact observed state")
	}
	expected, err := json.Marshal(normalizeJSONMap(owner.Credentials))
	if err != nil {
		return false, err
	}
	args := []any{owner.ID, owner.Platform, owner.Type, string(expected), owner.ProxyID}
	proxyPredicate, args, err := credentialMutationProxyPredicate(owner.ProxyID, owner.Proxy, 5, args)
	if err != nil {
		return false, err
	}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	sets := []string{}
	predicates := []string{"a.id = $1", "a.platform = $2", "a.type = $3", "a.credentials = $4::jsonb", "a.proxy_id IS NOT DISTINCT FROM $5", "a.deleted_at IS NULL", "a.status = " + arg(owner.Status), proxyPredicate}
	if mutation.RequireTempUnschedulableUnchanged {
		predicates = append(predicates, "a.temp_unschedulable_until IS NOT DISTINCT FROM "+arg(mutation.ObservedTempUntil)+"::timestamptz", "COALESCE(a.temp_unschedulable_reason, '') = "+arg(mutation.ObservedTempReason))
	}
	if mutation.OpenAIPlanType != "" {
		sets = append(sets, "credentials = a.credentials || jsonb_build_object('plan_type', "+arg(mutation.OpenAIPlanType)+"::text)")
	}
	if len(mutation.Extra) > 0 {
		raw, err := json.Marshal(mutation.Extra)
		if err != nil {
			return false, err
		}
		sets = append(sets, "extra = COALESCE(a.extra, '{}'::jsonb) || "+arg(string(raw))+"::jsonb")
	}
	if mutation.RateLimitUntil != nil {
		p := arg(*mutation.RateLimitUntil)
		if mutation.ExtendRateLimitOnly {
			sets = append(sets, "rate_limited_at = CASE WHEN a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at < "+p+" THEN NOW() ELSE a.rate_limited_at END", "rate_limit_reset_at = GREATEST(a.rate_limit_reset_at, "+p+")")
		} else {
			sets = append(sets, "rate_limited_at = NOW()", "rate_limit_reset_at = "+p)
		}
	}
	if mutation.ClearRateLimit {
		predicates = append(predicates, "a.rate_limited_at = "+arg(*mutation.ObservedLimitedAt), "a.rate_limit_reset_at = "+arg(*mutation.ObservedResetAt))
		sets = append(sets, "rate_limited_at = NULL", "rate_limit_reset_at = NULL")
	}
	if mutation.TempUnschedulableUntil != nil {
		p, reason := arg(*mutation.TempUnschedulableUntil), arg(mutation.TempUnschedulableReason)
		sets = append(sets, "temp_unschedulable_reason = CASE WHEN a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until < "+p+" THEN "+reason+" ELSE a.temp_unschedulable_reason END", "temp_unschedulable_until = GREATEST(a.temp_unschedulable_until, "+p+")")
	}
	if mutation.ClearTempUnschedulable {
		predicates = append(predicates, "a.temp_unschedulable_until = "+arg(*mutation.ObservedTempUntil), "COALESCE(a.temp_unschedulable_reason, '') = "+arg(mutation.ObservedTempReason))
		sets = append(sets, "temp_unschedulable_until = NULL", "temp_unschedulable_reason = NULL")
	}
	if len(sets) == 0 {
		return false, errors.New("empty credential state mutation")
	}
	sets = append(sets, "updated_at = NOW()")
	event := arg(service.SchedulerOutboxEventAccountChanged)
	query := "WITH updated AS (UPDATE accounts AS a SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(predicates, " AND ") + " RETURNING a.id) INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload) SELECT " + event + ", updated.id, NULL, NULL FROM updated"
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	// Eviction cannot overwrite a login that commits between reading the row
	// and publishing a snapshot. The atomic outbox drives authoritative reload.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	r.deleteSchedulerAccountSnapshot(cleanupCtx, owner.ID)
	return true, nil
}
