package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

// 国产供应商 payg（按量付费）账号余额探测服务。
//
// 仅覆盖有公开余额端点的供应商：
//   - Kimi/Moonshot：GET https://api.moonshot.cn/v1/users/me/balance (Bearer) → data.available_balance
//   - DeepSeek：     GET https://api.deepseek.com/user/balance (Bearer) → balance_infos[].total_balance + is_available
//
// 智谱（zhipu）无公开余额端点（OpenAPI 规格验证），仅靠响应式 429/402（见
// ratelimit_cn_providers.go）。解析逻辑对齐 cc-switch services/balance.rs::query_deepseek。
const (
	cnBalanceUpstreamTimeout = 15 * time.Second
	cnBalanceMaxBodyBytes    = 256 * 1024

	// Extra 余额快照键后缀（加 provider 前缀）。
	cnBalanceExtraSuffixBalance   = "balance"
	cnBalanceExtraSuffixCurrency  = "balance_currency"
	cnBalanceExtraSuffixAvailable = "balance_available" // deepseek is_available 健康标记
	cnBalanceExtraSuffixUpdated   = "balance_updated_at"
	cnBalanceExtraSuffixBalances  = "balances" // 多币种明细（deepseek USD+CNY）
)

// CNProviderBalanceEntry 是单一币种的余额明细。
type CNProviderBalanceEntry struct {
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"`
}

// CNProviderBalanceResult 是余额探测的返回结构（管理端 + UI 消费）。
type CNProviderBalanceResult struct {
	// owner is the immutable native credential/scheduler snapshot that produced
	// this result. It is never serialized to administrator responses.
	owner *Account

	Provider string `json:"provider"`
	Success  bool   `json:"success"`
	// Balance/Currency 为主币种（balance_infos 首条，兼容单币种消费方）；
	// 完整明细见 Balances（deepseek 双币种账号含 CNY + USD 两条）。
	Balance       float64                  `json:"balance"`
	Currency      string                   `json:"currency,omitempty"`
	Balances      []CNProviderBalanceEntry `json:"balances,omitempty"`
	BonusBalances []CNProviderBalanceEntry `json:"bonus_balances,omitempty"`
	Available     bool                     `json:"available"` // 健康标记（deepseek is_available；kimi 无此概念恒 true）
	StatusCode    int                      `json:"status_code,omitempty"`
	FetchedAt     int64                    `json:"fetched_at"`
	Persisted     bool                     `json:"persisted"`
	Error         string                   `json:"error,omitempty"`
}

// CNProviderBalanceService 探测 Kimi / DeepSeek payg 账号的账户余额。
type CNProviderBalanceService struct {
	accountRepo  AccountRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
	flight       singleflight.Group
}

