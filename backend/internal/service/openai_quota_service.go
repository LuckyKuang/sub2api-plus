package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/imroc/req/v3"
)

// ErrSparkShadowResetNotSupported is returned when ResetCredit is called on a
// spark shadow account. Shadow accounts do not hold credentials of their own;
// the caller must reset the parent account directly. It is a structured
// infraerrors value so the handler maps it to 409 Conflict (not a bare 500);
// errors.Is still matches it by identity since ResetCredit returns this var.
var ErrSparkShadowResetNotSupported = infraerrors.New(http.StatusConflict, "SPARK_SHADOW_RESET_NOT_SUPPORTED", "spark shadow account does not support credit reset; reset the parent account")

// Endpoints used by the OpenAI/ChatGPT/Codex quota query and reset feature.
const (
	chatGPTUsageURL            = "https://chatgpt.com/backend-api/wham/usage"
	chatGPTRateLimitCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	chatGPTRateLimitResetURL   = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	openaiQuotaUpstreamTimeout = 20 * time.Second
	openaiQuotaResetCreditsKey = "codex_reset_credit_snapshot"
	openaiQuotaCreditsKey      = "codex_credits_snapshot"
)

// OpenAIRateLimitWindow describes a single rate-limit window returned by
// /wham/usage. The upstream returns an explicit `null` window when the slot
// is unused, so consumers should treat a nil pointer as "no data".
type OpenAIRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
	missingUsedPercent bool
}

