package openai

import "strings"

// AllowedClientEntry describes an administrator-approved, non-official
// compatibility profile. It is never reported as an official Codex profile.
// Originator must agree with the leading User-Agent identity (after normalizing
// letter case for an explicitly configured third-party entry).
// UAContains 为必填字段：列表为空，或列表中存在任何空白 marker，均视为非法配置，
// 整体安全失败（return false）；每一项都必须出现在 User-Agent 中。
// This prevents an allow rule from degrading to an Originator-only check.
type AllowedClientEntry struct {
	Originator            string   `json:"originator"`
	UAContains            []string `json:"ua_contains"`
	SkipEngineFingerprint bool     `json:"skip_engine_fingerprint"`
}

// IsWhitelistable 报告该条目作为白名单条目是否「有可能命中」——镜像 IsAllowedClientMatch 的结构性
// 前置：originator 非空、ua_contains 至少一项、且无任何空白 marker（空白 marker 会让整条永不命中）。
// 仅供管理端写入校验，避免存入静默失效的白名单规则。黑名单（OR 宽 deny，允许 originator-only）不受此约束。
func (e AllowedClientEntry) IsWhitelistable() bool {
	if normalizeCodexClientHeader(e.Originator) == "" {
		return false
	}
	if len(e.UAContains) == 0 {
		return false
	}
	for _, marker := range e.UAContains {
		if normalizeCodexClientHeader(marker) == "" {
			return false
		}
	}
	return true
}

// IsAllowedClientMatch 判断请求头是否命中给定的额外客户端签名。
// originator 必须精确等值（归一化后）；UAContains 中每一项都必须出现在 UA 中。
// UAContains 为必填：列表为空或含任何空白 marker 均视为非法配置，整体安全失败。
func IsAllowedClientMatch(userAgent, originator string, entry AllowedClientEntry) bool {
	wantOriginator := normalizeCodexClientHeader(entry.Originator)
	if wantOriginator == "" {
		return false
	}
	if normalizeCodexClientHeader(originator) != wantOriginator {
		return false
	}
	if !HasCoherentConfiguredClientIdentity(userAgent, originator) {
		return false
	}
	// 预设必须声明 UA 特征：否则将退化为仅凭可伪造的 originator 单因子匹配。
	if len(entry.UAContains) == 0 {
		return false
	}
	ua := normalizeCodexClientHeader(userAgent)
	for _, marker := range entry.UAContains {
		normalizedMarker := normalizeCodexClientHeader(marker)
		if normalizedMarker == "" {
			// 空白 marker 让该项失去校验能力，会让双因子退化为仅 originator
			// 单因子；视为非法配置，安全失败。
			return false
		}
		if !strings.Contains(ua, normalizedMarker) {
			return false
		}
	}
	return true
}

// MatchClientEntry is the MatchClientEntries variant that also returns the
// matched explicit compatibility entry. It never changes official-profile
// classification.
func MatchClientEntry(userAgent, originator string, entries []AllowedClientEntry) (AllowedClientEntry, bool) {
	for _, e := range entries {
		if IsAllowedClientMatch(userAgent, originator, e) {
			return e, true
		}
	}
	return AllowedClientEntry{}, false
}

// MatchClientEntries 判断请求头是否命中任一白名单自由条目（双因子 AND）。薄封装 MatchClientEntry。
// 用于 codex_cli_only 全局白名单：放行经管理员审核的兼容客户端。
func MatchClientEntries(userAgent, originator string, entries []AllowedClientEntry) bool {
	_, ok := MatchClientEntry(userAgent, originator, entries)
	return ok
}

// IsDeniedClientMatch 黑名单单条 OR 语义：已声明字段中任一命中即 deny。
// originator 精确等值命中，或任一非空 ua_contains marker 出现在 UA 中。
// 全空字段（originator 与 ua_contains 均空）→ 不 deny（安全忽略）。
// 与白名单 AND 非对称：deny 应宽（挡可疑），allow 应严（防伪造）。
func IsDeniedClientMatch(userAgent, originator string, entry AllowedClientEntry) bool {
	if want := normalizeCodexClientHeader(entry.Originator); want != "" {
		if normalizeCodexClientHeader(originator) == want {
			return true
		}
	}
	ua := normalizeCodexClientHeader(userAgent)
	for _, marker := range entry.UAContains {
		if m := normalizeCodexClientHeader(marker); m != "" && strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// MatchDenyEntries 判断请求头是否命中任一黑名单条目（OR）。
func MatchDenyEntries(userAgent, originator string, entries []AllowedClientEntry) bool {
	for _, e := range entries {
		if IsDeniedClientMatch(userAgent, originator, e) {
			return true
		}
	}
	return false
}
