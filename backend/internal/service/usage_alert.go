package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	UsageAlertRulesExtraKey = "usage_alert_rules"

	// Legacy single-rule keys (migrated on read).
	WeComUsageAlertEnabledExtraKey    = "wecom_usage_alert_enabled"
	WeComUsageAlertWebhookURLExtraKey = "wecom_usage_alert_webhook_url"
	WeComUsageAlertCronExtraKey       = "wecom_usage_alert_cron"
	WeComUsageAlertForceProbeExtraKey = "wecom_usage_alert_force_probe"
	WeComUsageAlertNextRunAtExtraKey  = "wecom_usage_alert_next_run_at"
	WeComUsageAlertLastRunAtExtraKey  = "wecom_usage_alert_last_run_at"
	WeComUsageAlertLastErrorExtraKey  = "wecom_usage_alert_last_error"

	UsageAlertChannelWeCom   = "wecom"
	UsageAlertChannelFeishu  = "feishu"
	UsageAlertChannelCustom  = "custom"

	usageAlertMaxPerCycle       = 20
	usageAlertCycleInterval     = time.Minute
	usageAlertWebhookTimeout    = 15 * time.Second
	usageAlertMarkdownMaxRunes  = 3500
	usageAlertMaxRules          = 20
	usageAlertMaxQuietRanges    = 10
	usageAlertMaxCooldownSeconds = 30 * 24 * 3600 // 30 days
)

var ErrUsageAlertUnavailable = infraerrors.BadRequest("USAGE_ALERT_UNAVAILABLE", "usage alert service is not available")

// Deprecated: use ErrUsageAlertUnavailable.
var ErrWeComUsageAlertUnavailable = ErrUsageAlertUnavailable

var usageAlertCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// UsageAlertRule is one schedule + channel configuration for an account.
type UsageAlertRule struct {
	ID                   string     `json:"id"`
	Enabled              bool       `json:"enabled"`
	Channel              string     `json:"channel"` // wecom | feishu | custom
	WebhookURL           string     `json:"webhook_url"`
	Cron                 string     `json:"cron_expression"` // scheduled usage report
	ForceProbe           bool       `json:"force_probe"`
	ThresholdEnabled     bool       `json:"threshold_enabled"`
	ThresholdPercent     int        `json:"threshold_percent"`      // 1-99 when threshold_enabled
	ThresholdWatchCron   string     `json:"threshold_watch_cron"`   // required when threshold_enabled
	CooldownSeconds       int        `json:"cooldown_seconds"`       // required when threshold_enabled
	QuietHours           []string   `json:"quiet_hours"`            // optional daily ranges "HH:MM:SS-HH:MM:SS"
	NextRunAt            *time.Time `json:"next_run_at,omitempty"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	ThresholdNextRunAt   *time.Time `json:"threshold_next_run_at,omitempty"`
	LastThresholdAlertAt *time.Time `json:"last_threshold_alert_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
}

// UsageAlertConfig is the admin-facing multi-rule config for one account.
type UsageAlertConfig struct {
	Rules []UsageAlertRule `json:"rules"`
}

