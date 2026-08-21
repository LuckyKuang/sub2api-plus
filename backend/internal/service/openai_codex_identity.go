package service

import (
	"regexp"
	"strings"
)

// codexUpstreamMinVersion 上游 /backend-api/codex 接受的最低 version 头：
// 若请求携带 version 且低于该值，上游直接 404（issue #3901，2026-07 实测）。
const codexUpstreamMinVersion = "0.144.0"

// OpenAICodexUpstreamMinVersion is the public validation floor used by admin
// settings and OAuth credential probes.
const OpenAICodexUpstreamMinVersion = codexUpstreamMinVersion

// codexClientVersionMaxLen 官方版本号均为短 ASCII 串，远低于此上限。
const codexClientVersionMaxLen = 64

// codexClientVersionPattern 允许 0.146.0 与 0.147.0-alpha.4 两类官方形态。
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
// automatic synchronization. Explicit administrator overrides may still select
// another valid version through NormalizeCodexClientVersion.
func normalizeStableCodexClientVersion(version string) string {
	version = NormalizeCodexClientVersion(version)
	if version == "" || strings.Contains(version, "-") {
		return ""
	}
	return version
}

const (
	openAICodexVersionSourceOverride = "override"
	openAICodexVersionSourceSynced   = "synced"
	openAICodexVersionSourceCompiled = "compiled"
)

// resolveOpenAICodexClientVersion is the single version-selection rule used by
// settings, synchronization, and all outbound identity builders.
func resolveOpenAICodexClientVersion(override, synced string) (string, string) {
	if version := NormalizeCodexClientVersion(override); version != "" && CompareVersions(version, codexUpstreamMinVersion) >= 0 {
		return version, openAICodexVersionSourceOverride
	}
	if version := normalizeStableCodexClientVersion(synced); version != "" && CompareVersions(version, codexCLIVersion) >= 0 {
		return version, openAICodexVersionSourceSynced
	}
	return codexCLIVersion, openAICodexVersionSourceCompiled
}
