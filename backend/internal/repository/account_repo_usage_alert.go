package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// ListDueUsageAlertAccounts returns accounts that may have at least one due usage-alert rule.
// Fine-grained due checks (per-rule next_run_at) happen in Go via AccountHasDueUsageAlertRule.
func (r *accountRepository) ListDueUsageAlertAccounts(ctx context.Context, now time.Time, limit int) ([]service.Account, error) {
	if limit <= 0 {
		return []service.Account{}, nil
	}
	if r == nil || r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}

	fetchLimit := limit * 5
	if fetchLimit < limit {
		fetchLimit = limit
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, name, platform, type, extra
		FROM accounts
		WHERE deleted_at IS NULL
			AND (
				(
					jsonb_typeof(extra -> 'usage_alert_rules') = 'array'
					AND jsonb_array_length(extra -> 'usage_alert_rules') > 0
				)
				OR (
					extra @> '{"wecom_usage_alert_enabled": true}'::jsonb
					AND jsonb_typeof(extra -> 'wecom_usage_alert_webhook_url') = 'string'
					AND length(trim(both from extra ->> 'wecom_usage_alert_webhook_url')) > 0
					AND jsonb_typeof(extra -> 'wecom_usage_alert_cron') = 'string'
					AND length(trim(both from extra ->> 'wecom_usage_alert_cron')) > 0
				)
			)
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
		if !service.AccountHasDueUsageAlertRule(&account, now) {
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

// ListDueWeComUsageAlertAccounts is a legacy alias.
func (r *accountRepository) ListDueWeComUsageAlertAccounts(ctx context.Context, now time.Time, limit int) ([]service.Account, error) {
	return r.ListDueUsageAlertAccounts(ctx, now, limit)
}