func (w *OpenAIRateLimitWindow) UnmarshalJSON(data []byte) error {
	type windowAlias OpenAIRateLimitWindow
	var raw struct {
		windowAlias
		UsedPercent *float64 `json:"used_percent"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*w = OpenAIRateLimitWindow(raw.windowAlias)
	w.missingUsedPercent = raw.UsedPercent == nil
	if raw.UsedPercent != nil {
		w.UsedPercent = *raw.UsedPercent
	}
	return nil
}

// OpenAIRateLimit is a rate-limit envelope (primary + optional secondary window).
type OpenAIRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *OpenAIRateLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *OpenAIRateLimitWindow `json:"secondary_window,omitempty"`
}

// OpenAIAdditionalRateLimit describes a per-feature rate limit (e.g. Codex Spark).
type OpenAIAdditionalRateLimit struct {
	LimitName      string           `json:"limit_name"`
	MeteredFeature string           `json:"metered_feature"`
	RateLimit      *OpenAIRateLimit `json:"rate_limit,omitempty"`
}

// OpenAIRateLimitResetCreditDetail is the sanitized metadata surfaced for one
// available reset credit. Do not add upstream ids or tokens here.
type OpenAIRateLimitResetCreditDetail struct {
	ExpiresAt string `json:"expires_at,omitempty"`
}

// OpenAIRateLimitResetCredits captures the "available_count" surfaced for the
// rate_limit_reset_credit grant type, which the reset action consumes.
type OpenAIRateLimitResetCredits struct {
	AvailableCount  int                                `json:"available_count"`
	Credits         []OpenAIRateLimitResetCreditDetail `json:"credits,omitempty"`
	credentialOwner *Account
}

// OpenAIQuotaUsage is the typed projection of /wham/usage we expose to the UI.
// Fields not relevant to the quota card are intentionally omitted to keep the
// surface narrow; full upstream payload preservation is unnecessary.
type OpenAIQuotaUsage struct {
	UserID                string                       `json:"user_id,omitempty"`
	AccountID             string                       `json:"account_id,omitempty"`
	Email                 string                       `json:"email,omitempty"`
	PlanType              string                       `json:"plan_type,omitempty"`
	RateLimit             *OpenAIRateLimit             `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []OpenAIAdditionalRateLimit  `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *OpenAIRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	Credits               *OpenAICredits               `json:"credits,omitempty"`
	FetchedAt             int64                        `json:"fetched_at"`
	autoResetCandidates   []openAIAutoResetCreditCandidate
	weeklyObservedAt      time.Time
	credentialOwner       *Account
	observationPersisted  bool
}

// OpenAICredits Codex 账户额度（/wham/usage 的 credits 投影）。
type OpenAICredits struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type openAICreditsSnapshot struct {
	Credits   *OpenAICredits `json:"credits"`
	FetchedAt int64          `json:"fetched_at"`
}

// CacheCreditsSnapshot 将本轮用量观测到的 Codex 额度写入账号 extra 缓存。
func (s *OpenAIQuotaService) CacheCreditsSnapshot(ctx context.Context, accountID int64, usage *OpenAIQuotaUsage) error {
	if usage == nil {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_EMPTY_USAGE", "openai quota query returned an empty result")
	}
	accepted, err := persistOpenAICodexUsageObservation(ctx, s.accountRepo, accountID, usage.credentialOwner, map[string]any{
		openaiQuotaCreditsKey: openAICreditsSnapshot{Credits: usage.Credits, FetchedAt: usage.FetchedAt},
	}, nil, false)
	if err != nil || !accepted {
		return infraerrors.New(http.StatusConflict, "OPENAI_QUOTA_CACHE_OWNER_CHANGED", "Codex credential owner changed before caching credits").WithCause(err)
	}
	return nil
}

// OpenAIQuotaResetCredit captures the redeemed credit metadata returned by the
// reset endpoint.
type OpenAIQuotaResetCredit struct {
	ID              string `json:"id,omitempty"`
	ResetType       string `json:"reset_type,omitempty"`
	Status          string `json:"status,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
}

// OpenAIQuotaResetResult is the typed projection of /wham/rate-limit-reset-credits/consume.
// The inner Credit also carries `redeemed_at` (RFC3339 string); we deliberately do
// NOT add a top-level redeemed_at to avoid ambiguity with the nested field.
type OpenAIQuotaResetResult struct {
	Code         string                  `json:"code"`
	Credit       *OpenAIQuotaResetCredit `json:"credit,omitempty"`
	WindowsReset int                     `json:"windows_reset"`
}

// OpenAIQuotaService queries and consumes ChatGPT/Codex rate-limit reset credits
// for OpenAI OAuth accounts. It reuses the backend-api client factory; identity
// headers follow the official backend-client surface and no browser TLS
// fingerprint is applied.
type OpenAIQuotaService struct {
	accountRepo            AccountRepository
	tokenProvider          *OpenAITokenProvider
	privacyClientFactory   PrivacyClientFactory
	referralClient         OpenAIReferralClient
	agentIdentityTaskMu    sync.Mutex
	agentIdentityWS        agentIdentityWSConnectionInvalidator
	openAIIdentityResolver *OpenAIGatewayService
}

func (s *OpenAIQuotaService) applyOpenAIOutboundIdentity(ctx context.Context, account *Account, headers map[string]string) openAIOutboundIdentity {
	if len(headers) == 0 {
		return openAIOutboundIdentity{}
	}
	h := make(http.Header, len(headers))
	for key, value := range headers {
		h.Set(key, value)
	}
	// Official backend-client WHAM headers are User-Agent + auth + account id.
	// Originator/Version belong on inference, not /wham/usage or credit APIs.
	var settings *SettingService
	var identity openAIOutboundIdentity
	if s != nil && s.openAIIdentityResolver != nil {
		identity = s.openAIIdentityResolver.applyOpenAIOutboundIdentity(ctx, account, h, false)
		settings = s.openAIIdentityResolver.settingService
	} else {
		identity = resolveOpenAIOutboundIdentityFromSettings(ctx, account, nil)
		applyResolvedOpenAIOutboundIdentity(h, identity, false)
	}
	// WHAM omits Originator/Version. Residency is still a managed Codex header.
	applyOpenAICodexResidencyFromSettings(ctx, settings, h, true)
	for key := range headers {
		delete(headers, key)
	}
	for key, values := range h {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	for key := range headers {
		if strings.EqualFold(key, openai.CodexResidencyHeader) && key != openai.CodexResidencyHeader {
			headers[openai.CodexResidencyHeader] = headers[key]
			delete(headers, key)
		}
	}
	return identity
}

func openAIQuotaOwnerFingerprint(account *Account) (string, error) {
	owner, err := snapshotCNControlAccount(account)
	if err != nil {
		return "", err
	}
	delete(owner.Credentials, "task_id")
	return cnControlOwnerFingerprint(owner)
}

func (s *OpenAIQuotaService) ensureOpenAIAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) error {
	expectedFingerprint, err := openAIQuotaOwnerFingerprint(account)
	if err != nil {
		return err
	}
	identity := resolveOpenAIOutboundIdentityFromSettings(ctx, account, nil)
	if s != nil && s.openAIIdentityResolver != nil {
		identity = s.openAIIdentityResolver.resolveOpenAIOutboundIdentity(ctx, account)
	}
	validateOwner := func(candidate *Account) error {
		fingerprint, err := openAIQuotaOwnerFingerprint(candidate)
		if err != nil || fingerprint != expectedFingerprint {
			return fmt.Errorf("quota credential owner changed during agent task recovery")
		}
		return nil
	}
	guard := agentIdentityTaskOwnerGuard{validate: validateOwner, persist: func(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
		return persistOpenAIQuotaCredentials(ctx, s.accountRepo, expected, credentials)
	}}
	return ensureAgentIdentityTaskForAccountWithIdentity(ctx, s.accountRepo, s.agentIdentityWS, &s.agentIdentityTaskMu, account, expectedTaskID, identity, guard)
}

// NewOpenAIQuotaService constructs a quota service. token provider is required —
// it ensures we always invoke upstream with a valid (refreshed-if-needed)
// access_token, sharing the same refresh/locking machinery used by the gateway.
func NewOpenAIQuotaService(
	accountRepo AccountRepository,
	_ ProxyRepository,
	tokenProvider *OpenAITokenProvider,
	privacyClientFactory PrivacyClientFactory,
) *OpenAIQuotaService {
	return &OpenAIQuotaService{
		accountRepo:          accountRepo,
		tokenProvider:        tokenProvider,
		privacyClientFactory: privacyClientFactory,
	}
}

// QueryUsage fetches the latest rate-limit/usage snapshot for the given OpenAI
// OAuth account. Returns infraerrors so the handler layer can map them to
// stable error codes / HTTP statuses.
func (s *OpenAIQuotaService) QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	return s.queryUsage(ctx, accountID, true)
}

