package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// UpdateOAuthCredentialsIfUnchanged commits an OAuth token rotation only for
// the exact active grant and proxy sent to the provider. Scheduler invalidation
// is atomic with the update, so reauthorization cannot be overwritten.
func (r *accountRepository) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, id int64, platform string, expectedCredentials map[string]any, expectedProxyID *int64, credentials map[string]any) (bool, error) {
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
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
		UPDATE accounts AS a
		SET credentials = $1::jsonb, updated_at = NOW()
		WHERE a.id = $2 AND a.deleted_at IS NULL AND a.platform = $3 AND a.type = $4
			AND a.credentials = $5::jsonb AND a.proxy_id IS NOT DISTINCT FROM $6
			AND a.status = $7
		RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $8, updated.id, NULL, NULL FROM updated
	`, string(replacement), id, platform, service.AccountTypeOAuth, string(expected), expectedProxyID, service.StatusActive, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	// A snapshot read followed by publication can overwrite a concurrent login.
	// Evict instead; the atomic outbox drives the authoritative scheduler reload.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	r.deleteSchedulerAccountSnapshot(cleanupCtx, id)
	return true, nil
}
