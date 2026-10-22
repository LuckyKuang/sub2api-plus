package service

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/tidwall/gjson"
)

// 国产供应商（kimi/zhipu/deepseek）的响应式冷却辅助。
//
// 与 openai/anthropic 不同：
//   - 余额不足是「可恢复」状态（充值/检测恢复后自动重新调度），不能走 handleAuthError
//     永久置 status=error。这里改为 SetTempUnschedulable，由 CN 余额检测周期任务
//     （cn_provider_balance_check_service.go）在余额恢复后 ClearTempUnschedulable。
//   - Coding Plan 滚动窗口耗尽（429）的冷却终点应是真实的窗口重置时间（已由
//     CNProviderQuotaService 落入 account.Extra 快照），而非默认的秒级兜底。

// cnBalanceExtraSuffixLow 标记账号响应过「余额不足」，供余额检测任务区分
// 「确属余额不足」与「尚未探测」。
const cnBalanceExtraSuffixLow = "balance_low"

// cnBalanceLowReasonPrefix 是余额不足临时停调 reason 的稳定前缀。
// 周期余额检测任务据此识别「是我们停调的」并在余额恢复后安全清除——不会误清
// 其他子系统（阈值/限流/401）写入的临时停调。
const cnBalanceLowReasonPrefix = "cn_balance_low"

const kimiConcurrentRequestLimitMessage = "You've reached your concurrent request limit. Please wait for your ongoing requests to finish and try again."

const cnConcurrencyLimitReasonPrefix = "cn_concurrency_limit"

func isCNProviderConcurrencyLimit403(account *Account, upstreamMsg string) bool {
	return account != nil && account.Platform == PlatformKimi &&
		strings.TrimSpace(upstreamMsg) == kimiConcurrentRequestLimitMessage
}

// cnQuotaExhausted403ErrorType 是 Kimi Coding Plan 配额窗口耗尽的 403 错误类型。
// 与并发限制 403（见 isCNProviderConcurrencyLimit403）不同，这是 5h/weekly
// 窗口耗尽的限流信号：窗口到期后自动恢复，绝不能落入通用 403 升级计数
// （连续 3 次永久 SetError），必须按 429 口径冷却到真实窗口重置点。
const cnQuotaExhausted403ErrorType = "access_terminated_error"

// cnQuotaExhaustedReasonPrefix 是配额耗尽 403 临时停调 reason 的稳定前缀。
const cnQuotaExhaustedReasonPrefix = "cn_quota_exhausted"

// isCNProviderQuotaExhausted403 识别 CN 供应商配额窗口耗尽的 403。
// 先做廉价的文案子串匹配（usage limit / quota will reset），未命中再解析
// 响应体匹配结构化错误类型（Kimi 的 access_terminated_error），以兼容同族
// 供应商的文案变体。与并发限制文案互斥，两类信号不会误判。
// 仅限 Coding Plan 账号：滚动窗口快照（含重置时间）只有 Coding Plan 账号才有，
// 非 Coding Plan 账号的同类 403 语义不明（可能是需要升级套餐的硬上限），
// 应回退到通用 403 升级逻辑而非无限临时冷却。
func isCNProviderQuotaExhausted403(account *Account, responseBody []byte, upstreamMsg string) bool {
	if account == nil || !account.IsCNProvider() || !account.IsCodingPlan() {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(upstreamMsg))
	if strings.Contains(msg, "usage limit") || strings.Contains(msg, "quota will reset") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(gjson.GetBytes(responseBody, "error.type").String()), cnQuotaExhausted403ErrorType)
}

// handleCNProviderQuotaExhausted403 把 CN 供应商配额窗口耗尽的 403 按 429
// 同口径处理：有配额快照时冷却到最早的未来窗口重置点（SetRateLimited，
// 窗口过期后调度自动恢复）；快照缺失（额度探测尚未刷新）时兜底默认 403
// 冷却时长的临时停调，避免过度停调到数天后的窗口。无论哪条路径都不写
// 账号 error 状态。
func (s *RateLimitService) handleCNProviderQuotaExhausted403(
	ctx context.Context,
	account *Account,
	upstreamMsg string,
) {
	if s.cooldownCNProviderToQuotaSnapshotReset(ctx, account, cnQuotaExhaustedReasonPrefix, "cn_quota_exhausted_rate_limited") != nil {
		return
	}

	// upstreamMsg 由调用方（HandleUpstreamError）从同一响应体提取，此处直接复用；
	// 为空时仅存前缀，保证 reason 稳定可检索。
	reason := cnQuotaExhaustedReasonPrefix
	if msg := strings.TrimSpace(upstreamMsg); msg != "" {
		reason += ": " + msg
	}
	until := time.Now().Add(time.Duration(openAI403CooldownMinutesDefault) * time.Minute)
	s.setCNProviderTempUnschedulable(ctx, account, until, cnQuotaExhaustedReasonPrefix, reason, "cn_quota_exhausted_temp_unschedulable")
}