// NewCNProviderBalanceService 构造余额探测服务。
func NewCNProviderBalanceService(
	accountRepo AccountRepository,
	_ ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *CNProviderBalanceService {
	return &CNProviderBalanceService{
		accountRepo:  accountRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

// QueryBalance 探测指定 payg 账号的余额并落 Extra 快照。
func (s *CNProviderBalanceService) QueryBalance(ctx context.Context, accountID int64) (*CNProviderBalanceResult, error) {
	account, err := s.loadPayGAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.QueryBalanceForAccount(ctx, account)
}

// QueryBalanceForAccount 探测已加载账号（配额监控 fetcher / 周期余额检测复用，
// 避免二次 GetByID）。原生 payg 查询只合并同一完整凭据及代理快照。
func (s *CNProviderBalanceService) QueryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_NOT_CONFIGURED", "cn provider balance service is not configured")
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	account, err := snapshotCNControlAccount(account)
	if err != nil {
		return nil, err
	}
	if cnBalanceRequiresOwner(account) {
		if account.IsShadow() || account.ID <= 0 || account.CredentialGeneration != "" || account.CNQuotaGeneration != "" {
			return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_OWNER_INVALID", "balance probe requires a hydrated credential-owning account")
		}
	}
	owner, err := grokRequestOwnerFingerprint(account)
	if err != nil {
		return nil, err
	}
	key := "cn_balance:" + strconv.FormatInt(account.ID, 10) + ":" + owner + ":" + account.Status
	probeIdentityCtx := ctx
	if cnBalanceRequiresOwner(account) {
		probeIdentityCtx = WithOutboundIdentityScope(probeIdentityCtx, nil)
		recordRequestCredentialOwner(probeIdentityCtx, account, account.GetCNAPIKey())
		probeIdentityCtx = withRequestCredentialOwner(probeIdentityCtx, account, account.GetCNAPIKey())
	}
	probeIdentityCtx = context.WithoutCancel(WithAccountOutboundIdentity(probeIdentityCtx, account))
	resultCh := s.flight.DoChan(key, func() (any, error) {
		probeCtx, cancel := context.WithTimeout(probeIdentityCtx, cnBalanceUpstreamTimeout+5*time.Second)
		defer cancel()
		return s.queryBalanceForAccount(probeCtx, account)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case flightResult := <-resultCh:
		if flightResult.Err != nil {
			return nil, flightResult.Err
		}
		result, ok := flightResult.Val.(*CNProviderBalanceResult)
		if !ok || result == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_PROBE_RESULT_INVALID", "invalid cn provider balance probe result")
		}
		cloned := *result
		cloned.Balances = append([]CNProviderBalanceEntry(nil), result.Balances...)
		cloned.BonusBalances = append([]CNProviderBalanceEntry(nil), result.BonusBalances...)
		cloned.owner = snapshotRequestCredentialAccount(result.owner)
		return &cloned, nil
	}
}

func (s *CNProviderBalanceService) queryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	if account.IsCommandCode() {
		return s.queryCommandCodeBalance(ctx, account)
	}
	if account.IsCline() {
		return s.queryClineBalance(ctx, account)
	}
	if account.IsDomesticOAuth() && account.Platform == PlatformDeepseek {
		return s.queryDeepSeekOAuthWallet(ctx, account)
	}
	provider := account.Platform
	if provider != PlatformKimi && provider != PlatformDeepseek {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_ENDPOINT", "account provider has no balance endpoint")
	}

	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_APIKEY", "account api_key is empty")
	}

	targetURL := cnBalanceURL(account)
	// 探测发起前过出站 URL 安全策略（与网关转发/Grok 探测同一套校验）：
	// DeepSeek 端点由账号 base_url 衍生，不得把 API key 发往策略外主机。
	validatedURL, err := cnValidateProbeURL(s.cfg, targetURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "CN_BALANCE_URL_REJECTED", err.Error())
	}
	targetURL = validatedURL
	proxyURL, err := account.StrictProxyURL()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), cnBalanceUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "CN_BALANCE_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	req = prepareAccountOutboundRequest(req, account)
	if err := OutboundPreparationError(req.Context()); err != nil {
		return nil, err
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CN_BALANCE_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, cnBalanceMaxBodyBytes+1))

	now := time.Now().UTC()
	result := &CNProviderBalanceResult{
		Provider:   provider,
		FetchedAt:  now.Unix(),
		StatusCode: resp.StatusCode,
		Available:  true,
		owner:      snapshotRequestCredentialAccount(account),
	}
	if readErr != nil || len(bodyBytes) > cnBalanceMaxBodyBytes {
		result.Error = "Balance response unavailable: incomplete or oversized body"
		return result, nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Error = fmt.Sprintf("Authentication failed (HTTP %d)", resp.StatusCode)
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(bodyBytes)), 240))
		return result, nil
	}
	if !json.Valid(bodyBytes) {
		result.Error = "Invalid balance response: malformed JSON"
		return result, nil
	}

	var entries []CNProviderBalanceEntry
	available := true
	switch provider {
	case PlatformKimi:
		// Moonshot：code==0 成功；data.available_balance（number），单币种 CNY。
		code := gjson.GetBytes(bodyBytes, "code")
		status := gjson.GetBytes(bodyBytes, "status")
		balance, validBalance := cnParseF64(gjson.GetBytes(bodyBytes, "data.available_balance").Value())
		if code.Type != gjson.Number || code.Num != 0 || status.Type != gjson.True || !validBalance {
			result.Error = "Invalid balance response: missing successful finite balance observation"
			return result, nil
		}
		entries = append(entries, CNProviderBalanceEntry{Currency: "CNY", Balance: balance})
	case PlatformDeepseek:
		// is_available 缺省视为 true（健康）；显式存在时取其值。
		if v := gjson.GetBytes(bodyBytes, "is_available"); v.Exists() {
			if v.Type != gjson.True && v.Type != gjson.False {
				result.Error = "Invalid balance response: invalid availability observation"
				return result, nil
			}
			available = v.Bool()
		}
		// balance_infos 逐条解析：双币种账号同时返回 CNY + USD（数组顺序即
		// 主次序，首条为主币种）。
		balanceInfos := gjson.GetBytes(bodyBytes, "balance_infos")
		if !balanceInfos.Exists() || !balanceInfos.IsArray() {
			result.Error = "Invalid balance response: missing balance_infos"
			return result, nil
		}
		invalidEntry := false
		balanceInfos.ForEach(func(_, info gjson.Result) bool {
			currencyNode := info.Get("currency")
			currency := currencyNode.String()
			totalBalance := info.Get("total_balance")
			balance, ok := cnParseF64(totalBalance.Value())
			if !info.IsObject() || currencyNode.Type != gjson.String || (currency != "CNY" && currency != "USD") || !totalBalance.Exists() || !ok {
				invalidEntry = true
				return false
			}
			entries = append(entries, CNProviderBalanceEntry{Currency: currency, Balance: balance})
			return true
		})
		if invalidEntry || len(entries) == 0 {
			result.Error = "Invalid balance response: no valid balance entries"
			return result, nil
		}
	}
	result.Balances = entries
	result.Balance = entries[0].Balance
	result.Currency = entries[0].Currency
	result.Available = available
	updates := cnBalanceExtraUpdates(provider, result, now)
	result.Persisted, err = persistCNControlExtra(ctx, s.accountRepo, account, updates)
	if err != nil || !result.Persisted {
		slog.Warn("cn_balance_persist_failed", "account_id", account.ID, "provider", provider, "error", err)
		result.Error = "Account wallet owner changed or snapshot persistence unavailable"
		// A valid response remains useful only for the same complete owner. A
		// newer cooldown may fence persistence without invalidating that owner's
		// observation; a replaced or unreadable grant cannot report success.
		result.Success = err == nil && s.balanceObservationStillOwned(ctx, account)
		result.owner = account
		return result, nil
	}
	result.Success = true
	result.owner = account
	return result, nil
}

