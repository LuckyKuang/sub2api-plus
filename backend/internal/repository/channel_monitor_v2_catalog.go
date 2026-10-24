package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
)

// Keep the requested-model and composite routing dimensions identical to the
// passive error materializer. A routed/upstream model must not replace the name
// the caller actually requested in the settings catalog or error samples.
const channelMonitorV2ErrorModelSQL = `COALESCE(NULLIF(TRIM(current_error.requested_model), ''), NULLIF(TRIM(current_error.model), ''), 'unknown')`
const channelMonitorV2ErrorPlatformSQL = `lower(CASE
  WHEN g.platform = 'composite' THEN COALESCE(NULLIF(TRIM(a.platform), ''), NULLIF(NULLIF(lower(TRIM(current_error.platform)), ''), 'composite'), 'unknown')
  ELSE COALESCE(NULLIF(TRIM(current_error.platform), ''), 'unknown')
END)`

// Catalog observations deliberately bypass saved display/enable/exclusion
// policies and materialization coverage. This lets an operator discover and
// restore hidden models, or prepare a disabled platform's unsaved settings.
func (r *channelMonitorV2Repository) GetModelCatalog(ctx context.Context, filter service.ChannelMonitorV2Filter, cfg service.ChannelMonitorV2Config) (*service.ChannelMonitorV2ModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	usageScope, args := channelMonitorV2ModelCatalogWhere(filter, channelMonitorV2PlatformSQL, "ul.group_id")
	errorScope, _ := channelMonitorV2ModelCatalogWhere(filter, channelMonitorV2ErrorPlatformSQL, "current_error.group_id")
	query := `WITH observations AS (
  SELECT ` + channelMonitorV2PlatformSQL + ` AS platform, ` + channelMonitorV2ModelSQL + ` AS model,
    COALESCE(ul.group_id, 0) AS group_id,
    COUNT(DISTINCT COALESCE(NULLIF(ul.request_id, ''), 'usage:' || ul.id::text))::bigint AS success_requests,
    0::bigint AS error_requests
  FROM usage_logs ul
  LEFT JOIN groups g ON g.id = ul.group_id
  LEFT JOIN accounts a ON a.id = ul.account_id
  WHERE ul.created_at >= $1 AND ul.created_at < $2
    AND ` + usageLogSuccessFilterUL + ` AND COALESCE(ul.request_type, 0) NOT IN (4, 6)` + usageScope + `
  GROUP BY 1,2,3
  UNION ALL
  SELECT ` + channelMonitorV2ErrorPlatformSQL + ` AS platform,
      ` + channelMonitorV2ErrorModelSQL + ` AS model,
      COALESCE(current_error.group_id, 0) AS group_id, 0::bigint, COUNT(*)::bigint
    FROM ops_error_logs current_error
    LEFT JOIN groups g ON g.id = current_error.group_id
    LEFT JOIN accounts a ON a.id = current_error.account_id
    WHERE current_error.created_at >= $1 AND current_error.created_at < $2
      AND NOT current_error.is_count_tokens
      AND (COALESCE(current_error.status_code, 0) >= 400 OR current_error.error_type = 'cyber_policy')` + errorScope + `
      AND (NULLIF(current_error.request_id, '') IS NULL OR NOT EXISTS (
        SELECT 1 FROM ops_error_logs newer
        WHERE newer.request_id = current_error.request_id
          AND newer.created_at >= $1 AND newer.created_at < $2
          AND NOT newer.is_count_tokens
          AND (COALESCE(newer.status_code, 0) >= 400 OR newer.error_type = 'cyber_policy')
          AND (newer.created_at, newer.id) > (current_error.created_at, current_error.id)
      ))
  GROUP BY 1,2,3
)
SELECT platform, model, SUM(success_requests)::bigint, SUM(error_requests)::bigint
FROM observations
GROUP BY platform, model ORDER BY platform, model LIMIT ` + fmt.Sprint(service.ChannelMonitorV2ModelCatalogLimit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("get channel monitor v2 model catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type key struct{ platform, model string }
	items := map[key]*service.ChannelMonitorV2ModelCatalogItem{}
	for rows.Next() {
		var item service.ChannelMonitorV2ModelCatalogItem
		if err := rows.Scan(&item.Platform, &item.Model, &item.SuccessRequests, &item.ErrorRequests); err != nil {
			return nil, err
		}
		item.Sources = []string{}
		if item.SuccessRequests > 0 {
			item.Sources = append(item.Sources, "success")
		}
		if item.ErrorRequests > 0 {
			item.Sources = append(item.Sources, "error")
		}
		items[key{item.Platform, item.Model}] = &item
		if len(items) > service.ChannelMonitorV2ModelCatalogLimit {
			return nil, service.ErrChannelMonitorV2CatalogTooLarge
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, platform := range cfg.Platforms {
		if len(filter.Platforms) > 0 && !containsString(filter.Platforms, platform.Platform) {
			continue
		}
		for _, source := range []struct {
			name   string
			models []string
		}{{"configured", platform.Models}, {"excluded", platform.ExcludedModels}} {
			for _, model := range source.models {
				k := key{platform.Platform, model}
				item, ok := items[k]
				if !ok {
					item = &service.ChannelMonitorV2ModelCatalogItem{Platform: platform.Platform, Model: model, Sources: []string{}}
					items[k] = item
					if len(items) > service.ChannelMonitorV2ModelCatalogLimit {
						return nil, service.ErrChannelMonitorV2CatalogTooLarge
					}
				}
				item.Sources = append(item.Sources, source.name)
			}
		}
	}
	result := &service.ChannelMonitorV2ModelCatalog{
		ConfigVersion: cfg.Version, Range: filter.Range, Start: filter.Start, End: filter.End,
		Items: make([]service.ChannelMonitorV2ModelCatalogItem, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, *item)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if a.Platform != b.Platform {
			return a.Platform < b.Platform
		}
		return a.Model < b.Model
	})
	return result, nil
}

func channelMonitorV2ModelCatalogWhere(filter service.ChannelMonitorV2Filter, platformSQL, groupSQL string) (string, []any) {
	conditions := []string{}
	args := []any{filter.Start, filter.End}
	if len(filter.Platforms) > 0 {
		args = append(args, pq.Array(filter.Platforms))
		conditions = append(conditions, fmt.Sprintf("%s = ANY($%d)", platformSQL, len(args)))
	}
	if len(filter.GroupIDs) > 0 {
		args = append(args, pq.Array(filter.GroupIDs))
		conditions = append(conditions, fmt.Sprintf("%s = ANY($%d)", groupSQL, len(args)))
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " AND " + strings.Join(conditions, " AND "), args
}