func (s *OpenAIQuotaService) queryUsage(ctx context.Context, accountID int64, includeCredits bool) (*OpenAIQuotaUsage, error) {
	operation, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(operation.proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(operation.ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	agentIdentity := operation.owner.IsOpenAIAgentIdentity()

	var payload OpenAIQuotaUsage
	for recovered := false; ; {
		quotaHeaders, expectedTaskID, headerErr := s.buildCodexQuotaHeaders(callCtx, operation)
		if headerErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to build upstream authentication: %v", headerErr)
		}
		payload.weeklyObservedAt = time.Now().UTC()
		resp, err := client.R().
			SetContext(callCtx).
			SetHeaders(quotaHeaders).
			SetSuccessResult(&payload).
			Get(chatGPTUsageURL)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_REQUEST_FAILED", "upstream request failed: %v", err)
		}
		if !resp.IsSuccessState() {
			if agentIdentity && !recovered && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, []byte(resp.String())) {
				recovered = true
				if err := s.recoverQuotaAgentIdentityTask(callCtx, operation, expectedTaskID); err != nil {
					return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "agent identity task recovery failed: %v", err)
				}
				continue
			}
			status := resp.StatusCode
			if isOpenAIAutoResetContext(ctx) {
				slog.Warn("openai_quota_query_failed", "account_id", accountID, "status", status, "source", "auto_reset")
				return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_UPSTREAM_ERROR", "upstream returned %d", status)
			}
			body := truncate(s.redactQuotaErrorBody(callCtx, operation.owner, resp.String()), 240)
			slog.Warn("openai_quota_query_failed", "account_id", accountID, "status", status, "body", body)
			return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
		}
		break
	}

	payload.FetchedAt = time.Now().Unix()
	// Use the request start to reject old in-flight queries arriving late. The
	// same sample retains this timestamp through post-reset cache writes.
	snapshot := openAIQuotaUsageSnapshot(&payload, time.Now())
	payload.credentialOwner = snapshotRequestCredentialAccount(operation.owner)
	if !operation.selected.IsShadow() {
		if updates := buildCodexUsageExtraUpdates(snapshot, time.Now()); len(updates) > 0 {
			accepted, persistErr := persistOpenAICodexUsageObservation(callCtx, s.accountRepo, accountID, operation.owner, updates, snapshot, false)
			payload.observationPersisted = accepted && persistErr == nil
			if persistErr != nil {
				slog.Warn("openai_quota_usage_cache_failed", "account_id", accountID, "error", persistErr)
			}
		}
	}
	if !includeCredits {
		if payload.RateLimitResetCredits != nil {
			payload.RateLimitResetCredits.credentialOwner = snapshotRequestCredentialAccount(operation.owner)
		}
		return &payload, nil
	}
	details := s.queryResetCreditDetails(callCtx, client, operation)
	if details != nil {
		payload.autoResetCandidates = details.AutoResetCandidates
		hasDetailCount := details.AvailableCount != nil
		if payload.RateLimitResetCredits == nil {
			payload.RateLimitResetCredits = &OpenAIRateLimitResetCredits{}
		}
		if details.CreditListPresent {
			payload.RateLimitResetCredits.Credits = details.Credits
		}
		switch {
		case hasDetailCount:
			payload.RateLimitResetCredits.AvailableCount = *details.AvailableCount
		case details.CreditListPresent:
			payload.RateLimitResetCredits.AvailableCount = details.AvailableCreditCount
		}
	}
	if payload.RateLimitResetCredits != nil {
		payload.RateLimitResetCredits.credentialOwner = snapshotRequestCredentialAccount(operation.owner)
	}
	return &payload, nil
}