// WeComUsageAlertConfig keeps legacy single-rule API shape for older clients.
type WeComUsageAlertConfig struct {
	Enabled    bool       `json:"enabled"`
	WebhookURL string     `json:"webhook_url"`
	Cron       string     `json:"cron_expression"`
	ForceProbe bool       `json:"force_probe"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

type usageAlertRepository interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	ListDueUsageAlertAccounts(ctx context.Context, now time.Time, limit int) ([]Account, error)
}

// UsageAlertService schedules usage-window reports / threshold alerts to chat bots.
type UsageAlertService struct {
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

// WeComUsageAlertService is a compatibility alias.
type WeComUsageAlertService = UsageAlertService

func NewUsageAlertService(
	accountRepo AccountRepository,
	usageSvc *AccountUsageService,
	cfg *config.Config,
) *UsageAlertService {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	return &UsageAlertService{
		accountRepo:  accountRepo,
		usageSvc:     usageSvc,
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: usageAlertWebhookTimeout},
		parentCtx:    parentCtx,
		parentCancel: parentCancel,
	}
}

func NewWeComUsageAlertService(accountRepo AccountRepository, usageSvc *AccountUsageService, cfg *config.Config) *UsageAlertService {
	return NewUsageAlertService(accountRepo, usageSvc, cfg)
}

func ProvideUsageAlertService(
	accountRepo AccountRepository,
	usageSvc *AccountUsageService,
	cfg *config.Config,
) *UsageAlertService {
	svc := NewUsageAlertService(accountRepo, usageSvc, cfg)
	svc.Start()
	return svc
}

func ProvideWeComUsageAlertService(
	accountRepo AccountRepository,
	usageSvc *AccountUsageService,
	cfg *config.Config,
) *UsageAlertService {
	return ProvideUsageAlertService(accountRepo, usageSvc, cfg)
}

func (s *UsageAlertService) Start() {
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

func (s *UsageAlertService) Stop() {
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

func (s *UsageAlertService) runLoop() {
	defer s.wg.Done()
	_ = s.RunDue(s.parentCtx)
	ticker := time.NewTicker(usageAlertCycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.parentCtx.Done():
			return
		case <-ticker.C:
			if err := s.RunDue(s.parentCtx); err != nil {
				logger.LegacyPrintf("service.usage_alert", "run_due_failed: err=%v", err)
			}
		}
	}
}

func (s *UsageAlertService) GetConfig(ctx context.Context, accountID int64) (*UsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	cfg := UsageAlertConfigFromAccount(account)
	return &cfg, nil
}

func (s *UsageAlertService) UpdateConfig(ctx context.Context, accountID int64, input UsageAlertConfig) (*UsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	existing := UsageAlertConfigFromAccount(account)
	normalized, err := normalizeUsageAlertConfig(input, existing, time.Now())
	if err != nil {
		return nil, err
	}
	updates := usageAlertExtraUpdates(normalized)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, err
	}
	mergeAccountExtra(account, updates)
	cfg := UsageAlertConfigFromAccount(account)
	return &cfg, nil
}

// UsageAlertTestRequest is the optional body for POST .../usage-alert/test.
type UsageAlertTestRequest struct {
	RuleID string          `json:"rule_id"`
	Rule   *UsageAlertRule `json:"rule"`
}

// TestSend sends one rule immediately (ignores threshold gate so admins can verify the channel).
func (s *UsageAlertService) TestSend(ctx context.Context, accountID int64, ruleID string, draft *UsageAlertRule) (*UsageAlertConfig, error) {
	if s == nil || s.accountRepo == nil || s.usageSvc == nil {
		return nil, ErrUsageAlertUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	cfg := UsageAlertConfigFromAccount(account)
	var rule *UsageAlertRule
	if draft != nil {
		normalized, normErr := normalizeUsageAlertRule(*draft, time.Now(), true)
		if normErr != nil {
			return nil, normErr
		}
		rule = &normalized
	} else {
		for i := range cfg.Rules {
			if cfg.Rules[i].ID == ruleID || (ruleID == "" && i == 0) {
				rule = &cfg.Rules[i]
				break
			}
		}
	}
	if rule == nil {
		return nil, infraerrors.BadRequest("USAGE_ALERT_RULE_NOT_FOUND", "usage alert rule not found")
	}
	if strings.TrimSpace(rule.WebhookURL) == "" {
		return nil, infraerrors.BadRequest("USAGE_ALERT_WEBHOOK_REQUIRED", "webhook_url is required")
	}
	if err := validateUsageAlertWebhookURL(rule.Channel, rule.WebhookURL); err != nil {
		return nil, err
	}

	// Test send ignores quiet hours / cooldown / threshold gate.
	runErr := s.sendUsageAlert(ctx, account, *rule, "用量窗口报告", false)
	now := time.Now()
	for i := range cfg.Rules {
		if cfg.Rules[i].ID != rule.ID {
			continue
		}
		cfg.Rules[i].LastRunAt = &now
		if runErr != nil {
			cfg.Rules[i].LastError = runErr.Error()
		} else {
			cfg.Rules[i].LastError = ""
		}
		break
	}
	_ = s.accountRepo.UpdateExtra(ctx, account.ID, usageAlertExtraUpdates(cfg))
	if runErr != nil {
		return &cfg, infraerrors.BadRequest("USAGE_ALERT_SEND_FAILED", runErr.Error())
	}
	return &cfg, nil
}

func (s *UsageAlertService) RunDue(ctx context.Context) error {
	if s == nil || s.accountRepo == nil || s.usageSvc == nil {
		return nil
	}
	writer, ok := s.accountRepo.(usageAlertRepository)
	if !ok {
		return nil
	}
	now := time.Now()
	accounts, err := writer.ListDueUsageAlertAccounts(ctx, now, usageAlertMaxPerCycle)
	if err != nil {
		return err
	}
	for i := range accounts {
		account := accounts[i]
		cfg := UsageAlertConfigFromAccount(&account)
		changed := false
		for ri := range cfg.Rules {
			if !cfg.Rules[ri].Enabled {
				continue
			}
			if s.processReportDue(ctx, &account, &cfg.Rules[ri], now) {
				changed = true
			}
			if s.processThresholdDue(ctx, &account, &cfg.Rules[ri], now) {
				changed = true
			}
		}
		if changed {
			if err := s.accountRepo.UpdateExtra(ctx, account.ID, usageAlertExtraUpdates(cfg)); err != nil {
				logger.LegacyPrintf("service.usage_alert", "persist_failed: account_id=%d err=%v", account.ID, err)
			}
		}
	}
	return nil
}

// processReportDue handles the scheduled usage-window report cron.
func (s *UsageAlertService) processReportDue(ctx context.Context, account *Account, rule *UsageAlertRule, now time.Time) bool {
	if rule == nil || !rule.Enabled || strings.TrimSpace(rule.Cron) == "" {
		return false
	}
	if rule.NextRunAt != nil && rule.NextRunAt.After(now) {
		return false
	}

	quiet, _ := parseUsageAlertQuietHours(rule.QuietHours)
	if isInQuietHours(now, quiet) {
		if next, err := nextCronOutsideQuiet(rule.Cron, now, quiet); err == nil {
			rule.NextRunAt = &next
		}
		return true
	}

	runErr := s.sendUsageAlert(ctx, account, *rule, "用量窗口报告", false)
	runAt := time.Now()
	rule.LastRunAt = &runAt
	if runErr != nil {
		rule.LastError = runErr.Error()
		logger.LegacyPrintf("service.usage_alert", "report_failed: account_id=%d rule_id=%s err=%v", account.ID, rule.ID, runErr)
	} else {
		rule.LastError = ""
	}
	if next, err := nextCronOutsideQuiet(rule.Cron, runAt, quiet); err == nil {
		rule.NextRunAt = &next
	}
	return true
}

// processThresholdDue handles threshold watch cron + cooldown.
func (s *UsageAlertService) processThresholdDue(ctx context.Context, account *Account, rule *UsageAlertRule, now time.Time) bool {
	if rule == nil || !rule.Enabled || !rule.ThresholdEnabled {
		return false
	}
	if strings.TrimSpace(rule.ThresholdWatchCron) == "" || rule.CooldownSeconds <= 0 {
		return false
	}
	if rule.ThresholdNextRunAt != nil && rule.ThresholdNextRunAt.After(now) {
		return false
	}

	quiet, _ := parseUsageAlertQuietHours(rule.QuietHours)
	if isInQuietHours(now, quiet) {
		if next, err := nextCronOutsideQuiet(rule.ThresholdWatchCron, now, quiet); err == nil {
			rule.ThresholdNextRunAt = &next
		}
		return true
	}

	// During cooldown: do not fetch usage; wake when cooldown ends.
	if rule.LastThresholdAlertAt != nil {
		cooldownUntil := rule.LastThresholdAlertAt.Add(time.Duration(rule.CooldownSeconds) * time.Second)
		if now.Before(cooldownUntil) {
			rule.ThresholdNextRunAt = &cooldownUntil
			return true
		}
	}

	usage, err := s.usageSvc.GetUsage(ctx, account.ID, rule.ForceProbe)
	runAt := time.Now()
	if err != nil {
		rule.LastError = fmt.Sprintf("get usage: %v", err)
		logger.LegacyPrintf("service.usage_alert", "threshold_watch_failed: account_id=%d rule_id=%s err=%v", account.ID, rule.ID, err)
		if next, nextErr := nextCronOutsideQuiet(rule.ThresholdWatchCron, runAt, quiet); nextErr == nil {
			rule.ThresholdNextRunAt = &next
		}
		return true
	}

	maxUtil := maxUsageUtilization(usage)
	if maxUtil < float64(rule.ThresholdPercent) {
		rule.LastError = ""
		if next, nextErr := nextCronOutsideQuiet(rule.ThresholdWatchCron, runAt, quiet); nextErr == nil {
			rule.ThresholdNextRunAt = &next
		}
		return true
	}

	title := fmt.Sprintf("用量阈值告警（≥%d%%）", rule.ThresholdPercent)
	msg := buildUsageAlertMessage(account, usage, runAt, title, true, rule.ThresholdPercent, maxUtil)
	if sendErr := s.postWebhook(ctx, rule.Channel, rule.WebhookURL, title, msg, account, usage, *rule, maxUtil); sendErr != nil {
		rule.LastError = sendErr.Error()
		logger.LegacyPrintf("service.usage_alert", "threshold_alert_failed: account_id=%d rule_id=%s err=%v", account.ID, rule.ID, sendErr)
		if next, nextErr := nextCronOutsideQuiet(rule.ThresholdWatchCron, runAt, quiet); nextErr == nil {
			rule.ThresholdNextRunAt = &next
		}
		return true
	}

	rule.LastError = ""
	rule.LastThresholdAlertAt = &runAt
	cooldownUntil := runAt.Add(time.Duration(rule.CooldownSeconds) * time.Second)
	rule.ThresholdNextRunAt = &cooldownUntil
	return true
}

func (s *UsageAlertService) sendUsageAlert(ctx context.Context, account *Account, rule UsageAlertRule, title string, thresholdAlert bool) error {
	if account == nil {
		return fmt.Errorf("account is nil")
	}
	usage, err := s.usageSvc.GetUsage(ctx, account.ID, rule.ForceProbe)
	if err != nil {
		return fmt.Errorf("get usage: %w", err)
	}
	maxUtil := maxUsageUtilization(usage)
	msg := buildUsageAlertMessage(account, usage, time.Now(), title, thresholdAlert, rule.ThresholdPercent, maxUtil)
	return s.postWebhook(ctx, rule.Channel, rule.WebhookURL, title, msg, account, usage, rule, maxUtil)
}

func (s *UsageAlertService) postWebhook(
	ctx context.Context,
	channel, webhookURL, title string,
	msg usageAlertMessage,
	account *Account,
	usage *UsageInfo,
	rule UsageAlertRule,
	maxUtil float64,
) error {
	var payload any
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case UsageAlertChannelFeishu:
		payload = map[string]any{
			"msg_type": "post",
			"content": map[string]any{
				"post": map[string]any{
					"zh_cn": map[string]any{
						"title": title,
						"content": [][]map[string]string{
							{{"tag": "text", "text": msg.Plain}},
						},
					},
				},
			},
		}
	case UsageAlertChannelCustom:
		payload = map[string]any{
			"title":             title,
			"text":              msg.Plain,
			"markdown":          msg.Markdown,
			"account_id":        account.ID,
			"account_name":      account.Name,
			"platform":          account.Platform,
			"account_type":      account.Type,
			"threshold_enabled": rule.ThresholdEnabled,
			"threshold_percent": rule.ThresholdPercent,
			"max_utilization":   maxUtil,
			"force_probe":       rule.ForceProbe,
			"channel":           channel,
		}
	default: // wecom
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": msg.WeCom,
			},
		}
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
		client = &http.Client{Timeout: usageAlertWebhookTimeout}
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

	switch strings.ToLower(strings.TrimSpace(channel)) {
	case UsageAlertChannelFeishu:
		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
			return fmt.Errorf("feishu webhook code=%d msg=%s", result.Code, result.Msg)
		}
	case UsageAlertChannelCustom:
		// Custom endpoints may return any 2xx body.
	default:
		var result struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(respBody, &result); err == nil && result.ErrCode != 0 {
			return fmt.Errorf("wecom webhook errcode=%d errmsg=%s", result.ErrCode, result.ErrMsg)
		}
	}
	return nil
}

func UsageAlertConfigFromAccount(account *Account) UsageAlertConfig {
	if account == nil {
		return UsageAlertConfig{Rules: []UsageAlertRule{}}
	}
	if rules, ok := parseUsageAlertRules(account.Extra[UsageAlertRulesExtraKey]); ok {
		return UsageAlertConfig{Rules: rules}
	}
	// Migrate legacy single wecom config.
	legacy := WeComUsageAlertConfigFromAccount(account)
	if !legacy.Enabled && legacy.WebhookURL == "" && legacy.Cron == "" {
		return UsageAlertConfig{Rules: []UsageAlertRule{}}
	}
	rule := UsageAlertRule{
		ID:         "legacy-wecom",
		Enabled:    legacy.Enabled,
		Channel:    UsageAlertChannelWeCom,
		WebhookURL: legacy.WebhookURL,
		Cron:       legacy.Cron,
		ForceProbe: legacy.ForceProbe,
		NextRunAt:  legacy.NextRunAt,
		LastRunAt:  legacy.LastRunAt,
		LastError:  legacy.LastError,
	}
	return UsageAlertConfig{Rules: []UsageAlertRule{rule}}
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

func parseUsageAlertRules(raw any) ([]UsageAlertRule, bool) {
	if raw == nil {
		return nil, false
	}
	var rules []UsageAlertRule
	switch v := raw.(type) {
	case []UsageAlertRule:
		return v, true
	case []any, map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		if err := json.Unmarshal(b, &rules); err != nil {
			return nil, false
		}
		return rules, true
	case json.RawMessage:
		if err := json.Unmarshal(v, &rules); err != nil {
			return nil, false
		}
		return rules, true
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		if err := json.Unmarshal([]byte(v), &rules); err != nil {
			return nil, false
		}
		return rules, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		if err := json.Unmarshal(b, &rules); err != nil {
			return nil, false
		}
		return rules, true
	}
}

func normalizeUsageAlertConfig(input, existing UsageAlertConfig, now time.Time) (UsageAlertConfig, error) {
	if len(input.Rules) > usageAlertMaxRules {
		return UsageAlertConfig{}, infraerrors.BadRequest("USAGE_ALERT_TOO_MANY_RULES", fmt.Sprintf("at most %d rules allowed", usageAlertMaxRules))
	}
	existingByID := make(map[string]UsageAlertRule, len(existing.Rules))
	for _, r := range existing.Rules {
		existingByID[r.ID] = r
	}
	out := UsageAlertConfig{Rules: make([]UsageAlertRule, 0, len(input.Rules))}
	seen := map[string]struct{}{}
	for _, raw := range input.Rules {
		rule, err := normalizeUsageAlertRule(raw, now, false)
		if err != nil {
			return UsageAlertConfig{}, err
		}
		if rule.ID == "" {
			rule.ID = newUsageAlertRuleID()
		}
		if _, dup := seen[rule.ID]; dup {
			return UsageAlertConfig{}, infraerrors.BadRequest("USAGE_ALERT_DUPLICATE_RULE_ID", "duplicate rule id: "+rule.ID)
		}
		seen[rule.ID] = struct{}{}

		quiet, _ := parseUsageAlertQuietHours(rule.QuietHours)
		if prev, ok := existingByID[rule.ID]; ok {
			cronChanged := prev.Cron != rule.Cron
			watchChanged := prev.ThresholdWatchCron != rule.ThresholdWatchCron || prev.ThresholdEnabled != rule.ThresholdEnabled
			if !cronChanged {
				rule.NextRunAt = prev.NextRunAt
			}
			if !watchChanged {
				rule.ThresholdNextRunAt = prev.ThresholdNextRunAt
			}
			rule.LastRunAt = prev.LastRunAt
			rule.LastThresholdAlertAt = prev.LastThresholdAlertAt
			rule.LastError = prev.LastError
			if rule.Enabled && (cronChanged || !prev.Enabled || rule.NextRunAt == nil) {
				if next, err := nextCronOutsideQuiet(rule.Cron, now, quiet); err == nil {
					rule.NextRunAt = &next
				}
			}
			if rule.Enabled && rule.ThresholdEnabled && (watchChanged || !prev.Enabled || rule.ThresholdNextRunAt == nil) {
				if next, err := nextCronOutsideQuiet(rule.ThresholdWatchCron, now, quiet); err == nil {
					rule.ThresholdNextRunAt = &next
				}
			}
		} else {
			if rule.Enabled && rule.NextRunAt == nil {
				if next, err := nextCronOutsideQuiet(rule.Cron, now, quiet); err == nil {
					rule.NextRunAt = &next
				}
			}
			if rule.Enabled && rule.ThresholdEnabled && rule.ThresholdNextRunAt == nil {
				if next, err := nextCronOutsideQuiet(rule.ThresholdWatchCron, now, quiet); err == nil {
					rule.ThresholdNextRunAt = &next
				}
			}
		}
		if !rule.Enabled {
			rule.NextRunAt = nil
			rule.ThresholdNextRunAt = nil
		}
		if !rule.ThresholdEnabled {
			rule.ThresholdNextRunAt = nil
			rule.LastThresholdAlertAt = nil
		}
		out.Rules = append(out.Rules, rule)
	}
	return out, nil
}

func normalizeUsageAlertRule(input UsageAlertRule, now time.Time, allowDisabledIncomplete bool) (UsageAlertRule, error) {
	out := UsageAlertRule{
		ID:                   strings.TrimSpace(input.ID),
		Enabled:              input.Enabled,
		Channel:              strings.ToLower(strings.TrimSpace(input.Channel)),
		WebhookURL:           strings.TrimSpace(input.WebhookURL),
		Cron:                 strings.TrimSpace(input.Cron),
		ForceProbe:           input.ForceProbe,
		ThresholdEnabled:     input.ThresholdEnabled,
		ThresholdPercent:     input.ThresholdPercent,
		ThresholdWatchCron:   strings.TrimSpace(input.ThresholdWatchCron),
		CooldownSeconds:       input.CooldownSeconds,
		QuietHours:           nil,
		NextRunAt:            input.NextRunAt,
		LastRunAt:            input.LastRunAt,
		ThresholdNextRunAt:   input.ThresholdNextRunAt,
		LastThresholdAlertAt: input.LastThresholdAlertAt,
		LastError:            strings.TrimSpace(input.LastError),
	}
	if out.Channel == "" {
		out.Channel = UsageAlertChannelWeCom
	}
	switch out.Channel {
	case UsageAlertChannelWeCom, UsageAlertChannelFeishu, UsageAlertChannelCustom:
	default:
		return out, infraerrors.BadRequest("USAGE_ALERT_INVALID_CHANNEL", "channel must be wecom, feishu, or custom")
	}

	quiet, quietErr := parseUsageAlertQuietHours(input.QuietHours)
	if quietErr != nil {
		return out, quietErr
	}
	out.QuietHours = formatUsageAlertQuietHours(quiet)

	if !out.Enabled && allowDisabledIncomplete {
		if !out.ThresholdEnabled {
			out.ThresholdPercent = 0
			out.ThresholdWatchCron = ""
			out.CooldownSeconds = 0
		}
		return out, nil
	}
	if out.Enabled || !allowDisabledIncomplete {
		if out.WebhookURL == "" {
			return out, infraerrors.BadRequest("USAGE_ALERT_WEBHOOK_REQUIRED", "webhook_url is required")
		}
		if err := validateUsageAlertWebhookURL(out.Channel, out.WebhookURL); err != nil {
			return out, err
		}
		if out.Cron == "" {
			return out, infraerrors.BadRequest("USAGE_ALERT_CRON_REQUIRED", "cron_expression is required")
		}
		if _, err := computeUsageAlertNextRun(out.Cron, now); err != nil {
			return out, infraerrors.BadRequest("USAGE_ALERT_INVALID_CRON", "invalid cron expression: "+err.Error())
		}
	}
	if out.ThresholdEnabled {
		if out.ThresholdPercent <= 0 || out.ThresholdPercent >= 100 {
			return out, infraerrors.BadRequest("USAGE_ALERT_INVALID_THRESHOLD", "threshold_percent must be an integer between 1 and 99")
		}
		if out.ThresholdWatchCron == "" {
			return out, infraerrors.BadRequest("USAGE_ALERT_WATCH_CRON_REQUIRED", "threshold_watch_cron is required when threshold is enabled")
		}
		if _, err := computeUsageAlertNextRun(out.ThresholdWatchCron, now); err != nil {
			return out, infraerrors.BadRequest("USAGE_ALERT_INVALID_WATCH_CRON", "invalid threshold_watch_cron: "+err.Error())
		}
		if out.CooldownSeconds <= 0 || out.CooldownSeconds > usageAlertMaxCooldownSeconds {
			return out, infraerrors.BadRequest("USAGE_ALERT_INVALID_COOLDOWN", fmt.Sprintf("cooldown_seconds must be between 1 and %d", usageAlertMaxCooldownSeconds))
		}
	} else {
		out.ThresholdPercent = 0
		out.ThresholdWatchCron = ""
		out.CooldownSeconds = 0
		out.ThresholdNextRunAt = nil
		out.LastThresholdAlertAt = nil
	}
	return out, nil
}

func usageAlertExtraUpdates(cfg UsageAlertConfig) map[string]any {
	rules := cfg.Rules
	if rules == nil {
		rules = []UsageAlertRule{}
	}
	return map[string]any{
		UsageAlertRulesExtraKey: rules,
		// Clear legacy single-rule keys so runner uses the new array only.
		WeComUsageAlertEnabledExtraKey:    false,
		WeComUsageAlertWebhookURLExtraKey: "",
		WeComUsageAlertCronExtraKey:       "",
		WeComUsageAlertForceProbeExtraKey: false,
		WeComUsageAlertNextRunAtExtraKey:  nil,
		WeComUsageAlertLastRunAtExtraKey:  nil,
		WeComUsageAlertLastErrorExtraKey:  "",
	}
}

func validateUsageAlertWebhookURL(channel, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "invalid webhook_url")
	}
	if u.Scheme != "https" {
		return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "webhook_url must use https")
	}
	host := strings.ToLower(u.Hostname())
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case UsageAlertChannelWeCom:
		if host != "qyapi.weixin.qq.com" {
			return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "wecom webhook host must be qyapi.weixin.qq.com")
		}
		if u.Path != "/cgi-bin/webhook/send" {
			return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "wecom webhook path must be /cgi-bin/webhook/send")
		}
		if strings.TrimSpace(u.Query().Get("key")) == "" {
			return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "wecom webhook_url must include key query parameter")
		}
	case UsageAlertChannelFeishu:
		if host != "open.feishu.cn" && host != "open.larksuite.com" {
			return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "feishu webhook host must be open.feishu.cn or open.larksuite.com")
		}
		if !strings.Contains(u.Path, "/open-apis/bot/v2/hook/") {
			return infraerrors.BadRequest("USAGE_ALERT_INVALID_WEBHOOK", "feishu webhook path must contain /open-apis/bot/v2/hook/")
		}
	case UsageAlertChannelCustom:
		// Any https URL is accepted.
	}
	return nil
}

func computeUsageAlertNextRun(cronExpr string, from time.Time) (time.Time, error) {
	sched, err := usageAlertCronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

type usageAlertQuietRange struct {
	StartSec int // seconds from midnight [0, 86400)
	EndSec   int // seconds from midnight (0, 86400]; exclusive end semantics via wrap
	Wrap     bool
	Raw      string
}

func parseUsageAlertQuietHours(raw []string) ([]usageAlertQuietRange, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > usageAlertMaxQuietRanges {
		return nil, infraerrors.BadRequest("USAGE_ALERT_TOO_MANY_QUIET_HOURS", fmt.Sprintf("at most %d quiet_hours ranges allowed", usageAlertMaxQuietRanges))
	}
	out := make([]usageAlertQuietRange, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, "-")
		if len(parts) != 2 {
			return nil, infraerrors.BadRequest("USAGE_ALERT_INVALID_QUIET_HOURS", "quiet_hours entries must look like HH:MM:SS-HH:MM:SS")
		}
		startSec, err := parseClockToSeconds(parts[0])
		if err != nil {
			return nil, infraerrors.BadRequest("USAGE_ALERT_INVALID_QUIET_HOURS", "invalid quiet_hours start: "+parts[0])
		}
		endSec, err := parseClockToSeconds(parts[1])
		if err != nil {
			return nil, infraerrors.BadRequest("USAGE_ALERT_INVALID_QUIET_HOURS", "invalid quiet_hours end: "+parts[1])
		}
		if startSec == endSec {
			return nil, infraerrors.BadRequest("USAGE_ALERT_INVALID_QUIET_HOURS", "quiet_hours start and end must differ")
		}
		r := usageAlertQuietRange{
			StartSec: startSec,
			EndSec:   endSec,
			Wrap:     startSec > endSec,
			Raw:      formatClock(startSec) + "-" + formatClock(endSec),
		}
		out = append(out, r)
	}
	return out, nil
}

func formatUsageAlertQuietHours(ranges []usageAlertQuietRange) []string {
	if len(ranges) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, r.Raw)
	}
	return out
}

func parseClockToSeconds(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("invalid clock")
	}
	var h, m, s int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0, err
	}
	if len(parts) == 3 {
		if _, err := fmt.Sscanf(parts[2], "%d", &s); err != nil {
			return 0, err
		}
	}
	if h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59 {
		return 0, fmt.Errorf("out of range")
	}
	return h*3600 + m*60 + s, nil
}

func formatClock(sec int) string {
	if sec < 0 {
		sec = 0
	}
	if sec >= 24*3600 {
		sec = 24*3600 - 1
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func secondsOfDay(t time.Time) int {
	t = t.In(time.Local)
	return t.Hour()*3600 + t.Minute()*60 + t.Second()
}

func isInQuietHours(t time.Time, ranges []usageAlertQuietRange) bool {
	if len(ranges) == 0 {
		return false
	}
	sec := secondsOfDay(t)
	for _, r := range ranges {
		if r.Wrap {
			// e.g. 22:00:00-06:00:00
			if sec >= r.StartSec || sec <= r.EndSec {
				return true
			}
			continue
		}
		// Inclusive both ends: 18:00:00-23:59:59 covers the whole evening.
		if sec >= r.StartSec && sec <= r.EndSec {
			return true
		}
	}
	return false
}

func quietPeriodEnd(t time.Time, ranges []usageAlertQuietRange) *time.Time {
	if !isInQuietHours(t, ranges) {
		return nil
	}
	loc := t.Location()
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	sec := secondsOfDay(t)
	var candidates []time.Time
	for _, r := range ranges {
		inRange := false
		if r.Wrap {
			inRange = sec >= r.StartSec || sec <= r.EndSec
		} else {
			inRange = sec >= r.StartSec && sec <= r.EndSec
		}
		if !inRange {
			continue
		}
		if r.Wrap {
			if sec >= r.StartSec {
				// After end seconds tomorrow (+1s so we leave the inclusive end).
				candidates = append(candidates, dayStart.Add(24*time.Hour).Add(time.Duration(r.EndSec+1)*time.Second))
			} else {
				candidates = append(candidates, dayStart.Add(time.Duration(r.EndSec+1)*time.Second))
			}
		} else {
			end := dayStart.Add(time.Duration(r.EndSec+1) * time.Second)
			candidates = append(candidates, end)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Before(best) {
			best = c
		}
	}
	return &best
}

// nextCronOutsideQuiet returns the next cron fire time that is outside quiet hours.
func nextCronOutsideQuiet(cronExpr string, from time.Time, ranges []usageAlertQuietRange) (time.Time, error) {
	if len(ranges) == 0 {
		return computeUsageAlertNextRun(cronExpr, from)
	}
	cursor := from
	for i := 0; i < 500; i++ {
		next, err := computeUsageAlertNextRun(cronExpr, cursor)
		if err != nil {
			return time.Time{}, err
		}
		if !isInQuietHours(next, ranges) {
			return next, nil
		}
		if end := quietPeriodEnd(next, ranges); end != nil {
			cursor = *end
			continue
		}
		cursor = next
	}
	return computeUsageAlertNextRun(cronExpr, from)
}

func newUsageAlertRuleID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("r%d", time.Now().UnixNano())
	}
	return "r" + hex.EncodeToString(b[:])
}

func maxUsageUtilization(usage *UsageInfo) float64 {
	if usage == nil {
		return 0
	}
	max := 0.0
	for _, w := range []*UsageProgress{usage.FiveHour, usage.SevenDay, usage.SevenDaySonnet, usage.SevenDayFable} {
		if w != nil && w.Utilization > max {
			max = w.Utilization
		}
	}
	return max
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

type usageAlertMessage struct {
	WeCom    string
	Markdown string
	Plain    string
}

type usageAlertWindowRow struct {
	Code     string
	Name     string
	Meaning  string
	Progress *UsageProgress
}

func buildUsageAlertMessage(
	account *Account,
	usage *UsageInfo,
	now time.Time,
	title string,
	thresholdEnabled bool,
	thresholdPercent int,
	maxUtil float64,
) usageAlertMessage {
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

	headerPlain := fmt.Sprintf(
		"账号：%s（#%d）\n平台：%s / %s\n时间：%s",
		name, id, platform, accountType, now.Format("2006-01-02 15:04:05"),
	)
	if thresholdEnabled {
		headerPlain += fmt.Sprintf("\n告警阈值：≥%d%%", thresholdPercent)
		if maxUtil >= float64(thresholdPercent) {
			headerPlain += "（已触发）"
		}
	}

	rows := collectUsageAlertWindowRows(usage)
	quotaTable := formatUsageAlertQuotaTable(rows)
	localTable := formatUsageAlertLocalTable(rows)

	var body strings.Builder
	if usage != nil && strings.TrimSpace(usage.Error) != "" {
		body.WriteString("警告：")
		body.WriteString(strings.TrimSpace(usage.Error))
		body.WriteString("\n\n")
	}
	if len(rows) == 0 {
		body.WriteString("暂无上游限流窗口数据（可能尚未采样，或该账号类型不支持）。")
	} else {
		body.WriteString("【上游限流使用率】\n")
		body.WriteString(quotaTable)
		if localTable != "" {
			body.WriteString("\n\n【本站窗口统计】\n")
			body.WriteString(localTable)
		}
		body.WriteString("\n\n说明：\n")
		body.WriteString("· 使用率：上游限流配额已用比例\n")
		body.WriteString("· 重置时间：该窗口配额下次恢复时间\n")
		if localTable != "" {
			body.WriteString("· 本站统计：本系统在窗口期内记录的请求/Token/成本（非上游原值）\n")
			body.WriteString("· 核算成本 / 用户成本：分别对应平台核算价与对用户计费\n")
		}
		for _, row := range rows {
			body.WriteString(fmt.Sprintf("· %s：%s\n", row.Name, row.Meaning))
		}
	}

	plain := truncateRunes(title+"\n\n"+headerPlain+"\n\n"+body.String(), usageAlertMarkdownMaxRunes)

	// WeCom markdown: title + quote header + fenced monospace tables.
	var wecom strings.Builder
	wecom.WriteString("**")
	wecom.WriteString(escapeWeComMarkdown(title))
	wecom.WriteString("**\n")
	wecom.WriteString(fmt.Sprintf("> 账号：%s（#%d）\n> 平台：%s / %s\n> 时间：%s\n",
		escapeWeComMarkdown(name), id, escapeWeComMarkdown(platform), escapeWeComMarkdown(accountType),
		now.Format("2006-01-02 15:04:05")))
	if thresholdEnabled {
		wecom.WriteString(fmt.Sprintf("> 告警阈值：≥%d%%", thresholdPercent))
		if maxUtil >= float64(thresholdPercent) {
			wecom.WriteString("（已触发）")
		}
		wecom.WriteByte('\n')
	}
	if usage != nil && strings.TrimSpace(usage.Error) != "" {
		wecom.WriteString("> 警告：")
		wecom.WriteString(escapeWeComMarkdown(strings.TrimSpace(usage.Error)))
		wecom.WriteByte('\n')
	}
	wecom.WriteByte('\n')
	if len(rows) == 0 {
		wecom.WriteString("暂无上游限流窗口数据（可能尚未采样，或该账号类型不支持）。")
	} else {
		wecom.WriteString("**上游限流使用率**\n")
		// Avoid ``` code fences: WeCom renders them in reddish syntax colors.
		wecom.WriteString(formatUsageAlertQuotaLines(rows))
		wecom.WriteByte('\n')
		if localTable != "" {
			wecom.WriteString("\n**本站窗口统计**\n")
			wecom.WriteString(formatUsageAlertLocalLines(rows))
			wecom.WriteByte('\n')
		}
		wecom.WriteString("\n使用率=上游限流已用比例；重置时间=配额下次恢复。")
		if localTable != "" {
			wecom.WriteString("本站统计来自本地用量日志。")
		}
	}
	wecomText := truncateRunes(wecom.String(), usageAlertMarkdownMaxRunes)

	// GFM markdown for custom webhooks / renderers that support tables.
	var md strings.Builder
	md.WriteString("# ")
	md.WriteString(title)
	md.WriteString("\n\n")
	md.WriteString(fmt.Sprintf("- **账号**：%s（#%d）\n- **平台**：%s / %s\n- **时间**：%s\n",
		name, id, platform, accountType, now.Format("2006-01-02 15:04:05")))
	if thresholdEnabled {
		md.WriteString(fmt.Sprintf("- **告警阈值**：≥%d%%", thresholdPercent))
		if maxUtil >= float64(thresholdPercent) {
			md.WriteString("（已触发）")
		}
		md.WriteByte('\n')
	}
	md.WriteByte('\n')
	if usage != nil && strings.TrimSpace(usage.Error) != "" {
		md.WriteString("> 警告：")
		md.WriteString(strings.TrimSpace(usage.Error))
		md.WriteString("\n\n")
	}
	if len(rows) == 0 {
		md.WriteString("暂无上游限流窗口数据（可能尚未采样，或该账号类型不支持）。\n")
	} else {
		md.WriteString("## 上游限流使用率\n\n")
		md.WriteString("| 窗口 | 含义 | 使用率 | 重置时间 |\n")
		md.WriteString("| --- | --- | ---: | --- |\n")
		for _, row := range rows {
			md.WriteString(fmt.Sprintf("| %s | %s | %.0f%% | %s |\n",
				row.Name, row.Meaning, row.Progress.Utilization, formatUsageAlertReset(row.Progress)))
		}
		if localTable != "" {
			md.WriteString("\n## 本站窗口统计\n\n")
			md.WriteString("| 窗口 | 请求数 | Token | 核算成本 | 用户成本 |\n")
			md.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
			for _, row := range rows {
				if row.Progress == nil || row.Progress.WindowStats == nil {
					continue
				}
				ws := row.Progress.WindowStats
				md.WriteString(fmt.Sprintf("| %s | %d | %s | $%.2f | $%.2f |\n",
					row.Name, ws.Requests, formatCompactTokenCount(ws.Tokens), ws.Cost, ws.UserCost))
			}
		}
		md.WriteString("\n**说明**\n\n")
		md.WriteString("- 使用率：上游限流配额已用比例\n")
		md.WriteString("- 重置时间：该窗口配额下次恢复时间\n")
		if localTable != "" {
			md.WriteString("- 本站统计：本系统在窗口期内记录的请求/Token/成本（非上游原值）\n")
			md.WriteString("- 核算成本 / 用户成本：分别对应平台核算价与对用户计费\n")
		}
	}

	return usageAlertMessage{
		WeCom:    wecomText,
		Markdown: truncateRunes(md.String(), usageAlertMarkdownMaxRunes),
		Plain:    plain,
	}
}

