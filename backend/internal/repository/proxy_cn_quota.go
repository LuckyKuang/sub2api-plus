package repository

import "context"

// Proxy identity changes must also replace the scheduler's embedded proxy.
// Removing only the binding retains administrative observations while making
// the old quota windows ineligible until the new route is probed.
func invalidateProxyCNQuotaSnapshots(ctx context.Context, exec sqlExecutor, proxyID int64) ([]int64, error) {
	rows, err := exec.QueryContext(ctx, `
		UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)
			- 'kimi_quota_generation' - 'zhipu_quota_generation' - 'minimax_quota_generation',
			updated_at=NOW()
		WHERE proxy_id=$1 AND deleted_at IS NULL AND platform IN ('kimi','zhipu','minimax')
			AND extra ? (platform || '_quota_generation')
		RETURNING id
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