// CacheResetCreditsSnapshot persists a complete reset-credit snapshot after an
// explicit UI refresh. The snapshot is written to the account that was queried
// (for a spark shadow that is the shadow row, even though the credits belong to
// its parent) because it is a per-row display cache: each row caches exactly
// what its own card renders, and shadows cannot consume credits anyway.
//
// Missing expiration details leave the old cache intact:
// a snapshot claiming N>0 available credits without their expiration timestamps
// cannot be aged out by readers, so it would keep showing (and offering to
// consume) credits that already expired. Callers must treat this rejection as a
// partial success — the upstream read itself is still valid.
func (s *OpenAIQuotaService) CacheResetCreditsSnapshot(ctx context.Context, accountID int64, credits *OpenAIRateLimitResetCredits) error {
	return s.cacheResetCreditsSnapshot(ctx, accountID, credits, nil, nil)
}

// CachePostResetSnapshot persists the credits and usage windows observed after a reset.
func (s *OpenAIQuotaService) CachePostResetSnapshot(ctx context.Context, accountID int64, usage *OpenAIQuotaUsage) error {
	if usage == nil {
		return s.cacheResetCreditsSnapshot(ctx, accountID, nil, nil, nil)
	}
	var updates map[string]any
	var snapshot *OpenAICodexUsageSnapshot
	if !usage.observationPersisted {
		snapshot = openAIQuotaUsageSnapshot(usage, time.Now())
		updates = buildCodexUsageExtraUpdates(snapshot, time.Now())
	}
	return s.cacheResetCreditsSnapshot(ctx, accountID, usage.RateLimitResetCredits, updates, snapshot)
}

func (s *OpenAIQuotaService) cacheResetCreditsSnapshot(ctx context.Context, accountID int64, credits *OpenAIRateLimitResetCredits, updates map[string]any, snapshot *OpenAICodexUsageSnapshot) error {
	if credits == nil || (credits.AvailableCount > 0 && len(credits.Credits) == 0) {
		return infraerrors.New(
			http.StatusBadGateway,
			"OPENAI_QUOTA_RESET_CREDITS_REFRESH_FAILED",
			"failed to refresh reset-credit expiration details; cached data was preserved",
		)
	}
	if updates == nil {
		updates = make(map[string]any, 1)
	}
	updates[openaiQuotaResetCreditsKey] = credits
	accepted, err := persistOpenAICodexUsageObservation(ctx, s.accountRepo, accountID, credits.credentialOwner, updates, snapshot, false)
	if err != nil || !accepted {
		return infraerrors.New(
			http.StatusInternalServerError,
			"OPENAI_QUOTA_CACHE_WRITE_FAILED",
			"failed to cache reset-credit details",
		).WithCause(err)
	}
	return nil
}

