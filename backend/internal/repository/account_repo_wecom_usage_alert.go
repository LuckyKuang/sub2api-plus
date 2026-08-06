package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// ListDueWeComUsageAlertAccounts returns enabled WeCom usage-alert accounts whose
// next_run_at is due (missing/invalid next_run_at is treated as due).
func (r *accountRepository) ListDueWeComUsageAlertAccounts(ctx context.Context, now time.Time, limit int) ([]service.Account, error) {
	if limit <= 0 {
		return []service.Account{}, nil
	}
	if r == nil || r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}

	fetchLimit := limit * 3
	if fetchLimit < limit {
		fetchLimit = limit
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, name, platform, type, extra
		FROM accounts
		WHERE deleted_at IS NULL
			AND extra @> '{"wecom_usage_alert_enabled": true}'::jsonb
			AND jsonb_typeof(extra -> 'wecom_usage_alert_webhook_url') = 'string'
			AND length(trim(both from extra ->> 'wecom_usage_alert_webhook_url')) > 0
			AND jsonb_typeof(extra -> 'wecom_usage_alert_cron') = 'string'
			AND length(trim(both from extra ->> 'wecom_usage_alert_cron')) > 0
		ORDER BY id ASC
		LIMIT $1
	`, fetchLimit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.Account, 0, limit)
	for rows.Next() {
		var account service.Account
		var extraBytes []byte
		if err := rows.Scan(&account.ID, &account.Name, &account.Platform, &account.Type, &extraBytes); err != nil {
			return nil, err
		}
		if len(extraBytes) > 0 {
			if err := json.Unmarshal(extraBytes, &account.Extra); err != nil {
				return nil, err
			}
		}
		cfg := service.WeComUsageAlertConfigFromAccount(&account)
		if cfg.NextRunAt != nil && cfg.NextRunAt.After(now) {
			continue
		}
		out = append(out, account)
		if len(out) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