func (s *RateLimitService) handleCNProviderConcurrencyLimit403(
	ctx context.Context,
	account *Account,
) {
	until := time.Now().Add(time.Duration(openAI403CooldownMinutesDefault) * time.Minute)
	s.setCNProviderTempUnschedulable(ctx, account, until,
		cnConcurrencyLimitReasonPrefix, cnConcurrencyLimitReasonPrefix+": "+kimiConcurrentRequestLimitMessage,
		"cn_provider_concurrency_limited")
}

// cnBalanceLowReason 构造余额不足临时停调的 reason（带稳定前缀）。
func cnBalanceLowReason(upstreamMsg string) string {
	if upstreamMsg = strings.TrimSpace(upstreamMsg); upstreamMsg != "" {
		return cnBalanceLowReasonPrefix + ": " + upstreamMsg
	}
	return cnBalanceLowReasonPrefix + ": 余额不足，账号临时停调"
}

// cnProviderResponseIndicatesInsufficientBalance 通过响应体文案识别余额不足
// （智谱 payg 无独立余额端点，仅能靠响应文案识别）。
func cnProviderResponseIndicatesInsufficientBalance(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	s := strings.ToLower(string(body))
	return strings.Contains(s, "余额不足") ||
		strings.Contains(s, "insufficient balance") ||
		strings.Contains(s, "insufficient_credit") ||
		strings.Contains(s, "balance is not enough") ||
		strings.Contains(s, "no enough balance")
}

// MiniMax Code's llm-error-classifier.ts distinguishes Token Plan exhaustion
// (2056/2067) from Credits exhaustion (1400010161), including HTTP 500 wrappers.
// Inspect only provider-defined status fields, never incidental message digits.
func (s *RateLimitService) handleMiniMaxUsageError(ctx context.Context, account *Account, status int, body []byte, message string) bool {
	if account == nil || account.Platform != PlatformMiniMax || status < 400 || status == http.StatusUnauthorized {
		return false
	}
	var code int64
	for _, path := range []string{"status_code", "base_resp.status_code", "error.status_code"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.Number {
			switch value.Float() {
			case 2056, 2067, 1400010161:
				code = value.Int()
			}
		}
		if code != 0 {
			break
		}
	}
	if code == 0 {
		return false
	}
	// Native OAuth may have refreshed after the outer handler selected an
	// account. Cool down only the grant that actually produced this response.
	owner := account
	if requestCredentialOwnerBound(ctx, account) {
		frozen, ok := requestCredentialOwner(ctx, account)
		if !ok || frozen.ID != account.ID || frozen.Platform != PlatformMiniMax {
			slog.Warn("minimax_usage_owner_unavailable", "account_id", account.ID, "code", code)
			return true
		}
		owner = frozen
	}
	now := time.Now()
	until := now.Add(time.Duration(openAI403CooldownMinutesDefault) * time.Minute)
	reason := cnQuotaExhaustedReasonPrefix
	if code == 1400010161 {
		until, reason = now.Add(s.cnBalanceCooldownDuration()), cnBalanceLowReasonPrefix
	} else if reset := cnProviderQuotaSnapshotReset(owner, now); reset != nil {
		until = *reset
	}
	notifyReason := reason
	if message != "" {
		reason += ": " + message
	}
	repo, ok := s.accountRepo.(credentialCooldownRepository)
	if !ok {
		slog.Warn("minimax_usage_cooldown_unavailable", "account_id", owner.ID, "code", code)
		return true
	}
	applied, err := repo.SetCredentialTempUnschedulableIfUnchanged(ctx, owner, until, reason)
	if err != nil {
		slog.Warn("minimax_usage_cooldown_failed", "account_id", owner.ID, "code", code, "error", err)
	} else if applied {
		s.notifyAccountSchedulingBlocked(owner, until, notifyReason)
	}
	return true
}