func (s *OpenAIQuotaService) queryResetCreditDetails(ctx context.Context, client *req.Client, operation *openAIQuotaOperation) *openAIRateLimitResetCreditDetails {
	accountID := operation.selected.ID
	quotaHeaders, _, headerErr := s.buildCodexQuotaHeaders(ctx, operation)
	if headerErr != nil {
		slog.Warn("openai_quota_reset_credit_details_auth_failed", "account_id", accountID, "error", headerErr)
		return nil
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeaders(quotaHeaders).
		Get(chatGPTRateLimitCreditsURL)
	if err != nil {
		slog.Warn("openai_quota_reset_credit_details_failed", "account_id", accountID, "error", err)
		return nil
	}
	if !resp.IsSuccessState() {
		slog.Warn("openai_quota_reset_credit_details_failed", "account_id", accountID, "status", resp.StatusCode)
		return nil
	}

	details, err := parseOpenAIRateLimitResetCreditDetails(resp.Bytes())
	if err != nil {
		slog.Warn("openai_quota_reset_credit_details_parse_failed", "account_id", accountID, "error", err)
		if details.AvailableCount == nil {
			return nil
		}
	}
	if details.AvailableCount == nil && !details.CreditListPresent {
		return nil
	}
	return &details
}

// ResetCredit consumes one rate_limit_reset_credit for the given OpenAI account.
// The redeem_request_id is auto-generated (uuid-like) — upstream uses it for
// idempotency. Returns the consumed credit metadata so the UI can refresh.
func (s *OpenAIQuotaService) ResetCredit(ctx context.Context, accountID int64) (*OpenAIQuotaResetResult, error) {
	redeemRequestID, err := generateRedeemRequestID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_QUOTA_REDEEM_ID_FAILED", "failed to generate redeem id: %v", err)
	}
	return s.resetCredit(ctx, accountID, "", redeemRequestID, false)
}

// ResetCreditTargeted 使用固定卡 ID 与兑换 ID执行自动消费。调用方必须在重试时
// 复用同一组参数；本方法不会回退到不带 credit_id 的旧消费方式。
func (s *OpenAIQuotaService) ResetCreditTargeted(ctx context.Context, accountID int64, creditID, redeemRequestID string) (*OpenAIQuotaResetResult, error) {
	creditID = strings.TrimSpace(creditID)
	redeemRequestID = strings.TrimSpace(redeemRequestID)
	if creditID == "" || redeemRequestID == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_TARGETED_RESET_INVALID", "credit_id and redeem_request_id are required")
	}
	return s.resetCredit(ctx, accountID, creditID, redeemRequestID, true)
}

