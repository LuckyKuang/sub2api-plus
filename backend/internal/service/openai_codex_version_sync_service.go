package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	// openAICodexVersionSyncInterval 自动同步间隔。上游客户端发版频率是天级，
	// 6 小时足够及时跟上，同时把对 GitHub API 的调用压到每天 4 次。
	openAICodexVersionSyncInterval = 6 * time.Hour
	// openAICodexVersionSyncTimeout 单次同步的整体超时。
	openAICodexVersionSyncTimeout = 30 * time.Second
	// openAICodexVersionSyncRepo 官方 Codex 客户端仓库。
	openAICodexVersionSyncRepo = "openai/codex"
	// openAICodexVersionSyncPerPage 回退路径单次拉取的 release 数量（主路径见
	// fetchLatestStableVersion）。该仓库预发布极密集——0.145.0 与 0.146.0 之间隔着 20 多个
	// alpha，实测 30 条里只有 2 条稳定版，第二条已排在第 26 位，因此这个页大小不能再往下调，
	// 否则整页扫不到稳定版、同步会静默停更。
	openAICodexVersionSyncPerPage = 30
	// openAICodexVersionTagPrefix 客户端 release 的 tag 前缀（如 rust-v0.146.0）。
	// 同仓库还有其他组件的 tag（如 rusty-v8-*），必须按前缀过滤，否则会同步到无关版本号。
	openAICodexVersionTagPrefix = "rust-v"
)

// OpenAICodexVersionSyncService 周期性把官方 Codex 客户端的最新稳定版版本号同步到设置，
// 供出站规范身份使用，避免为了跟上游版本而发新版本。
//
// 同步值写入 SettingKeyOpenAICodexClientVersionSynced（本服务独占写入）；管理员在面板填写的
// SettingKeyOpenAICodexClientVersion 优先级更高，因此手工固定版本不会被同步覆盖。
type OpenAICodexVersionSyncService struct {
	settingRepo    SettingRepository
	settingService *SettingService
	githubClient   GitHubReleaseClient
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
}

func NewOpenAICodexVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
	interval time.Duration,
) *OpenAICodexVersionSyncService {
	return &OpenAICodexVersionSyncService{
		settingRepo:    settingRepo,
		settingService: settingService,
		githubClient:   githubClient,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *OpenAICodexVersionSyncService) Start() {
	if s == nil || s.settingRepo == nil || s.githubClient == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runInitial()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *OpenAICodexVersionSyncService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runInitial 执行启动时的首次同步。若同步值在一个同步周期内已被刷新过则跳过：
// 频繁重启、滚动发布或崩溃重启会让「启动即同步」放大成对 GitHub 的连续请求，
// 而版本号是天级变化的，重启后没有立刻重新拉取的必要。
func (s *OpenAICodexVersionSyncService) runInitial() {
	if s.syncedWithinInterval() {
		return
	}
	s.runOnce()
}

// syncedWithinInterval 判断不低于编译基线的稳定同步值是否仍在一个同步周期内。
// 优先用上次检查时间；升级前没有该字段时回退到同步值行的 UpdatedAt。
// 读取失败、上次检查失败、预发布、低于基线或尚无有效同步值时返回 false，让启动同步照常执行。
func (s *OpenAICodexVersionSyncService) syncedWithinInterval() bool {
	if s.interval <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	version := normalizeStableCodexClientVersion(s.currentSyncedVersion(ctx))
	if version == "" || CompareVersions(version, codexCLIVersion) < 0 {
		return false
	}
	if strings.TrimSpace(s.currentSyncError(ctx)) != "" {
		return false
	}
	checkedAt := s.lastCheckAt(ctx)
	if checkedAt.IsZero() {
		return false
	}
	return time.Since(checkedAt) < s.interval
}

// OpenAICodexVersionSyncResult 是一次立即同步的可观察结果，供面板展示。
type OpenAICodexVersionSyncResult struct {
	SyncedVersion string
	CheckedAt     string
	Error         string
	Updated       bool
}

// SyncNow 立刻向官方仓库检查一次，忽略自动同步开关。
// 后台定时任务仍受开关约束；面板打开开关或点击「立即同步」走这条路径。
func (s *OpenAICodexVersionSyncService) SyncNow(ctx context.Context) OpenAICodexVersionSyncResult {
	if s == nil || s.settingRepo == nil || s.githubClient == nil {
		return OpenAICodexVersionSyncResult{Error: "codex version sync is unavailable"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, openAICodexVersionSyncTimeout)
	defer cancel()
	return s.syncOnce(ctx)
}

func (s *OpenAICodexVersionSyncService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	if !s.autoSyncEnabled(ctx) {
		return
	}
	s.syncOnce(ctx)
}

func (s *OpenAICodexVersionSyncService) syncOnce(ctx context.Context) OpenAICodexVersionSyncResult {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	current := normalizeStableCodexClientVersion(s.currentSyncedVersion(ctx))
	latest, fetchErr := s.fetchLatestStableVersion(ctx)
	latest = normalizeStableCodexClientVersion(latest)
	if latest == "" {
		msg := "no stable rust-v Codex release found"
		if fetchErr != nil {
			msg = fetchErr.Error()
		}
		s.recordSyncOutcome(ctx, checkedAt, msg)
		slog.Warn("openai_codex_version_sync_no_usable_version", "error", msg)
		return OpenAICodexVersionSyncResult{SyncedVersion: current, CheckedAt: checkedAt, Error: msg}
	}
	if CompareVersions(latest, codexCLIVersion) < 0 {
		msg := fmt.Sprintf("synced version %s is below compiled baseline %s", latest, codexCLIVersion)
		s.recordSyncOutcome(ctx, checkedAt, msg)
		slog.Warn("openai_codex_version_sync_below_compiled_baseline", "version", latest, "baseline", codexCLIVersion)
		return OpenAICodexVersionSyncResult{SyncedVersion: current, CheckedAt: checkedAt, Error: msg}
	}

	updated := false
	// 只向前推进：上游偶发返回旧数据或重新发布历史 tag 时不把已同步的版本号降级。
	if current == "" || CompareVersions(latest, current) > 0 {
		if err := s.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersionSynced, latest); err != nil {
			msg := err.Error()
			s.recordSyncOutcome(ctx, checkedAt, msg)
			slog.Warn("openai_codex_version_sync_persist_failed", "version", latest, "error", err)
			return OpenAICodexVersionSyncResult{SyncedVersion: current, CheckedAt: checkedAt, Error: msg}
		}
		updated = true
		current = latest
		s.settingService.InvalidateOpenAICodexClientVersionCache()
		slog.Info("openai_codex_version_synced", "version", latest)
	}
	s.recordSyncOutcome(ctx, checkedAt, "")
	return OpenAICodexVersionSyncResult{SyncedVersion: current, CheckedAt: checkedAt, Updated: updated}
}

func (s *OpenAICodexVersionSyncService) recordSyncOutcome(ctx context.Context, checkedAt, errMsg string) {
	if s == nil || s.settingRepo == nil {
		return
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersionSyncedCheckedAt, checkedAt); err != nil {
		slog.Warn("openai_codex_version_sync_checked_at_persist_failed", "error", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersionSyncError, errMsg); err != nil {
		slog.Warn("openai_codex_version_sync_error_persist_failed", "error", err)
	}
}

// fetchLatestStableVersion 取官方最新稳定版客户端版本号；取不到时返回空串，
// 由调用方保持既有值（不清空、不降级），各失败分支自行落日志。
//
// 主路径 /releases/latest：该端点本身就排除 draft 与 prerelease，直接给出最新正式发布，
// 因此不受该仓库预发布密度的影响，也不需要为了「窗口里得有一条稳定版」而多拉数据——
// 实测单条 release 约 0.3MB，而 per_page=30 的列表页约 10MB。
//
// 回退列表扫描：latest 是跨 tag 家族按 published_at 取的，若同仓库其他组件
// （如 rusty-v8-*）某天发了正式 release 而成为 latest，主路径会被 rust-v 前缀过滤挡掉；
// 此时必须扫一页 release 才能继续跟随官方版本，否则版本号会静默停更。
// 两条路径共用同一套过滤（前缀 / draft / prerelease / 版本号形态），语义不会分叉。
func (s *OpenAICodexVersionSyncService) fetchLatestStableVersion(ctx context.Context) (string, error) {
	var firstErr error
	release, err := s.githubClient.FetchLatestRelease(ctx, openAICodexVersionSyncRepo)
	if err != nil {
		slog.Warn("openai_codex_version_sync_latest_fetch_failed", "error", err)
		firstErr = err
	} else if version := latestCodexStableReleaseVersion([]*GitHubRelease{release}); version != "" {
		return version, nil
	}

	// 主路径没拿到可用版本（抓取失败，或 latest 不是客户端 tag 家族的稳定版）。
	releases, err := s.githubClient.FetchRecentReleases(ctx, openAICodexVersionSyncRepo, openAICodexVersionSyncPerPage)
	if err != nil {
		slog.Warn("openai_codex_version_sync_fetch_failed", "error", err)
		if firstErr != nil {
			return "", firstErr
		}
		return "", err
	}
	version := latestCodexStableReleaseVersion(releases)
	if version == "" {
		slog.Warn("openai_codex_version_sync_no_stable_release", "repo", openAICodexVersionSyncRepo)
		if firstErr != nil {
			return "", firstErr
		}
		return "", errors.New("no stable rust-v Codex release found")
	}
	return version, nil
}

// autoSyncEnabled 读取面板开关。缺失或空值视为开启，与设置默认值一致；
// 读取失败时保持开启，避免一次数据库抖动就静默停掉版本跟随。
func (s *OpenAICodexVersionSyncService) autoSyncEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexVersionAutoSyncEnabled)
	if err != nil {
		return true
	}
	if strings.TrimSpace(value) == "" {
		return true
	}
	return strings.TrimSpace(value) == "true"
}

func (s *OpenAICodexVersionSyncService) currentSyncedVersion(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if err != nil {
		return ""
	}
	return value
}

func (s *OpenAICodexVersionSyncService) currentSyncError(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSyncError)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *OpenAICodexVersionSyncService) lastCheckAt(ctx context.Context) time.Time {
	if value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSyncedCheckedAt); err == nil {
		if checkedAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(value)); parseErr == nil {
			return checkedAt
		}
	}
	setting, err := s.settingRepo.Get(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if err != nil || setting == nil {
		return time.Time{}
	}
	return setting.UpdatedAt
}

// latestCodexStableReleaseVersion 从 release 列表里挑出最大的稳定版客户端版本号。
// 过滤条件：tag 前缀为 rust-v（排除同仓库其他组件的 tag）、非草稿、非预发布、
// 版本号不带 -alpha/-beta 之类后缀。取最大值而非最新发布，避免重新发布历史 tag 造成回退。
// 主路径的单条 /releases/latest 结果也走本函数（单元素切片），保证两条取数路径的过滤语义一致。
func latestCodexStableReleaseVersion(releases []*GitHubRelease) string {
	best := ""
	for _, release := range releases {
		if release == nil || release.Draft || release.Prerelease {
			continue
		}
		tag := strings.TrimSpace(release.TagName)
		if !strings.HasPrefix(tag, openAICodexVersionTagPrefix) {
			continue
		}
		version := NormalizeCodexClientVersion(strings.TrimPrefix(tag, openAICodexVersionTagPrefix))
		if version == "" || strings.Contains(version, "-") {
			continue
		}
		if best == "" || CompareVersions(version, best) > 0 {
			best = version
		}
	}
	return best
}
