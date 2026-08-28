package middleware

import (
	"errors"
	"math"
	"strconv"
	"time"

	pkgerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// isSubscriptionUsageLimitError identifies the quota errors that are backed
// by an explicit usage window and therefore must expose retry/reset headers.
func isSubscriptionUsageLimitError(err error) bool {
	return errors.Is(err, service.ErrDailyLimitExceeded) ||
		errors.Is(err, service.ErrWeeklyLimitExceeded) ||
		errors.Is(err, service.ErrMonthlyLimitExceeded) ||
		errors.Is(err, service.ErrFiveHourLimitExceeded) ||
		errors.Is(err, service.ErrGroupSubscriptionLimitExceeded)
}

func applySubscriptionQuotaResetHeaders(c *gin.Context, err error) {
	appErr := pkgerrors.FromError(err)
	if appErr == nil || appErr.Metadata == nil {
		return
	}
	raw := appErr.Metadata["window_resets_at"]
	resetAt, parseErr := time.Parse(time.RFC3339, raw)
	if parseErr != nil || resetAt.IsZero() {
		return
	}
	seconds := int(math.Ceil(time.Until(resetAt).Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
}

func subscriptionQuotaResponseCode(err error) string {
	appErr := pkgerrors.FromError(err)
	if appErr != nil && appErr.Reason != "" {
		return appErr.Reason
	}
	return "USAGE_LIMIT_EXCEEDED"
}