func (s *OpenAIQuotaService) resetCredit(ctx context.Context, accountID int64, creditID, redeemRequestID string, targeted bool) (*OpenAIQuotaResetResult, error) {
	// Shadow guard: resetting credits via a shadow account would silently
	// operate on the parent's quota; that is surprising and unwanted. Callers
	// must reset the parent account directly.
	//
	// Fail-closed: if the account cannot be loaded (transient DB error), we
	// must NOT fall through to prepareUpstreamCall. That function resolves a
	// shadow to its parent and would perform a parent-level reset — exactly
	// what this guard must prevent. Return the load error instead.
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}
	selected, loadErr := s.accountRepo.GetByID(ctx, accountID)
	if loadErr != nil || selected == nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", loadErr)
	}
	if selected.IsShadow() {
		return nil, ErrSparkShadowResetNotSupported
	}
	operation, err := s.prepareUpstreamAccountCall(ctx, selected)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(operation.proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(operation.ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	agentIdentity := operation.owner.IsOpenAIAgentIdentity()

	var payload OpenAIQuotaResetResult
	for recovered := false; ; {
		headers, expectedTaskID, headerErr := s.buildCodexQuotaHeaders(callCtx, operation)
		if headerErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to build upstream authentication: %v", headerErr)
		}
		headers["content-type"] = "application/json"
		body := map[string]string{"redeem_request_id": redeemRequestID}
		if targeted {
			body["credit_id"] = creditID
		}
		resp, err := client.R().
			SetContext(callCtx).
			SetHeaders(headers).
			SetBody(body).
			SetSuccessResult(&payload).
			Post(chatGPTRateLimitResetURL)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_RESET_REQUEST_FAILED", "upstream request failed: %v", err)
		}
		if !resp.IsSuccessState() {
			if agentIdentity && !recovered && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, []byte(resp.String())) {
				recovered = true
				if err := s.recoverQuotaAgentIdentityTask(callCtx, operation, expectedTaskID); err != nil {
					return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "agent identity task recovery failed: %v", err)
				}
				continue
			}
			status := resp.StatusCode
			if targeted {
				slog.Warn("openai_quota_targeted_reset_failed", "account_id", accountID, "status", status)
				return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_RESET_UPSTREAM_ERROR", "upstream returned %d", status)
			}
			body := truncate(s.redactQuotaErrorBody(callCtx, operation.owner, resp.String()), 240)
			slog.Warn("openai_quota_reset_failed", "account_id", accountID, "status", status, "body", body)
			return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_RESET_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
		}
		break
	}

	slog.Info("openai_quota_reset_success",
		"account_id", accountID,
		"code", payload.Code,
		"windows_reset", payload.WindowsReset,
	)
	return &payload, nil
}

// openAIQuotaOperation binds all calls in a quota/reset/referral operation to
// the credentials and route that supplied authentication. A later account edit
// affects the next operation, never this operation's headers or retry route.
type openAIQuotaOperation struct {
	ctx                                     context.Context
	selected, owner                         *Account
	accessToken, chatGPTAccountID, proxyURL string
	fedRAMP                                 bool
}

func (s *OpenAIQuotaService) prepareUpstreamCall(ctx context.Context, accountID int64) (*openAIQuotaOperation, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	return s.prepareUpstreamAccountCall(ctx, account)
}

func (s *OpenAIQuotaService) prepareUpstreamAccountCall(ctx context.Context, selected *Account) (*openAIQuotaOperation, error) {
	if s == nil || s.accountRepo == nil || s.privacyClientFactory == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}
	if selected == nil || selected.Platform != PlatformOpenAI {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_PLATFORM", "account is not an OpenAI account")
	}
	if selected.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_TYPE", "account is not an OAuth account")
	}
	selected = snapshotRequestCredentialAccount(selected)
	if expected := codexObservationOwner(ctx, selected.ID); expected != nil && credentialStateGeneration(expected) != credentialStateGeneration(selected) {
		return nil, infraerrors.New(http.StatusConflict, "OPENAI_QUOTA_CREDENTIAL_CHANGED", "credential owner changed during quota operation")
	}
	account := selected
	if account.IsShadow() {
		var err error
		account, err = resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil || account == nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_SHADOW_RESOLVE_FAILED", "failed to resolve shadow account: %v", err)
		}
	}
	account = snapshotRequestCredentialAccount(account)
	// A bound operation requires the proxy snapshot captured with its owner.
	// A later lookup cannot repair a missing snapshot or authorize a new route.
	// Validate before token refresh can perform egress.
	if _, err := requestCredentialOwnerProxyURL(context.Background(), account); err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_PROXY_UNAVAILABLE", "configured account proxy is unavailable").WithCause(err)
	}
	ctx = WithOutboundIdentityScope(ctx, nil)
	var token string
	if account.IsOpenAIAgentIdentity() {
		if err := s.ensureOpenAIAgentIdentityTask(ctx, account, ""); err != nil {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to acquire agent task").WithCause(err)
		}
		recordRequestCredentialOwner(ctx, account, "")
	} else {
		if s.tokenProvider == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota token provider is not configured")
		}
		var err error
		ctx, err = withOpenAIQuotaTokenOwner(ctx, account)
		if err != nil {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_ACCOUNT_INVALID", "cannot capture quota token owner").WithCause(err)
		}
		token, err = s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil || strings.TrimSpace(token) == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "failed to acquire access token").WithCause(err)
		}
	}
	ctx = withRequestCredentialOwner(ctx, selected, token)
	owner, err := requestCredentialAccount(ctx, selected)
	if err != nil || owner == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_CREDENTIAL_CHANGED", "credential owner is unavailable").WithCause(err)
	}
	if expected := codexObservationOwner(ctx, owner.ID); expected != nil && credentialStateGeneration(expected) != credentialStateGeneration(owner) {
		return nil, infraerrors.New(http.StatusConflict, "OPENAI_QUOTA_CREDENTIAL_CHANGED", "credential owner changed while acquiring quota authentication")
	}
	proxyURL, err := requestCredentialOwnerProxyURL(ctx, owner)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_PROXY_UNAVAILABLE", "configured account proxy is unavailable").WithCause(err)
	}
	chatGPTAccountID := strings.TrimSpace(owner.GetCredential("chatgpt_account_id"))
	if chatGPTAccountID == "" {
		chatGPTAccountID = strings.TrimSpace(owner.GetCredential("organization_id"))
	}
	if chatGPTAccountID == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_MISSING_ACCOUNT_ID", "chatgpt_account_id is missing; please re-authorize this account")
	}
	// Resolve settings now so client creation and subsequent retries cannot
	// substitute a different configured identity halfway through an operation.
	s.applyOpenAIOutboundIdentity(ctx, owner, map[string]string{"accept": "application/json"})
	return &openAIQuotaOperation{ctx: ctx, selected: selected, owner: snapshotRequestCredentialAccount(owner), accessToken: token, chatGPTAccountID: chatGPTAccountID, proxyURL: proxyURL, fedRAMP: owner.IsChatGPTAccountFedRAMP()}, nil
}

