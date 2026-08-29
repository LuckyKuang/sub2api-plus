package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	codexPrimaryWindowDuration   = 7 * 24 * time.Hour
	codexSecondaryWindowDuration = 5 * time.Hour
)

var codexDefaultQuotaHeaderNames = []string{
	"X-Codex-Primary-Used-Percent",
	"X-Codex-Primary-Reset-After-Seconds",
	"X-Codex-Primary-Reset-At",
	"X-Codex-Primary-Window-Minutes",
	"X-Codex-Secondary-Used-Percent",
	"X-Codex-Secondary-Reset-After-Seconds",
	"X-Codex-Secondary-Reset-At",
	"X-Codex-Secondary-Window-Minutes",
	"X-Codex-Primary-Over-Secondary-Limit-Percent",
}

type codexClientQuotaMode uint8

const (
	codexClientQuotaHidden codexClientQuotaMode = iota
	codexClientQuotaUpstream
	codexClientQuotaLocal
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

func (s *OpenAIGatewayService) codexClientQuotaMode(c *gin.Context, account *Account) codexClientQuotaMode {
	if c != nil && c.Request != nil && s.IsCodexLocalGroupQuotaEnabledForGroup(c.Request.Context(), groupFromRequestContext(c)) {
		return codexClientQuotaLocal
	}
	if account != nil && account.IsOpenAIPassthroughEnabled() {
		return codexClientQuotaUpstream
	}
	return codexClientQuotaHidden
}

func removeCodexDefaultQuotaHeaders(dst http.Header) {
	for _, key := range codexDefaultQuotaHeaderNames {
		dst.Del(key)
	}
}

// finalizeCodexClientQuotaHeaders applies the product-level quota visibility
// policy after generic response-header filtering.
func (s *OpenAIGatewayService) finalizeCodexClientQuotaHeaders(dst http.Header, c *gin.Context, account *Account) http.Header {
	mode := s.codexClientQuotaMode(c, account)
	if mode == codexClientQuotaUpstream {
		return dst
	}
	if dst == nil {
		if mode == codexClientQuotaHidden {
			return nil
		}
		dst = make(http.Header)
	}
	removeCodexDefaultQuotaHeaders(dst)
	if mode == codexClientQuotaLocal {
		applyCodexLocalQuotaHeaders(dst, groupFromRequestContext(c), codexLocalSubscriptionFromGin(c))
	}
	return dst
}

// finalizeCodexClientQuotaEvent applies the same policy to the default
// codex.rate_limits event family used by the official WebSocket client.
// Model-specific metered limit families remain untouched.
func (s *OpenAIGatewayService) finalizeCodexClientQuotaEvent(payload []byte, c *gin.Context, account *Account) ([]byte, bool) {
	if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "codex.rate_limits" || !isDefaultCodexRateLimitEvent(payload) {
		return payload, true
	}
	switch s.codexClientQuotaMode(c, account) {
	case codexClientQuotaUpstream:
		return payload, true
	case codexClientQuotaHidden:
		return nil, false
	case codexClientQuotaLocal:
		quota := BuildCodexLocalGroupQuotaUsage(groupFromRequestContext(c), codexLocalSubscriptionFromGin(c), time.Now())
		if quota == nil {
			return nil, false
		}
		event, err := json.Marshal(struct {
			Type             string                   `json:"type"`
			MeteredLimitName string                   `json:"metered_limit_name"`
			RateLimits       codexLocalRateLimitEvent `json:"rate_limits"`
		}{
			Type:             "codex.rate_limits",
			MeteredLimitName: "codex",
			RateLimits: codexLocalRateLimitEvent{
				Primary:   codexLocalRateLimitEventWindowFromQuota(quota.RateLimit.PrimaryWindow),
				Secondary: codexLocalRateLimitEventWindowFromQuota(quota.RateLimit.SecondaryWindow),
			},
		})
		if err != nil {
			return nil, false
		}
		return event, true
	default:
		return nil, false
	}
}

type codexLocalRateLimitEvent struct {
	Primary   *codexLocalRateLimitEventWindow `json:"primary"`
	Secondary *codexLocalRateLimitEventWindow `json:"secondary"`
}

type codexLocalRateLimitEventWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetAt       int64   `json:"reset_at"`
}

func codexLocalRateLimitEventWindowFromQuota(window *CodexLocalQuotaWindow) *codexLocalRateLimitEventWindow {
	if window == nil {
		return nil
	}
	return &codexLocalRateLimitEventWindow{
		UsedPercent:   window.UsedPercent,
		WindowMinutes: window.LimitWindowSeconds / 60,
		ResetAt:       window.ResetAt,
	}
}

func isDefaultCodexRateLimitEvent(payload []byte) bool {
	limitName := strings.TrimSpace(gjson.GetBytes(payload, "metered_limit_name").String())
	if limitName == "" {
		limitName = strings.TrimSpace(gjson.GetBytes(payload, "limit_name").String())
	}
	if limitName == "" {
		return true
	}
	normalized := strings.ReplaceAll(strings.ToLower(limitName), "_", "-")
	return normalized == "codex"
}
