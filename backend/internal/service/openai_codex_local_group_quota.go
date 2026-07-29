package service

import (
	"context"
	"math"
	"time"
)

const (
	codexPrimaryWindowDuration   = 7 * 24 * time.Hour
	codexSecondaryWindowDuration = 5 * time.Hour
)

// CodexLocalGroupQuotaUsage is the deliberately local response body served to
// Codex from /backend-api/wham/usage. It contains only the current API key's
// subscription quota and never account, email, OAuth, or upstream metadata.
type CodexLocalGroupQuotaUsage struct {
	RateLimit CodexLocalGroupRateLimit `json:"rate_limit"`
}

// CodexLocalGroupRateLimit follows the public /wham/usage rate_limit shape
// without reusing the account-oriented OpenAIQuotaUsage DTO.
type CodexLocalGroupRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *CodexLocalQuotaWindow `json:"primary_window"`
	SecondaryWindow *CodexLocalQuotaWindow `json:"secondary_window"`
}

type CodexLocalQuotaWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

// IsCodexLocalGroupQuotaEnabledForGroup is intentionally restrictive: the
// local view is only meaningful for an OpenAI subscription group. All other
// groups retain their existing response behavior.
func (s *OpenAIGatewayService) IsCodexLocalGroupQuotaEnabledForGroup(ctx context.Context, group *Group) bool {
	return s != nil && s.settingService != nil && group != nil &&
		group.Platform == PlatformOpenAI && group.IsSubscriptionType() &&
		s.settingService.IsOpenAICodexLocalGroupQuotaEnabled(ctx)
}

// BuildCodexLocalGroupQuotaUsage converts local subscription usage into the
// Codex primary (7-day) and secondary (5-hour) window order. An inactive or
// expired window is nil rather than a fabricated reset countdown.
func BuildCodexLocalGroupQuotaUsage(group *Group, sub *UserSubscription, now time.Time) *CodexLocalGroupQuotaUsage {
	if group == nil || sub == nil {
		return nil
	}
	primary := buildCodexLocalQuotaWindow(group.WeeklyLimitUSD, sub.WeeklyUsageUSD, sub.WeeklyWindowStart, codexPrimaryWindowDuration, now)
	secondary := buildCodexLocalQuotaWindow(group.FiveHourLimitUSD, sub.FiveHourUsageUSD, sub.FiveHourWindowStart, codexSecondaryWindowDuration, now)
	limitReached := (primary != nil && primary.UsedPercent >= 100) || (secondary != nil && secondary.UsedPercent >= 100)
	return &CodexLocalGroupQuotaUsage{
		RateLimit: CodexLocalGroupRateLimit{
			Allowed:         !limitReached,
			LimitReached:    limitReached,
			PrimaryWindow:   primary,
			SecondaryWindow: secondary,
		},
	}
}

func buildCodexLocalQuotaWindow(limit *float64, used float64, start *time.Time, duration time.Duration, now time.Time) *CodexLocalQuotaWindow {
	if limit == nil || *limit <= 0 || start == nil || !start.Add(duration).After(now) {
		return nil
	}
	resetAt := start.Add(duration)
	usedPercent := math.Min(100, math.Max(0, (used / *limit)*100))
	remaining := int64(math.Ceil(resetAt.Sub(now).Seconds()))
	if remaining < 1 {
		remaining = 1
	}
	return &CodexLocalQuotaWindow{
		UsedPercent:        usedPercent,
		LimitWindowSeconds: int64(duration.Seconds()),
		ResetAfterSeconds:  remaining,
		ResetAt:            resetAt.Unix(),
	}
}
