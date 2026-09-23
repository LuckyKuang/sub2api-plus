package service

import (
	"context"
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

// parseCodexRateLimitEventSnapshot 把官方带内 `codex.rate_limits` 事件解析成与
// HTTP 响应头同一形态的 OpenAICodexUsageSnapshot，对齐官方
// codex-api/src/rate_limits.rs 的 parse_rate_limit_event。
//
// WebSocket 是这个信号的唯一来源：101 握手只带 reasoning-included / server-model /
// turn-state，官方客户端完全靠带内事件更新配额。Plus 此前只在 HTTP 路径从响应头
// 建快照，导致 WS 轮不刷新账号配额视图。
//
// 只处理默认 `codex` 族：附加族由本地分组配额合成单独处理，避免把模型级信号写进
// 账号级 5h/7d 视图。
func parseCodexRateLimitEventSnapshot(payload []byte) *OpenAICodexUsageSnapshot {
	if !gjson.ValidBytes(payload) || !isDefaultCodexRateLimitEvent(payload) {
		return nil
	}
	rateLimits := gjson.GetBytes(payload, "rate_limits")
	if !rateLimits.Exists() {
		return nil
	}

	snapshot := &OpenAICodexUsageSnapshot{}
	hasData := false

	// Official RateLimitEventWindow: used_percent (required), window_minutes,
	// reset_at. A window with no parseable used_percent is dropped, matching
	// parse_rate_limit_window's `used_percent.and_then(...)`.
	if window := rateLimits.Get("primary"); window.Exists() {
		if used := eventWindowUsedPercent(window); used != nil {
			snapshot.PrimaryUsedPercent = used
			snapshot.PrimaryWindowMinutes = eventWindowMinutes(window)
			snapshot.PrimaryResetAtUnix = eventWindowResetAt(window)
			hasData = true
		}
	}
	if window := rateLimits.Get("secondary"); window.Exists() {
		if used := eventWindowUsedPercent(window); used != nil {
			snapshot.SecondaryUsedPercent = used
			snapshot.SecondaryWindowMinutes = eventWindowMinutes(window)
			snapshot.SecondaryResetAtUnix = eventWindowResetAt(window)
			hasData = true
		}
	}

	// Official RateLimitEventCredits: has_credits and unlimited are both
	// required, balance is optional. Mirrors parse_credits_snapshot.
	if hasCredits := eventWindowBool(gjson.GetBytes(payload, "credits.has_credits")); hasCredits != nil {
		if unlimited := eventWindowBool(gjson.GetBytes(payload, "credits.unlimited")); unlimited != nil {
			snapshot.CreditsHasCredits = hasCredits
			snapshot.CreditsUnlimited = unlimited
			if balance := strings.TrimSpace(gjson.GetBytes(payload, "credits.balance").String()); balance != "" {
				snapshot.CreditsBalance = balance
			}
			hasData = true
		}
	}

	if !hasData {
		return nil
	}
	return snapshot
}

func eventWindowUsedPercent(window gjson.Result) *float64 {
	value := window.Get("used_percent")
	if value.Type != gjson.Number {
		return nil
	}
	parsed := value.Float()
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func eventWindowMinutes(window gjson.Result) *int {
	value := window.Get("window_minutes")
	if value.Type != gjson.Number {
		return nil
	}
	parsed := int(value.Float())
	if parsed <= 0 {
		return nil
	}
	return &parsed
}

// eventWindowResetAt keeps the same calendar bound as the header path
// (validOpenAIQuotaResetUnix) so a malformed event value cannot park an account.
func eventWindowResetAt(window gjson.Result) *int64 {
	value := window.Get("reset_at")
	if value.Type != gjson.Number {
		return nil
	}
	parsed := int64(value.Float())
	if !validOpenAIQuotaResetUnix(parsed) {
		return nil
	}
	return &parsed
}

func eventWindowBool(value gjson.Result) *bool {
	switch strings.ToLower(strings.TrimSpace(value.String())) {
	case "true", "1":
		result := true
		return &result
	case "false", "0":
		result := false
		return &result
	default:
		return nil
	}
}

// observeOpenAICodexRateLimitEventSnapshot refreshes the account Codex usage
// snapshot from an in-band `codex.rate_limits` event, so a WebSocket turn keeps
// the same quota view an HTTP turn builds from response headers.
func (s *OpenAIGatewayService) observeOpenAICodexRateLimitEventSnapshot(ctx context.Context, account *Account, payload []byte) {
	if s == nil || account == nil {
		return
	}
	snapshot := parseCodexRateLimitEventSnapshot(payload)
	if snapshot == nil {
		return
	}
	s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
}