// handleCNProviderInsufficientBalance 把余额不足标记为可恢复的临时停调：
// 写入 balance_low 快照 + SetTempUnschedulable 一个余额检测周期，
// 由周期任务在余额恢复后清除。返回前已通知调度阻塞。
func (s *RateLimitService) handleCNProviderInsufficientBalance(
	ctx context.Context,
	account *Account,
	upstreamMsg string,
) {
	msg := cnBalanceLowReason(upstreamMsg)
	until := time.Now().Add(s.cnBalanceCooldownDuration())
	if !s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{
		Extra:                  map[string]any{cnExtraKey(account.Platform, cnBalanceExtraSuffixLow): true},
		TempUnschedulableUntil: &until, TempUnschedulableReason: msg,
	}, until, "cn_insufficient_balance") {
		return
	}
	slog.Info("cn_provider_insufficient_balance",
		"account_id", account.ID,
		"platform", account.Platform,
		"until", until.UTC(),
	)
}

// cnBalanceCooldownDuration 返回余额不足临时停调的持续时长（= 2× 余额检测周期，
// 默认 20 分钟）。周期任务会在余额恢复后提前清除，故此处只需保证冷却覆盖到下一次
// 周期检测即可。
func (s *RateLimitService) cnBalanceCooldownDuration() time.Duration {
	if s == nil {
		return cnBalanceCheckCooldown(nil)
	}
	return cnBalanceCheckCooldown(s.cfg)
}

// cnBalanceCheckCooldown 返回余额类冷却时长（= 2× 余额检测周期，默认周期 10 分钟）。
func cnBalanceCheckCooldown(cfg *config.Config) time.Duration {
	minutes := 10
	if cfg != nil {
		if cfgMin := cfg.Gateway.CNProviders.BalanceCheckIntervalMinutes; cfgMin > 0 {
			minutes = cfgMin
		}
	}
	cooldown := time.Duration(minutes) * time.Minute * 2
	if cooldown < time.Minute {
		cooldown = 10 * time.Minute
	}
	return cooldown
}

// cnProviderQuotaSnapshotReset 读取 Coding Plan 账号快照中最早一个仍在未来的窗口
// 重置时间（5h / weekly）。429 多数由 5h 滚动窗口触发，取较早的重置点可避免
// 把账号冷却到 weekly 重置（可达数天）的过度停调；如果确是 weekly 窗口耗尽，
// 周期额度探测刷新快照后阈值评估会再次停调到正确的时间点。
// 无快照或均已过期返回 nil。
func cnProviderQuotaSnapshotReset(account *Account, now time.Time) *time.Time {
	if account == nil || len(account.Extra) == 0 {
		return nil
	}
	if !cnQuotaSnapshotMatches(account) {
		return nil
	}
	switch {
	case account.IsOpenCodeGo():
	case account.IsCommandCode():
		// 有充值积分时上游跳过窗口检查，429 与窗口无关。
		if !commandCodeWindowsBinding(account.Extra) {
			return nil
		}
	case account.IsCNProvider() && account.IsCodingPlan():
	default:
		return nil
	}
	provider := account.Platform
	suffixes := []string{cnExtraSuffix5hReset, cnExtraSuffixWeeklyReset}
	if cnQuotaHasMonthlyWindow(provider) {
		suffixes = append(suffixes, cnExtraSuffixMonthlyReset)
	}
	if provider == PlatformKimi {
		suffixes = append(suffixes, cnExtraSuffixMonthlyCodeReset)
	}
	var earliest *time.Time
	for _, suffix := range suffixes {
		window := strings.TrimSuffix(suffix, "_reset_at")
		if unlimited, _ := account.Extra[cnExtraKey(provider, window+"_unlimited")].(bool); unlimited {
			continue
		}
		t := parseSchedulingResetAt(account.Extra[cnExtraKey(provider, suffix)])
		if t == nil || !t.After(now) {
			continue
		}
		if earliest == nil || t.Before(*earliest) {
			earliest = t
		}
	}
	return earliest
}

// cooldownCNProviderToQuotaSnapshotReset 把账号冷却到配额快照中最早的未来窗口
// 重置点（SetRateLimited，窗口过期后调度自动恢复），供 429（OpenCodeGo /
// Coding Plan）与配额耗尽 403 共用。仅在冷却成功持久化后返回非 nil；
// 无快照或写库失败时返回 nil，由调用方落入各自的兜底逻辑（避免写库失败
// 时账号完全失去持久冷却）。
func (s *RateLimitService) cooldownCNProviderToQuotaSnapshotReset(ctx context.Context, account *Account, reason, logEvent string) *time.Time {
	owner, ok := credentialStateMutationOwner(ctx, account)
	if !ok {
		return nil
	}
	until := cnProviderQuotaSnapshotReset(owner, time.Now())
	if until == nil {
		return nil
	}
	if !s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{RateLimitUntil: until, ExtendRateLimitOnly: true}, *until, reason) {
		return nil
	}
	slog.Info(logEvent,
		"account_id", account.ID,
		"platform", account.Platform,
		"reset_at", until.UTC(),
	)
	return until
}

