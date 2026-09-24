package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
)

type cachedClaudeCodeClientVersion struct {
	version   string
	expiresAt int64 // unix nano
}

const claudeCodeClientVersionCacheTTL = 60 * time.Second
const claudeCodeClientVersionErrorTTL = 5 * time.Second
const claudeCodeClientVersionDBTimeout = 5 * time.Second

// claudeCodeClientVersionSFKey singleflight 键。
const claudeCodeClientVersionSFKey = "claude_code_client_version"

// NormalizeClaudeCodeClientVersion 校验并归一化 Claude Code 客户端版本号，非法值返回空串。
// 容忍前导 "v" 与首尾空白；合法性复用 claude.IsSupportedCLIVersion。
func NormalizeClaudeCodeClientVersion(version string) string {
	normalized := strings.TrimSpace(version)
	normalized = strings.TrimPrefix(normalized, "v")
	if normalized == "" {
		return ""
	}
	if !claude.IsSupportedCLIVersion(normalized) {
		return ""
	}
	return normalized
}

// GetClaudeCodeClientVersion 返回出站声明的 Claude Code CLI 客户端版本号。
// 优先级：管理员面板覆写 → 自动同步到的最新稳定版 → claude.CLIVersion() 内置基线。
// 与 Codex 客户端版本同步服务并存（双轨，互不干扰）。
func (s *SettingService) GetClaudeCodeClientVersion(ctx context.Context) string {
	fallback := claude.CLIVersion()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if cached, ok := s.claudeCodeVersionCache.Load().(*cachedClaudeCodeClientVersion); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.version
		}
	}

	result, _, _ := s.claudeCodeVersionSF.Do(claudeCodeClientVersionSFKey, func() (any, error) {
		if cached, ok := s.claudeCodeVersionCache.Load().(*cachedClaudeCodeClientVersion); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.version, nil
			}
		}
		if ctx == nil {
			ctx = context.Background()
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCodeClientVersionDBTimeout)
		defer cancel()
		values, err := s.settingRepo.GetMultiple(dbCtx, []string{
			SettingKeyClaudeCodeClientVersion,
			SettingKeyClaudeCodeClientVersionSynced,
		})
		if err != nil {
			slog.Warn("failed to get claude code client version setting", "error", err)
			s.claudeCodeVersionCache.Store(&cachedClaudeCodeClientVersion{
				version:   fallback,
				expiresAt: time.Now().Add(claudeCodeClientVersionErrorTTL).UnixNano(),
			})
			return fallback, nil
		}
		version := NormalizeClaudeCodeClientVersion(values[SettingKeyClaudeCodeClientVersion])
		if version == "" {
			if raw := values[SettingKeyClaudeCodeClientVersion]; strings.TrimSpace(raw) != "" {
				slog.Warn("ignoring invalid claude_code_client_version setting; falling back to the next layer", "value", raw)
			}
			version = NormalizeClaudeCodeClientVersion(values[SettingKeyClaudeCodeClientVersionSynced])
			if version == "" && strings.TrimSpace(values[SettingKeyClaudeCodeClientVersionSynced]) != "" {
				slog.Warn("ignoring invalid claude_code_client_version_synced setting; falling back to the built-in pin", "value", values[SettingKeyClaudeCodeClientVersionSynced])
			}
		}
		if version == "" {
			version = fallback
		}
		s.claudeCodeVersionCache.Store(&cachedClaudeCodeClientVersion{
			version:   version,
			expiresAt: time.Now().Add(claudeCodeClientVersionCacheTTL).UnixNano(),
		})
		return version, nil
	})
	if version, ok := result.(string); ok && version != "" {
		return version
	}
	return fallback
}

// InvalidateClaudeCodeClientVersionCache 丢弃版本号缓存，下次读取回源。
// 面板保存与自动同步写入后调用。
func (s *SettingService) InvalidateClaudeCodeClientVersionCache() {
	if s == nil {
		return
	}
	s.claudeCodeVersionSF.Forget(claudeCodeClientVersionSFKey)
	s.claudeCodeVersionCache.Store((*cachedClaudeCodeClientVersion)(nil))
}