func collectUsageAlertWindowRows(usage *UsageInfo) []usageAlertWindowRow {
	if usage == nil {
		return nil
	}
	candidates := []usageAlertWindowRow{
		{Code: "5h", Name: "5小时", Meaning: "近 5 小时上游限流配额", Progress: usage.FiveHour},
		{Code: "7d", Name: "7天", Meaning: "近 7 天上游限流配额", Progress: usage.SevenDay},
		{Code: "7d S", Name: "7天·Sonnet", Meaning: "近 7 天 Sonnet 模型限流配额", Progress: usage.SevenDaySonnet},
		{Code: "7d F", Name: "7天·Fable", Meaning: "近 7 天 Fable 模型限流配额", Progress: usage.SevenDayFable},
	}
	out := make([]usageAlertWindowRow, 0, len(candidates))
	for _, row := range candidates {
		if row.Progress == nil {
			continue
		}
		out = append(out, row)
	}
	return out
}

func formatUsageAlertReset(progress *UsageProgress) string {
	if progress == nil {
		return "—"
	}
	if progress.ResetsAt != nil {
		return progress.ResetsAt.Format("01-02 15:04")
	}
	if progress.RemainingSeconds > 0 {
		return "约 " + formatDurationSeconds(progress.RemainingSeconds)
	}
	return "—"
}

