package service

import (
	"regexp"
	"strings"
)

// codexUpstreamMinVersion 上游 /backend-api/codex 接受的最低 version 头：
// 若请求携带 version 且低于该值，上游直接 404（issue #3901，2026-07 实测）。
const codexUpstreamMinVersion = "0.144.0"

// OpenAICodexUpstreamMinVersion exposes the shared lower bound to outbound
// adapters that validate an already-resolved identity triple.
const OpenAICodexUpstreamMinVersion = codexUpstreamMinVersion

// codexClientVersionMaxLen 官方版本号均为短 ASCII 串，远低于此上限。
const codexClientVersionMaxLen = 64

// codexClientVersionPattern 允许 0.147.0 与 0.148.0-alpha.4 两类官方形态。
var codexClientVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z.]+)?$`)

// NormalizeCodexClientVersion 校验并归一化 Codex 客户端版本号，非法值返回空串。
// 该值会被拼进出站 User-Agent 与 version 头，必须拒绝任意字节，避免管理员误填或
// 自动同步拿到异常值时把不可控内容透给上游。
func NormalizeCodexClientVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > codexClientVersionMaxLen || !codexClientVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

// normalizeStableCodexClientVersion accepts only release versions suitable for
// the automatic synchronization setting. Explicit administrator overrides may
// still select a prerelease through NormalizeCodexClientVersion.
func normalizeStableCodexClientVersion(version string) string {
	version = NormalizeCodexClientVersion(version)
	if version == "" || strings.Contains(version, "-") {
		return ""
	}
	return version
}
