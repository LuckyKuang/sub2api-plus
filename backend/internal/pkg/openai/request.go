package openai

import (
	"regexp"
	"strings"
)

// CodexCLIUserAgentPrefixes matches Codex CLI User-Agent patterns
// Examples: "codex-tui/1.0.0", "codex_vscode/1.0.0", "codex_cli_rs/0.1.2"
var CodexCLIUserAgentPrefixes = []string{
	"codex-tui/",
	"codex_vscode/",
	"codex_cli_rs/",
}

// IsCodexCLIRequest checks if the User-Agent indicates a Codex CLI request
func IsCodexCLIRequest(userAgent string) bool {
	ua := normalizeCodexClientHeader(userAgent)
	if ua == "" {
		return false
	}
	return matchCodexClientHeaderPrefixes(ua, CodexCLIUserAgentPrefixes)
}

// IsCodexOfficialClientRequest recognizes only a complete, current official
// User-Agent. It intentionally does not recognize the legacy compatibility
// aliases: callers that need them must receive an explicit policy decision.
func IsCodexOfficialClientRequest(userAgent string) bool {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return false
	}
	_, ok := ClassifyOfficialCodexClientProfile(ua, strings.TrimSpace(ua[:slash]))
	return ok
}

// IsCodexOfficialClientRequestStrict is retained for callers compiled against
// the old API. The old and strict forms now have the same exact behavior.
func IsCodexOfficialClientRequestStrict(userAgent string) bool {
	return IsCodexOfficialClientRequest(userAgent)
}

// codexUATrailerName extracts the name from a trailing `(name; version)` UA
// group. It is retained for diagnostics and UA version synchronization only;
// it is not an identity source and must not be used to recover an Originator.
func codexUATrailerName(ua string) string {
	last := strings.LastIndex(ua, "(")
	if last < 0 {
		return ""
	}
	rest := ua[last+1:]
	closeIdx := strings.Index(rest, ")")
	if closeIdx < 0 {
		return ""
	}
	inner := strings.TrimSpace(rest[:closeIdx])
	if semi := strings.Index(inner, ";"); semi >= 0 {
		inner = strings.TrimSpace(inner[:semi])
	}
	return inner
}

// IsCodexOfficialClientOriginator checks the reviewed current-official
// registry without lowercasing a caller-controlled identity.
func IsCodexOfficialClientOriginator(originator string) bool {
	originator = strings.TrimSpace(originator)
	if !isSaneCodexOriginator(originator) {
		return false
	}
	if _, ok := codexBuiltInProfileByOriginator[originator]; ok {
		return true
	}
	return strings.HasPrefix(originator, "Codex ") && isSaneCodexProductFamilyName(originator)
}

// IsCodexOfficialClientByHeaders checks whether the request headers indicate an
// exact, coherent current-official Codex client profile.
func IsCodexOfficialClientByHeaders(userAgent, originator string) bool {
	_, ok := ClassifyOfficialCodexClientProfile(userAgent, originator)
	return ok
}

func normalizeCodexClientHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchCodexClientHeaderPrefixes(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		normalizedPrefix := normalizeCodexClientHeader(prefix)
		if normalizedPrefix == "" {
			continue
		}
		// 优先前缀匹配；若 UA/Originator 被网关拼接为复合字符串时，退化为包含匹配。
		if strings.HasPrefix(value, normalizedPrefix) || strings.Contains(value, normalizedPrefix) {
			return true
		}
	}
	return false
}

// PairCodexClientIdentity is retained for older internal callers. It is now
// strict-current-only; new code must use PairConfiguredCodexClientIdentity and
// explicitly pass the legacy compatibility policy.
func PairCodexClientIdentity(userAgent string) (originator string, pairedUA string, ok bool) {
	profile, ua, ok := PairConfiguredCodexClientIdentity(userAgent, false)
	if !ok {
		return "", "", false
	}
	return profile.Originator, ua, true
}

// codexOriginatorMaxLen 官方 clientInfo.name 均为短 ASCII 标识，远低于此上限。
const codexOriginatorMaxLen = 64