func formatUsageAlertQuotaTable(rows []usageAlertWindowRow) string {
	if len(rows) == 0 {
		return ""
	}
	// Monospace table for plain/custom text channels.
	colWindow, colUtil, colReset := 12, 8, 14
	var b strings.Builder
	b.WriteString(padUsageAlertCell("窗口", colWindow))
	b.WriteString(padUsageAlertCell("使用率", colUtil))
	b.WriteString(padUsageAlertCell("重置时间", colReset))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("-", colWindow+colUtil+colReset))
	b.WriteByte('\n')
	for _, row := range rows {
		b.WriteString(padUsageAlertCell(row.Name, colWindow))
		b.WriteString(padUsageAlertCell(fmt.Sprintf("%.0f%%", row.Progress.Utilization), colUtil))
		b.WriteString(padUsageAlertCell(formatUsageAlertReset(row.Progress), colReset))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatUsageAlertQuotaLines is WeCom-friendly (no code fence; default black text).
func formatUsageAlertQuotaLines(rows []usageAlertWindowRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("窗口：%s　使用率：%.0f%%　重置：%s",
			row.Name, row.Progress.Utilization, formatUsageAlertReset(row.Progress)))
	}
	return b.String()
}

func formatUsageAlertLocalTable(rows []usageAlertWindowRow) string {
	hasStats := false
	for _, row := range rows {
		if row.Progress != nil && row.Progress.WindowStats != nil {
			hasStats = true
			break
		}
	}
	if !hasStats {
		return ""
	}
	colWindow, colReq, colTok, colCost, colUser := 12, 8, 10, 10, 10
	var b strings.Builder
	b.WriteString(padUsageAlertCell("窗口", colWindow))
	b.WriteString(padUsageAlertCell("请求数", colReq))
	b.WriteString(padUsageAlertCell("Token", colTok))
	b.WriteString(padUsageAlertCell("核算成本", colCost))
	b.WriteString(padUsageAlertCell("用户成本", colUser))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("-", colWindow+colReq+colTok+colCost+colUser))
	b.WriteByte('\n')
	for _, row := range rows {
		if row.Progress == nil || row.Progress.WindowStats == nil {
			continue
		}
		ws := row.Progress.WindowStats
		b.WriteString(padUsageAlertCell(row.Name, colWindow))
		b.WriteString(padUsageAlertCell(fmt.Sprintf("%d", ws.Requests), colReq))
		b.WriteString(padUsageAlertCell(formatCompactTokenCount(ws.Tokens), colTok))
		b.WriteString(padUsageAlertCell(fmt.Sprintf("$%.2f", ws.Cost), colCost))
		b.WriteString(padUsageAlertCell(fmt.Sprintf("$%.2f", ws.UserCost), colUser))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatUsageAlertLocalLines(rows []usageAlertWindowRow) string {
	var b strings.Builder
	n := 0
	for _, row := range rows {
		if row.Progress == nil || row.Progress.WindowStats == nil {
			continue
		}
		ws := row.Progress.WindowStats
		if n > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("窗口：%s　请求：%d　Token：%s　核算：$%.2f　用户：$%.2f",
			row.Name, ws.Requests, formatCompactTokenCount(ws.Tokens), ws.Cost, ws.UserCost))
		n++
	}
	return b.String()
}

