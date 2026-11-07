package service

import (
	"context"
	"errors"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

// Only fixed reasons and internal routing metadata enter indexed system logs.
// SQL/transport errors can contain credentials or user data; never log err.Error.
func logOpenAISessionRouteFailure(ctx context.Context, err error, stage string, route OpenAISessionRoute, candidateID int64) {
	var code string
	switch {
	case errors.Is(err, ErrOpenAISessionRouteStoreUnavailable):
		code = "route_store_unavailable"
	case errors.Is(err, ErrOpenAISessionRouteLeaseLost):
		code = "lease_lost"
	case errors.Is(err, ErrOpenAISessionRouteNotAccepted):
		code = "candidate_not_accepted"
	default:
		return
	}
	reason := code
	if stage == "selection" && code == "lease_lost" && openAISessionRouteLeaseActive(route, time.Now()) {
		reason = "migration_in_progress"
	}
	remaining := int64(0)
	if route.LeaseID != "" && !route.LeaseExpiresAt.IsZero() {
		if ms := time.Until(route.LeaseExpiresAt).Milliseconds(); ms > 0 {
			remaining = ms
		}
	}
	logger.FromContext(ctx).Warn("openai_session_route_failed",
		zap.String("stage", stage), zap.String("error_code", code), zap.String("reason", reason),
		zap.Int64("owner_account_id", route.OwnerAccountID), zap.Int64("candidate_account_id", candidateID),
		zap.Int64("generation", route.Generation), zap.Int64("lease_remaining_ms", remaining))
}