func cnBalanceRequiresOwner(account *Account) bool {
	return account != nil && (account.Platform == PlatformKimi || account.Platform == PlatformDeepseek) && !account.IsCommandCode() && !account.IsCline()
}

// cnBalanceExtraUpdates 构造 provider 维度的余额快照更新。
func cnBalanceExtraUpdates(provider string, result *CNProviderBalanceResult, now time.Time) map[string]any {
	balanceUpdates := make([]any, 0, len(result.Balances))
	for _, entry := range result.Balances {
		balanceUpdates = append(balanceUpdates, map[string]any{
			"currency": entry.Currency,
			"balance":  entry.Balance,
		})
	}
	return map[string]any{
		cnExtraKey(provider, cnBalanceExtraSuffixBalance):   result.Balance,
		cnExtraKey(provider, cnBalanceExtraSuffixCurrency):  result.Currency,
		cnExtraKey(provider, cnBalanceExtraSuffixAvailable): result.Available,
		cnExtraKey(provider, cnBalanceExtraSuffixUpdated):   now.Format(time.RFC3339),
		cnExtraKey(provider, cnBalanceExtraSuffixBalances):  balanceUpdates,
		// 余额探测成功即清除响应式 402/429 写下的 balance_low 标记。
		cnExtraKey(provider, cnBalanceExtraSuffixLow): false,
	}
}

// loadPayGAccount 加载 payg 模式的国产供应商账号（余额仅对 payg 有意义；coding 走额度）。
func (s *CNProviderBalanceService) loadPayGAccount(ctx context.Context, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	return account, nil
}

// validatePayGAccount 加载后的非 DB 校验（ForAccount 入口同样复用，
// 保证直传 account 也不绕过平台/模式检查）。
func validatePayGAccount(account *Account) error {
	if account == nil {
		return infraerrors.New(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	// Cline 积分余额只查官方主机。
	if account.IsCline() {
		if !account.clineAccountAPISupported() {
			return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_ENDPOINT", "cline balance is only available for api key accounts on the official host")
		}
		return nil
	}
	// Command Code 积分余额与套餐窗口同源，只查官方主机。
	if account.IsCommandCode() {
		if !account.commandCodeUsageSupported() {
			return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_ENDPOINT", "command code balance is only available for api key accounts on the official host")
		}
		return nil
	}
	if !account.IsCNProvider() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_INVALID_PLATFORM", "account is not a CN provider account")
	}
	// coding 账号走额度探测，余额端点不适用。
	if account.IsCodingPlan() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_CODING_PLAN", "coding plan account has no balance endpoint; use quota probe")
	}
	return nil
}

// cnBalanceURL 解析账号的余额端点。
//
//   - Kimi：固定 https://api.moonshot.cn/v1/users/me/balance（与 base_url 无关，Moonshot 仅此一处）
//   - DeepSeek：基于 base_url 拼接 /user/balance（支持自定义域名）
func cnBalanceURL(account *Account) string {
	if account == nil {
		return ""
	}
	switch account.Platform {
	case PlatformKimi:
		return "https://api.moonshot.cn/v1/users/me/balance"
	case PlatformDeepseek:
		// Anthropic 协议账号的凭证 base_url 指向 /anthropic 端点，余额探测需回退
		// 到 OpenAI 格式 base（协议感知）再拼接 /user/balance。
		baseURL := strings.TrimRight(account.GetOpenAIFormatBaseURL(), "/")
		baseURL = strings.TrimSuffix(baseURL, "/v1")
		return baseURL + "/user/balance"
	default:
		return ""
	}
}

// Recheck only observation ownership after a rejected write. This read never
// authorizes a scheduling mutation; that remains a separate atomic CAS.
func (s *CNProviderBalanceService) balanceObservationStillOwned(ctx context.Context, expected *Account) bool {
	if expected == nil || s == nil || s.accountRepo == nil {
		return false
	}
	current, err := s.accountRepo.GetByID(ctx, expected.ID)
	if err != nil || current == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || current.Status != expected.Status || current.Schedulable != expected.Schedulable || current.CredentialGeneration != "" || current.CNQuotaGeneration != "" {
		return false
	}
	currentOwner, err := grokRequestOwnerFingerprint(current)
	if err != nil {
		return false
	}
	expectedOwner, err := grokRequestOwnerFingerprint(expected)
	return err == nil && currentOwner == expectedOwner
}
