package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const (
	WeComUsageAlertEnabledExtraKey    = "wecom_usage_alert_enabled"
	WeComUsageAlertWebhookURLExtraKey = "wecom_usage_alert_webhook_url"
	WeComUsageAlertCronExtraKey       = "wecom_usage_alert_cron"
	WeComUsageAlertForceProbeExtraKey = "wecom_usage_alert_force_probe"
	WeComUsageAlertNextRunAtExtraKey  = "wecom_usage_alert_next_run_at"
	WeComUsageAlertLastRunAtExtraKey  = "wecom_usage_alert_last_run_at"
	WeComUsageAlertLastErrorExtraKey  = "wecom_usage_alert_last_error"

	weComUsageAlertMaxPerCycle      = 20
	weComUsageAlertCycleInterval    = time.Minute
	weComUsageAlertWebhookTimeout   = 15 * time.Second
	weComUsageAlertMarkdownMaxRunes = 3500
)

var ErrWeComUsageAlertUnavailable = infraerrors.BadRequest("WECOM_USAGE_ALERT_UNAVAILABLE", "wecom usage alert service is not available")

var weComUsageAlertCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// WeComUsageAlertConfig is the admin-facing config for one account.
type WeComUsageAlertConfig struct {
	Enabled      bool       `json:"enabled"`
	WebhookURL   string     `json:"webhook_url"`
	Cron         string     `json:"cron_expression"`
	ForceProbe   bool       `json:"force_probe"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

type weComUsageAlertRepository interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	ListDueWeComUsageAlertAccounts(ctx context.Context, now time.Time, limit int) ([]Account, error)
}

// WeComUsageAlertService schedules and sends account usage-window reports to WeCom bots.
type WeComUsageAlertService struct {
	accountRepo AccountRepository
	usageSvc    *AccountUsageService
	cfg         *config.Config
	httpClient  *http.Client

	mu           sync.Mutex
	started      bool
	stopped      bool
	parentCtx    context.Context
	parentCancel context.CancelFunc
	wg           sync.WaitGroup
}

func NewWeComUsageAlertService(
	accountRepo AccountRepository,
	usageSvc *AccountUsageService,
	cfg *config.Config,
) *WeComUsageAlertService {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	return &WeComUsageAlertService{
		accountRepo:  accountRepo,
		usageSvc:     usageSvc,
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: weComUsageAlertWebhookTimeout},
		parentCtx:    parentCtx,
		parentCancel: parentCancel,
	}
}

func ProvideWeComUsageAlertService(
	accountRepo AccountRepository,
	usageSvc *AccountUsageService,
	cfg *config.Config,
) *WeComUsageAlertService {
	svc := NewWeComUsageAlertService(accountRepo, usageSvc, cfg)
	svc.Start()
	return svc
}

func (s *WeComUsageAlertService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runLoop()
}

func (s *WeComUsageAlertService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.parentCancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *WeComUsageAlertService) runLoop() {
	defer s.wg.Done()
	_ = s.RunDue(s.parentCtx)
	ticker := time.NewTicker(weComUsageAlertCycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.parentCtx.Done():
			return
		case <-ticker.C:
			if err := s.RunDue(s.parentCtx); err != nil {
				logger.LegacyPrintf("service.wecom_usage_alert", "run_due_failed: err=%v", err)
			}
		}
	}
}

func (s *WeComUsageAlertService) GetConfig(ctx context.Context, accountID int64) (*WeComUsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrWeComUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	cfg := WeComUsageAlertConfigFromAccount(account)
	return &cfg, nil
}

func (s *WeComUsageAlertService) UpdateConfig(ctx context.Context, accountID int64, input WeComUsageAlertConfig) (*WeComUsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrWeComUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeWeComUsageAlertConfig(input, time.Now())
	if err != nil {
		return nil, err
	}

	updates := weComUsageAlertExtraUpdates(normalized)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, err
	}
	mergeAccountExtra(account, updates)
	cfg := WeComUsageAlertConfigFromAccount(account)
	return &cfg, nil
}

func (s *WeComUsageAlertService) TestSend(ctx context.Context, accountID int64) (*WeComUsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil || s.usageSvc == nil {
		return nil, ErrWeComUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	cfg := WeComUsageAlertConfigFromAccount(account)
	if strings.TrimSpace(cfg.WebhookURL) == "" {
		return nil, infraerrors.BadRequest("WECOM_USAGE_ALERT_WEBHOOK_REQUIRED", "webhook_url is required")
	}
	if err := validateWeComWebhookURL(cfg.WebhookURL); err != nil {
		return nil, err
	}
	if err := s.sendForAccount(ctx, account, cfg.ForceProbe, false); err != nil {
		_ = s.persistRunState(ctx, account.ID, cfg.Cron, time.Now(), err.Error(), cfg.NextRunAt)
		return nil, infraerrors.BadRequest("WECOM_USAGE_ALERT_SEND_FAILED", err.Error())
	}
	now := time.Now()
	next := cfg.NextRunAt
	_ = s.persistRunState(ctx, account.ID, cfg.Cron, now, "", next)
	updated, err := s.GetConfig(ctx, accountID)
	if err != nil {
		return &cfg, nil
	}
	return updated, nil
}

func (s *WeComUsageAlertService) RunDue(ctx context.Context) error {
	if s == nil || s.accountRepo == nil || s.usageSvc == nil {
		return nil
	}
	writer, ok := s.accountRepo.(weComUsageAlertRepository)
	if !ok {
		return nil
	}
	now := time.Now()
	accounts, err := writer.ListDueWeComUsageAlertAccounts(ctx, now, weComUsageAlertMaxPerCycle)
	if err != nil {
		return err
	}
	for i := range accounts {
		account := accounts[i]
		cfg := WeComUsageAlertConfigFromAccount(&account)
		runErr := s.sendForAccount(ctx, &account, cfg.ForceProbe, true)
		errText := ""
		if runErr != nil {
			errText = runErr.Error()
			logger.LegacyPrintf("service.wecom_usage_alert", "send_failed: account_id=%d err=%v", account.ID, runErr)
		}
		_ = s.persistRunState(ctx, account.ID, cfg.Cron, time.Now(), errText, nil)
	}
	return nil
}

func (s *WeComUsageAlertService) sendForAccount(ctx context.Context, account *Account, forceProbe bool, _ bool) error {
	if account == nil {
		return fmt.Errorf("account is nil")
	}
	webhookURL := strings.TrimSpace(account.getExtraString(WeComUsageAlertWebhookURLExtraKey))
	if webhookURL == "" {
		return fmt.Errorf("webhook_url is empty")
	}
	usage, err := s.usageSvc.GetUsage(ctx, account.ID, forceProbe)
	if err != nil {
		return fmt.Errorf("get usage: %w", err)
	}
	content := formatWeComUsageAlertMarkdown(account, usage, time.Now())
	return s.postWeComMarkdown(ctx, webhookURL, content)
}

func (s *WeComUsageAlertService) persistRunState(ctx context.Context, accountID int64, cronExpr string, now time.Time, lastError string, keepNext *time.Time) error {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil
	}
	updates := map[string]any{
		WeComUsageAlertLastRunAtExtraKey: now.UTC().Format(time.RFC3339),
		WeComUsageAlertLastErrorExtraKey: lastError,
	}
	if keepNext != nil {
		updates[WeComUsageAlertNextRunAtExtraKey] = keepNext.UTC().Format(time.RFC3339)
	} else if strings.TrimSpace(cronExpr) != "" {
		if next, err := computeWeComUsageAlertNextRun(cronExpr, now); err == nil {
			updates[WeComUsageAlertNextRunAtExtraKey] = next.UTC().Format(time.RFC3339)
		}
	}
	return s.accountRepo.UpdateExtra(ctx, accountID, updates)
}

func (s *WeComUsageAlertService) postWeComMarkdown(ctx context.Context, webhookURL, content string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: weComUsageAlertWebhookTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("webhook errcode=%d errmsg=%s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func WeComUsageAlertConfigFromAccount(account *Account) WeComUsageAlertConfig {
	if account == nil {
		return WeComUsageAlertConfig{}
	}
	cfg := WeComUsageAlertConfig{
		Enabled:    account.getExtraBool(WeComUsageAlertEnabledExtraKey),
		WebhookURL: strings.TrimSpace(account.getExtraString(WeComUsageAlertWebhookURLExtraKey)),
		Cron:       strings.TrimSpace(account.getExtraString(WeComUsageAlertCronExtraKey)),
		ForceProbe: account.getExtraBool(WeComUsageAlertForceProbeExtraKey),
		LastError:  strings.TrimSpace(account.getExtraString(WeComUsageAlertLastErrorExtraKey)),
	}
	if t, err := parseOptionalRFC3339(account.Extra[WeComUsageAlertNextRunAtExtraKey]); err == nil {
		cfg.NextRunAt = t
	}
	if t, err := parseOptionalRFC3339(account.Extra[WeComUsageAlertLastRunAtExtraKey]); err == nil {
		cfg.LastRunAt = t
	}
	return cfg
}

func normalizeWeComUsageAlertConfig(input WeComUsageAlertConfig, now time.Time) (WeComUsageAlertConfig, error) {
	out := WeComUsageAlertConfig{
		Enabled:    input.Enabled,
		WebhookURL: strings.TrimSpace(input.WebhookURL),
		Cron:       strings.TrimSpace(input.Cron),
		ForceProbe: input.ForceProbe,
	}
	if !out.Enabled {
		return out, nil
	}
	if out.WebhookURL == "" {
		return out, infraerrors.BadRequest("WECOM_USAGE_ALERT_WEBHOOK_REQUIRED", "webhook_url is required when enabled")
	}
	if err := validateWeComWebhookURL(out.WebhookURL); err != nil {
		return out, err
	}
	if out.Cron == "" {
		return out, infraerrors.BadRequest("WECOM_USAGE_ALERT_CRON_REQUIRED", "cron_expression is required when enabled")
	}
	next, err := computeWeComUsageAlertNextRun(out.Cron, now)
	if err != nil {
		return out, infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_CRON", "invalid cron expression: "+err.Error())
	}
	out.NextRunAt = &next
	return out, nil
}

func weComUsageAlertExtraUpdates(cfg WeComUsageAlertConfig) map[string]any {
	updates := map[string]any{
		WeComUsageAlertEnabledExtraKey:    cfg.Enabled,
		WeComUsageAlertWebhookURLExtraKey: cfg.WebhookURL,
		WeComUsageAlertCronExtraKey:       cfg.Cron,
		WeComUsageAlertForceProbeExtraKey: cfg.ForceProbe,
		WeComUsageAlertLastErrorExtraKey:  "",
	}
	if cfg.Enabled && cfg.NextRunAt != nil {
		updates[WeComUsageAlertNextRunAtExtraKey] = cfg.NextRunAt.UTC().Format(time.RFC3339)
	} else {
		updates[WeComUsageAlertNextRunAtExtraKey] = nil
	}
	return updates
}

func validateWeComWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_WEBHOOK", "invalid webhook_url")
	}
	if u.Scheme != "https" {
		return infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_WEBHOOK", "webhook_url must use https")
	}
	host := strings.ToLower(u.Hostname())
	if host != "qyapi.weixin.qq.com" {
		return infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_WEBHOOK", "webhook_url host must be qyapi.weixin.qq.com")
	}
	if u.Path != "/cgi-bin/webhook/send" {
		return infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_WEBHOOK", "webhook_url path must be /cgi-bin/webhook/send")
	}
	if strings.TrimSpace(u.Query().Get("key")) == "" {
		return infraerrors.BadRequest("WECOM_USAGE_ALERT_INVALID_WEBHOOK", "webhook_url must include key query parameter")
	}
	return nil
}

func computeWeComUsageAlertNextRun(cronExpr string, from time.Time) (time.Time, error) {
	sched, err := weComUsageAlertCronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

func parseOptionalRFC3339(raw any) (*time.Time, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil")
	}
	str := strings.TrimSpace(fmt.Sprint(raw))
	if str == "" || str == "<nil>" {
		return nil, fmt.Errorf("empty")
	}
	t, err := time.Parse(time.RFC3339, str)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, str)
		if err != nil {
			return nil, err
		}
	}
	return &t, nil
}

func formatWeComUsageAlertMarkdown(account *Account, usage *UsageInfo, now time.Time) string {
	name := "unknown"
	platform := ""
	accountType := ""
	id := int64(0)
	if account != nil {
		name = account.Name
		platform = account.Platform
		accountType = account.Type
		id = account.ID
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("**用量窗口报告**\n> 账号: %s (#%d)\n> 平台: %s / %s\n> 时间: %s\n",
		escapeWeComMarkdown(name), id, escapeWeComMarkdown(platform), escapeWeComMarkdown(accountType), now.Format("2006-01-02 15:04:05")))

	if usage == nil {
		b.WriteString("\n暂无用量数据")
		return truncateRunes(b.String(), weComUsageAlertMarkdownMaxRunes)
	}
	if usage.Error != "" {
		b.WriteString("\n> 警告: ")
		b.WriteString(escapeWeComMarkdown(usage.Error))
		b.WriteByte('\n')
	}

	appendWindow := func(label string, progress *UsageProgress) {
		if progress == nil {
			return
		}
		reset := "未知"
		if progress.ResetsAt != nil {
			reset = progress.ResetsAt.Format("2006-01-02 15:04:05")
		} else if progress.RemainingSeconds > 0 {
			reset = formatDurationSeconds(progress.RemainingSeconds)
		}
		b.WriteString(fmt.Sprintf("\n**%s** %.0f%%  重置: %s", label, progress.Utilization, reset))
		if progress.WindowStats != nil {
			ws := progress.WindowStats
			b.WriteString(fmt.Sprintf("\n请求 %d · Token %s · A $%.2f · U $%.2f",
				ws.Requests, formatCompactTokenCount(ws.Tokens), ws.Cost, ws.UserCost))
		}
		b.WriteByte('\n')
	}

	appendWindow("5h", usage.FiveHour)
	appendWindow("7d", usage.SevenDay)
	appendWindow("7d S", usage.SevenDaySonnet)
	appendWindow("7d F", usage.SevenDayFable)

	if usage.FiveHour == nil && usage.SevenDay == nil && usage.SevenDaySonnet == nil && usage.SevenDayFable == nil {
		b.WriteString("\n暂无 5h/7d 窗口数据（可能尚未采样或该账号类型不支持）")
	}

	return truncateRunes(b.String(), weComUsageAlertMarkdownMaxRunes)
}

func escapeWeComMarkdown(s string) string {
	replacer := strings.NewReplacer("`", "'", "*", "＊", "_", "＿", "\n", " ")
	return replacer.Replace(s)
}

func formatDurationSeconds(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatCompactTokenCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