func (s *OpenAIQuotaService) recoverQuotaAgentIdentityTask(ctx context.Context, operation *openAIQuotaOperation, expectedTaskID string) error {
	before := snapshotRequestCredentialAccount(operation.owner)
	recovered := snapshotRequestCredentialAccount(before)
	if err := s.ensureOpenAIAgentIdentityTask(ctx, recovered, expectedTaskID); err != nil {
		return err
	}
	if !sameAgentIdentityTaskGrant(before, recovered) {
		return fmt.Errorf("agent identity task recovery changed credential owner")
	}
	recordRequestCredentialOwner(operation.ctx, recovered, "")
	next := withRequestCredentialOwner(operation.ctx, operation.selected, "")
	owner, err := requestCredentialAccount(next, operation.selected)
	if err != nil || !sameRequestCredentialGrant(owner, recovered) {
		return fmt.Errorf("agent identity recovered owner is unavailable")
	}
	operation.ctx, operation.owner = next, owner
	return nil
}

func (s *OpenAIQuotaService) buildCodexQuotaHeaders(ctx context.Context, operation *openAIQuotaOperation) (map[string]string, string, error) {
	if operation == nil || operation.owner == nil {
		return nil, "", fmt.Errorf("quota credential owner is unavailable")
	}
	account := operation.owner
	headers := buildCodexCommonHeaders(operation.accessToken, operation.chatGPTAccountID, operation.fedRAMP)
	if !account.IsOpenAIAgentIdentity() {
		s.applyOpenAIOutboundIdentity(operation.ctx, account, headers)
		return headers, "", nil
	}
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return nil, "", err
	}
	assertion, err := buildAgentAssertion(key, time.Now())
	if err != nil {
		return nil, "", err
	}
	headers["authorization"] = assertion
	s.applyOpenAIOutboundIdentity(operation.ctx, account, headers)
	return headers, key.taskID, nil
}

func (s *OpenAIQuotaService) redactQuotaErrorBody(ctx context.Context, owner *Account, body string) string {
	return string(redactAgentIdentitySensitiveBodyForAccount(ctx, s.accountRepo, owner, []byte(body)))
}

// buildCodexCommonHeaders sets the request headers expected by the chatgpt.com
// backend so calls succeed past Cloudflare/WASM checks.
func buildCodexCommonHeaders(accessToken, chatGPTAccountID string, fedRAMP bool) map[string]string {
	headers := map[string]string{
		"authorization":      "Bearer " + accessToken,
		"chatgpt-account-id": chatGPTAccountID,
		"accept":             "application/json",
	}
	if fedRAMP {
		headers["x-openai-fedramp"] = "true"
	}
	return headers
}