// setCNProviderTempUnschedulable 写临时停调（含调度层通知与日志），供 CN 403
// 各分支（并发限制、配额耗尽兜底）共用。告警 key 由 notifyReason 派生，保持
// 各分支既有日志 key 稳定（<prefix>_set_temp_unschedulable_failed）。
func (s *RateLimitService) setCNProviderTempUnschedulable(ctx context.Context, account *Account, until time.Time, notifyReason, storeReason, logEvent string) {
	if !s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{TempUnschedulableUntil: &until, TempUnschedulableReason: storeReason}, until, notifyReason) {
		return
	}
	slog.Info(logEvent,
		"account_id", account.ID,
		"platform", account.Platform,
		"until", until.UTC(),
	)
}

func (s *RateLimitService) applyCNProviderCredentialState(ctx context.Context, account *Account, mutation CredentialStateMutation, until time.Time, reason string) bool {
	owner, known := credentialStateMutationOwner(ctx, account)
	if !known {
		slog.Warn("cn_credential_snapshot_unavailable", "account_id", account.ID)
		return false
	}
	repo, ok := s.accountRepo.(CredentialStateMutationRepository)
	if !ok {
		slog.Warn("cn_credential_state_repository_unavailable", "account_id", owner.ID)
		return false
	}
	applied, err := repo.ApplyCredentialStateMutation(ctx, owner, mutation)
	if err != nil {
		slog.Warn("cn_credential_state_mutation_failed", "account_id", owner.ID, "error", err)
		return false
	}
	if applied {
		s.notifyAccountSchedulingBlocked(owner, until, reason)
	}
	return applied
}

// applyCNProviderReactive429 处理国产供应商的 429 响应。
// 返回 true 表示已处理（调用方应 return），false 表示未命中、继续走默认 429 逻辑。
func (s *RateLimitService) applyCNProviderReactive429(
	ctx context.Context,
	account *Account,
	headers http.Header,
	responseBody []byte,
) bool {
	if account.IsOpenCodeGo() {
		if s.cooldownCNProviderToQuotaSnapshotReset(ctx, account, "429", "opencode_go_rate_limited") != nil {
			return true
		}
		if resetAt := parseOpenAIRateLimitResetTime(responseBody); resetAt != nil {
			resetTime := time.Unix(*resetAt, 0)
			if !s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{RateLimitUntil: &resetTime, ExtendRateLimitOnly: true}, resetTime, "429") {
				return true
			}
			slog.Info("opencode_go_rate_limited",
				"account_id", account.ID,
				"platform", account.Platform,
				"reset_at", resetTime,
			)
			return true
		}
		return false
	}
	if !account.IsCNProvider() {
		return false
	}
	// 1) 余额不足文案：可恢复临时停调（含智谱 payg 这类无余额端点的场景）。
	if cnProviderResponseIndicatesInsufficientBalance(responseBody) {
		s.handleCNProviderInsufficientBalance(ctx, account, extractUpstreamErrorMessage(responseBody))
		return true
	}
	// 2) Coding Plan 窗口耗尽：冷却到快照中最早的窗口重置点（见
	// cnProviderQuotaSnapshotReset：429 多由 5h 窗口触发，取较早点避免过度停调）。
	if account.IsCodingPlan() &&
		s.cooldownCNProviderToQuotaSnapshotReset(ctx, account, "429", "cn_coding_plan_rate_limited") != nil {
		return true
	}
	// Keep the existing compatible-endpoint reset sources, but fence every CN
	// fallback as well: falling through to the generic writer loses the grant.
	if result := calculateAnthropic429ResetTime(headers); result != nil {
		s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{RateLimitUntil: &result.resetAt, ExtendRateLimitOnly: true}, result.resetAt, "429")
		return true
	}
	if ts, err := strconv.ParseInt(headers.Get("anthropic-ratelimit-unified-reset"), 10, 64); err == nil {
		resetAt := time.Unix(ts, 0)
		s.applyCNProviderCredentialState(ctx, account, CredentialStateMutation{RateLimitUntil: &resetAt, ExtendRateLimitOnly: true}, resetAt, "429")
		return true
	}
	s.apply429FallbackRateLimit(ctx, account, "no_reset_time")
	return true
}
