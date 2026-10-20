package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// The row comparison, proxy lock and durable invalidation are one SQL statement.
// A late probe cannot overwrite a replacement grant or publish stale scheduling
// state, including when the proxy record changes without changing its ID.
func (r *accountRepository) UpdateCNProviderQuotaIfUnchanged(ctx context.Context, expected *service.Account, updates map[string]any) (bool, error) {
	if r == nil || r.sql == nil || expected == nil {
		return false, errors.New("quota snapshot repository is not configured")
	}
	if expected.Platform != service.PlatformKimi && expected.Platform != service.PlatformZhipu && expected.Platform != service.PlatformMiniMax {
		return false, errors.New("unsupported quota snapshot platform")
	}
	credentials, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	proxy := proxyProbeIdentity{}
	if expected.Proxy != nil {
		proxy = proxyProbeIdentityFromService(expected.Proxy)
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH proxy_guard AS (
			SELECT id FROM proxies WHERE id=$6 AND deleted_at IS NULL
				AND protocol=$9 AND host=$10 AND port=$11
				AND COALESCE(username,'')=$12 AND COALESCE(password,'')=$13 AND status=$14
			FOR SHARE
		), updated AS (
			UPDATE accounts AS a SET extra=COALESCE(a.extra,'{}'::jsonb)||$1::jsonb, updated_at=NOW()
			WHERE a.id=$2 AND a.deleted_at IS NULL AND a.platform=$3 AND a.type=$4
				AND a.credentials=$5::jsonb AND a.proxy_id IS NOT DISTINCT FROM $6
				AND a.status=$7 AND ($6::bigint IS NULL OR EXISTS(SELECT 1 FROM proxy_guard))
			RETURNING a.id
		)
		INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
		SELECT $8,updated.id,NULL,NULL FROM updated
	`, string(payload), expected.ID, expected.Platform, expected.Type, string(credentials), expected.ProxyID, expected.Status, service.SchedulerOutboxEventAccountChanged,
		proxy.protocol, proxy.host, proxy.port, proxy.username, proxy.password, proxy.status)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, expected.ID)
	return true, nil
}