// generateRedeemRequestID produces a UUID-v4-shaped string without pulling in a
// new dependency. ChatGPT uses this as an idempotency key for the consume call.
func generateRedeemRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Set version (4) and variant (RFC 4122) bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:]), nil
}

// buildCodexSparkWindowExtraUpdates extracts Codex Spark usage windows from the
// /wham/usage response body's additional_rate_limits, matching the entry with
// MeteredFeature == "codex_bengalfox". It produces plain codex_* keys (NOT the
// Method-Z "codex_spark_" prefix) so that a spark shadow account's extra map
// is populated with the same key names used by the scheduling / frontend layers.
// Returns nil when no codex_bengalfox entry is present or when the RateLimit
// yields no window data.
func buildCodexSparkWindowExtraUpdates(usage *OpenAIQuotaUsage, now time.Time) map[string]any {
	if usage == nil {
		return nil
	}
	var spark *OpenAIRateLimit
	for i := range usage.AdditionalRateLimits {
		a := usage.AdditionalRateLimits[i]
		if a.MeteredFeature == "codex_bengalfox" {
			spark = a.RateLimit
			break
		}
	}
	if spark == nil {
		return nil
	}

	// Reuse OpenAICodexUsageSnapshot / Normalize to map primary/secondary windows
	// to canonical 5h/7d buckets (same logic as probeOpenAICodexSnapshot).
	snap := &OpenAICodexUsageSnapshot{}
	if w := spark.PrimaryWindow; w != nil {
		p := w.UsedPercent
		snap.PrimaryUsedPercent = &p
		ra := int(w.ResetAfterSeconds)
		snap.PrimaryResetAfterSeconds = &ra
		wm := int(w.LimitWindowSeconds / 60)
		snap.PrimaryWindowMinutes = &wm
	}
	if w := spark.SecondaryWindow; w != nil {
		p := w.UsedPercent
		snap.SecondaryUsedPercent = &p
		ra := int(w.ResetAfterSeconds)
		snap.SecondaryResetAfterSeconds = &ra
		wm := int(w.LimitWindowSeconds / 60)
		snap.SecondaryWindowMinutes = &wm
	}

	normalized := snap.Normalize()
	if normalized == nil {
		return nil
	}

	updates := make(map[string]any)
	if normalized.Used5hPercent != nil {
		updates["codex_5h_used_percent"] = *normalized.Used5hPercent
	}
	if normalized.Reset5hSeconds != nil {
		updates["codex_5h_reset_after_seconds"] = *normalized.Reset5hSeconds
	}
	if normalized.Window5hMinutes != nil {
		updates["codex_5h_window_minutes"] = *normalized.Window5hMinutes
	}
	if normalized.Used7dPercent != nil {
		updates["codex_7d_used_percent"] = *normalized.Used7dPercent
	}
	if normalized.Reset7dSeconds != nil {
		updates["codex_7d_reset_after_seconds"] = *normalized.Reset7dSeconds
	}
	if normalized.Window7dMinutes != nil {
		updates["codex_7d_window_minutes"] = *normalized.Window7dMinutes
	}
	if r := codexResetAtRFC3339(now, normalized.Reset5hSeconds); r != nil {
		updates["codex_5h_reset_at"] = *r
	}
	if r := codexResetAtRFC3339(now, normalized.Reset7dSeconds); r != nil {
		updates["codex_7d_reset_at"] = *r
	}
	if len(updates) == 0 {
		return nil
	}
	updates["codex_usage_updated_at"] = now.Format(time.RFC3339)
	return updates
}

// mapUpstreamStatus collapses upstream HTTP statuses into a stable set we
// surface from the admin handler. 4xx upstream errors are surfaced as 502
// (BadGateway) so callers can distinguish "your input is bad" (400) from
// "upstream said no" (502); 401/403 are bubbled directly to hint at re-auth.
func mapUpstreamStatus(status int) int {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return status
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status >= 400 && status < 500:
		return http.StatusBadGateway
	case status >= 500:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}