func padUsageAlertCell(s string, width int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	n := 0
	for _, r := range runes {
		if r <= 0x7f {
			n++
		} else {
			n += 2 // CJK roughly double-width in monospace chat clients
		}
	}
	if n >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-n)
}

// formatUsageAlertMarkdown keeps a convenience wrapper for tests / callers needing WeCom body.
func formatUsageAlertMarkdown(
	account *Account,
	usage *UsageInfo,
	now time.Time,
	title string,
	thresholdEnabled bool,
	thresholdPercent int,
	maxUtil float64,
) string {
	return buildUsageAlertMessage(account, usage, now, title, thresholdEnabled, thresholdPercent, maxUtil).WeCom
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

// AccountHasDueUsageAlertRule reports whether any enabled rule is due
// for report cron and/or threshold watch cron.
func AccountHasDueUsageAlertRule(account *Account, now time.Time) bool {
	cfg := UsageAlertConfigFromAccount(account)
	for _, rule := range cfg.Rules {
		if !rule.Enabled {
			continue
		}
		if strings.TrimSpace(rule.WebhookURL) == "" {
			continue
		}
		if strings.TrimSpace(rule.Cron) != "" && (rule.NextRunAt == nil || !rule.NextRunAt.After(now)) {
			return true
		}
		if rule.ThresholdEnabled &&
			strings.TrimSpace(rule.ThresholdWatchCron) != "" &&
			(rule.ThresholdNextRunAt == nil || !rule.ThresholdNextRunAt.After(now)) {
			return true
		}
	}
	return false
}