// isSaneCodexOriginator 拒绝超长或含不可打印/非 ASCII 字节的候选 originator，
// 避免 `Codex ` 家族宽前缀把客户端可控的任意字节当作官方身份逐字转发给上游。
func isSaneCodexOriginator(name string) bool {
	if name == "" || len(name) > codexOriginatorMaxLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// CodexCLIOriginator 是 codex-rs 客户端的历史默认 originator，保留用于兼容识别。
const CodexCLIOriginator = "codex_cli_rs"

// CodexDefaultOriginator 是网关默认使用的 Codex TUI originator。
const CodexDefaultOriginator = "codex-tui"

// CodexUserAgentVersion 提取 Codex UA 的完整版本段，即 `{client}/{version} (...` 中的 version。
// 与 ParseCodexEngineVersion 的区别：后者只取三段数字用于引擎版本比较（会丢掉 -alpha.4
// 之类的预发布后缀），本函数保留原样，因为出站 version 头必须与 UA 版本段逐字一致。
// 取不到（非 Codex 形态 UA）时返回空串。
func CodexUserAgentVersion(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return ""
	}
	rest := ua[slash+1:]
	if space := strings.IndexByte(rest, ' '); space >= 0 {
		rest = rest[:space]
	}
	return strings.TrimSpace(rest)
}

// SetCodexUserAgentVersion 用 version 重建 Codex 形态 UA 中的版本声明，其余部分
// （客户端名、OS / 架构 / 终端指纹）原样保留；UA 不是 `{client}/{version}` 形态时返回空串，
// 由调用方决定整体回退。
//
// 当尾部 `(name; version)` 声明与首段为同一个已认可客户端身份时，版本同步会一并更新；
// 这包括显式启用的旧版兼容身份。尾部绝不会用于恢复或改变首段身份，且不一致的尾部保持
// 原样，避免误伤 OS 组（如 `(Ubuntu 22.4.0; x86_64)`）。
func SetCodexUserAgentVersion(userAgent, version string) string {
	ua := strings.TrimSpace(userAgent)
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return ""
	}
	client := strings.TrimSpace(ua[:slash])
	if client == "" {
		return ""
	}
	rest := ua[slash+1:]
	tail := ""
	if space := strings.IndexByte(rest, ' '); space >= 0 {
		tail = rest[space:]
	} else if strings.TrimSpace(rest) == "" {
		// `client/` 没有版本段，不是可重建的 Codex 形态。
		return ""
	}
	return rewriteCodexUATrailerVersion(client+"/"+version+tail, version)
}

// rewriteCodexUATrailerVersion updates a trailing identity declaration only
// when it exactly agrees with the leading configured profile. It never uses a
// trailer to recover, change, or bless the leading identity.
func rewriteCodexUATrailerVersion(ua, version string) string {
	open := strings.LastIndex(ua, "(")
	if open < 0 {
		return ua
	}
	closeIdx := strings.Index(ua[open+1:], ")")
	if closeIdx < 0 {
		return ua
	}
	inner := ua[open+1 : open+1+closeIdx]
	semi := strings.Index(inner, ";")
	if semi < 0 {
		return ua
	}
	name := strings.TrimSpace(inner[:semi])
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 || name == "" || name != strings.TrimSpace(ua[:slash]) {
		return ua
	}
	if _, _, ok := PairConfiguredCodexClientIdentity(name+"/"+version, true); !ok {
		return ua
	}
	return ua[:open+1] + name + "; " + version + ua[open+1+closeIdx:]
}

// codexEngineVersionPattern 提取版本段开头的三段数字 X.Y.Z（忽略 -alpha 等后缀）。
var codexEngineVersionPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// ParseCodexEngineVersion 从 codex-rs 形态 UA 取引擎版本：
// `{originator}/{X.Y.Z} (...)`，第一个 '/' 后、首个空格或 '(' 前的三段版本。
// 该版本是 codex-rs CARGO_PKG_VERSION（引擎版本，CLI/app-server 一致）。
func ParseCodexEngineVersion(ua string) (string, bool) {
	ua = strings.TrimSpace(ua)
	slash := strings.IndexByte(ua, '/')
	if slash < 0 {
		return "", false
	}
	rest := ua[slash+1:]
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' || rest[i] == '(' {
			end = i
			break
		}
	}
	m := codexEngineVersionPattern.FindString(strings.TrimSpace(rest[:end]))
	if m == "" {
		return "", false
	}
	return m, true
}
