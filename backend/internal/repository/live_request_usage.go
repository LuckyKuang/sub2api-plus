package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// ReadLiveUsage does one bounded lookup using the unique billing key and owner.
// It does not scan a time range, calculate statistics, or guess request matches.
func (r *usageLogRepository) ReadLiveUsage(ctx context.Context, keys []service.LiveUsageKey) ([]service.UsageLog, error) {
	if len(keys) == 0 {
		return []service.UsageLog{}, nil
	}
	if len(keys) > 200 {
		return nil, fmt.Errorf("live usage lookup exceeds window")
	}
	conditions := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*3)
	for _, key := range keys {
		n := len(args)
		conditions = append(conditions, fmt.Sprintf("(request_id = $%d AND api_key_id = $%d AND user_id = $%d)", n+1, n+2, n+3))
		args = append(args, key.RequestID, key.APIKeyID, key.UserID)
	}
	logs, err := r.queryUsageLogs(ctx, "SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE "+strings.Join(conditions, " OR ")+" LIMIT 200", args...)
	if err != nil {
		return nil, err
	}
	groups, err := r.loadGroups(ctx, collectUsageLogIDs(logs).groupIDs)
	if err != nil {
		return nil, err
	}
	for i := range logs {
		if logs[i].GroupID != nil {
			logs[i].Group = groups[*logs[i].GroupID]
		}
	}
	return logs, nil
}

var _ service.LiveUsageReader = (*usageLogRepository)(nil)
