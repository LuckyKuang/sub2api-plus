package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
	"go.uber.org/zap"
)

// NewOpenAIOAuthClient creates a new OpenAI OAuth client
func NewOpenAIOAuthClient() service.OpenAIOAuthClient {
	tokenURL := resolveOpenAIOAuthURLOverride("CODEX_REFRESH_TOKEN_URL_OVERRIDE", openai.TokenURL)
	return &openaiOAuthService{
		tokenURL:          tokenURL,
		revokeURL:         resolveOpenAIOAuthRevokeURL(tokenURL),
		deviceAuthAPIBase: openai.DeviceAuthAPIBase,
	}
}

// resolveOpenAIOAuthURLOverride 读取官方 env 覆盖名；空值或非法值回退默认并告警。
func resolveOpenAIOAuthURLOverride(env, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return fallback
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		logger.L().Warn("openai_oauth_url_override_invalid",
			zap.String("component", "repository.openai_oauth"),
			zap.String("env", env),
			zap.String("value", raw),
			zap.String("fallback", fallback))
		return fallback
	}
	return raw
}

// codexRevokeTokenPath 是官方 revoke 端点的固定路径（官方 revoke.rs
// derive_revoke_token_endpoint 同样只改 path、清空 query）。
const codexRevokeTokenPath = "/oauth/revoke"

// openAICodexRevokeTimeout 对齐官方 auth/revoke.rs 的 REVOKE_HTTP_TIMEOUT：
// 吊销是登出/删号路径上的同步步骤，不该占用凭据面的 120s 预算。
const openAICodexRevokeTimeout = 10 * time.Second

// resolveOpenAIOAuthRevokeURL 按官方 revoke.rs 的三级解析得出吊销端点：显式
// CODEX_REVOKE_TOKEN_URL_OVERRIDE 优先；否则从已解析的 refresh 端点把 path
// 改写为 /oauth/revoke（同一台覆盖主机上的官方派生规则）；都没有则回退默认
// 主机。企业/测试环境只配了 refresh override 时，吊销必须打到同一主机。
func resolveOpenAIOAuthRevokeURL(refreshURL string) string {
	if raw := strings.TrimSpace(os.Getenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE")); raw != "" {
		return resolveOpenAIOAuthURLOverride("CODEX_REVOKE_TOKEN_URL_OVERRIDE", openai.RevokeURL)
	}
	if derived := deriveOpenAIOAuthRevokeURL(refreshURL); derived != "" {
		logger.L().Info("openai_oauth_revoke_url_derived_from_refresh_override",
			zap.String("component", "repository.openai_oauth"),
			zap.String("refresh_url", refreshURL),
			zap.String("revoke_url", derived))
		return derived
	}
	return openai.RevokeURL
}

// deriveOpenAIOAuthRevokeURL 把 refresh 端点改写为官方 revoke 端点；refresh
// 端点仍是官方默认地址时返回空串，让调用方回退默认 revoke 主机。
func deriveOpenAIOAuthRevokeURL(refreshURL string) string {
	trimmed := strings.TrimSpace(refreshURL)
	if trimmed == "" || trimmed == openai.TokenURL {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.Path = codexRevokeTokenPath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

type openaiOAuthService struct {
	tokenURL          string
	revokeURL         string
	deviceAuthAPIBase string
}

func (s *openaiOAuthService) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return s.exchangeCode(ctx, code, codeVerifier, redirectURI, proxyURL, clientID, "", "", "")
}

func (s *openaiOAuthService) ExchangeCodeWithUserAgent(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID, userAgent string) (*openai.TokenResponse, error) {
	return s.exchangeCode(ctx, code, codeVerifier, redirectURI, proxyURL, clientID, userAgent, "", "")
}

func (s *openaiOAuthService) ExchangeCodeWithIdentity(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID, userAgent, originator, version string) (*openai.TokenResponse, error) {
	return s.exchangeCode(ctx, code, codeVerifier, redirectURI, proxyURL, clientID, userAgent, originator, version)
}

func (s *openaiOAuthService) exchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID, userAgent, originator, version string) (*openai.TokenResponse, error) {
	// 换票走 raw client（无 cookie jar，官方 raw client 同样无 cookie）。
	client, err := createOpenAIRawReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	if redirectURI == "" {
		redirectURI = openai.DefaultRedirectURI
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}

	body := openai.EncodeAuthorizationCodeTokenBody(code, redirectURI, clientID, codeVerifier)

	var tokenResp openai.TokenResponse

	_, _, _ = userAgent, originator, version
	// Official Codex uses create_raw_auth_client for authorization-code
	// exchange: form five fields in official order, no User-Agent /
	// Originator / Version headers.
	resp, err := omitOpenAIRawAuthUserAgent(client.R()).
		SetContext(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(body).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, newOpenAINoProxyHintError(err)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}

	if !resp.IsSuccessState() {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_TOKEN_EXCHANGE_FAILED", "token exchange failed: status %d, body: %s", resp.StatusCode, resp.String())
	}

	return &tokenResp, nil
}

func (s *openaiOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, "", "", "", "")
}

func (s *openaiOAuthService) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID, "", "", "")
}

func (s *openaiOAuthService) RefreshTokenWithClientIDAndUserAgent(ctx context.Context, refreshToken, proxyURL, clientID, userAgent string) (*openai.TokenResponse, error) {
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID, userAgent, "", "")
}

func (s *openaiOAuthService) RefreshTokenWithClientIDAndIdentity(ctx context.Context, refreshToken, proxyURL, clientID, userAgent, originator, version string) (*openai.TokenResponse, error) {
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID, userAgent, originator, version)
}

func (s *openaiOAuthService) refreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID, userAgent, originator, version string) (*openai.TokenResponse, error) {
	// 调用方应始终传入正确的 client_id；为兼容旧数据，未指定时默认使用 OpenAI ClientID
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}
	client, err := createOpenAICredentialReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	refreshReq := openai.RefreshTokenRequest{
		ClientID:     clientID,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}

	var tokenResp openai.TokenResponse

	userAgent, originator, version = resolveOpenAIOAuthIdentity(userAgent, originator, version)
	_ = version
	request := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Originator", originator).
		SetHeader("Content-Type", "application/json")
	if value := openai.CodexResidencyFromContext(ctx); value != "" {
		request = request.SetHeader(openai.CodexResidencyHeader, value)
	}
	resp, err := request.
		SetBodyJsonMarshal(refreshReq).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, newOpenAINoProxyHintError(err)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}

	if !resp.IsSuccessState() {
		return nil, classifyOpenAIRefreshTokenFailure(resp.StatusCode, resp.String())
	}

	return &tokenResp, nil
}

func resolveOpenAIOAuthIdentity(userAgent, requestedOriginator, version string) (string, string, string) {
	// Normal service calls supply the already policy-approved identity tuple.
	// Accept a legacy member only when that explicit Originator capability agrees
	// byte-for-byte; old user-agent-only APIs therefore remain strict current
	// official-only and cannot accidentally reopen the migration set.
	profile, pairedUserAgent, ok := openai.PairConfiguredCodexClientIdentity(strings.TrimSpace(userAgent), true)
	if !ok && strings.TrimSpace(requestedOriginator) != "" {
		// Identity-aware callers already selected the client family. Outbound
		// versions may use the historical two-part syntax supported by the
		// settings resolver, which is deliberately separate from ingress SemVer.
		uaVersion := service.NormalizeCodexClientVersion(openai.CodexUserAgentVersion(userAgent))
		if uaVersion != "" && uaVersion == service.NormalizeCodexClientVersion(version) && service.CompareVersions(uaVersion, service.OpenAICodexUpstreamMinVersion) >= 0 {
			familyUA := openai.SetCodexUserAgentVersion(userAgent, service.DefaultOpenAICodexVersion)
			if candidate, _, valid := openai.PairConfiguredCodexClientIdentity(familyUA, true); valid && candidate.Originator == strings.TrimSpace(requestedOriginator) {
				profile, pairedUserAgent, ok = candidate, strings.TrimSpace(userAgent), true
			}
		}
	}
	if ok && (profile.Profile != openai.CodexClientProfileLegacyCompatibility || strings.TrimSpace(requestedOriginator) == profile.Originator) {
		pairedOriginator := profile.Originator
		resolvedVersion := service.NormalizeCodexClientVersion(version)
		if resolvedVersion == "" {
			resolvedVersion = service.NormalizeCodexClientVersion(openai.CodexUserAgentVersion(pairedUserAgent))
		}
		if resolvedVersion != "" && service.CompareVersions(resolvedVersion, service.OpenAICodexUpstreamMinVersion) >= 0 {
			if rebuilt := openai.SetCodexUserAgentVersion(pairedUserAgent, resolvedVersion); rebuilt != "" {
				pairedUserAgent = rebuilt
			}
			return pairedUserAgent, pairedOriginator, resolvedVersion
		}
	}
	defaultProfile, defaultUserAgent, ok := openai.PairConfiguredCodexClientIdentity(service.DefaultOpenAICodexUserAgent, false)
	if !ok {
		return service.DefaultOpenAICodexUserAgent, openai.CodexDefaultOriginator, service.DefaultOpenAICodexVersion
	}
	return defaultUserAgent, defaultProfile.Originator, service.DefaultOpenAICodexVersion
}

// createOpenAICredentialReqClient 是 OAuth 凭据面共享客户端（刷新/吊销/enrich/wham）：
// 启用对齐官方的 ChatGPT Cloudflare 基础设施 cookie jar（按代理隔离）。
func createOpenAICredentialReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL:          proxyURL,
		Timeout:           120 * time.Second,
		ChatGPTCookieJar:  true,
		OpenAICodexClient: true,
	})
}

// createOpenAIRawReqClient 是换票/device 等 raw 客户端：不启用 cookie jar
// （官方 raw client 同样无 cookie），保持干净的授权会话；官方 raw auth client
// 同样走自定义 CA 策略，因此仍标记为 Codex 客户端。
func createOpenAIRawReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL:          proxyURL,
		Timeout:           120 * time.Second,
		OpenAICodexClient: true,
	})
}

func omitOpenAIRawAuthUserAgent(r *req.Request) *req.Request {
	// req/v3 injects "req/v3 (...)" unless User-Agent is present and empty.
	return r.SetHeader("User-Agent", "")
}

func shouldReturnOpenAINoProxyHint(ctx context.Context, proxyURL string, err error) bool {
	if strings.TrimSpace(proxyURL) != "" || err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return !errors.Is(err, context.Canceled)
}

func newOpenAINoProxyHintError(cause error) error {
	return infraerrors.New(
		http.StatusBadGateway,
		"OPENAI_OAUTH_PROXY_REQUIRED",
		"OpenAI OAuth request failed: no proxy is configured and this server could not reach OpenAI directly. Select a proxy that can access OpenAI, then retry; if the authorization code has expired, regenerate the authorization URL.",
	).WithCause(cause)
}

func classifyOpenAIRefreshTokenFailure(status int, body string) error {
	code := extractOpenAIRefreshTokenErrorCode(body)
	isInvalidGrant := status == http.StatusBadRequest && strings.EqualFold(code, "invalid_grant")
	permanent := status == http.StatusUnauthorized || isPermanentOpenAIRefreshCode(code) || isInvalidGrant
	if permanent {
		return infraerrors.Newf(
			http.StatusUnauthorized,
			"OPENAI_OAUTH_REFRESH_PERMANENT",
			"token refresh permanently failed: status %d, code %s, body: %s",
			status,
			strings.TrimSpace(code),
			body,
		)
	}
	return infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", "token refresh failed: status %d, body: %s", status, body)
}

func isPermanentOpenAIRefreshCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated":
		return true
	default:
		return false
	}
}

func extractOpenAIRefreshTokenErrorCode(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	if errVal, ok := payload["error"]; ok {
		switch typed := errVal.(type) {
		case string:
			return strings.TrimSpace(typed)
		case map[string]any:
			if code, _ := typed["code"].(string); strings.TrimSpace(code) != "" {
				return strings.TrimSpace(code)
			}
		}
	}
	if code, _ := payload["code"].(string); strings.TrimSpace(code) != "" {
		return strings.TrimSpace(code)
	}
	return ""
}

func (s *openaiOAuthService) RevokeToken(ctx context.Context, token, tokenTypeHint, clientID, proxyURL, userAgent, originator string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	revokeURL := strings.TrimSpace(s.revokeURL)
	if revokeURL == "" {
		revokeURL = openai.RevokeURL
	}
	client, err := createOpenAICredentialReqClient(proxyURL)
	if err != nil {
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	hint := strings.TrimSpace(tokenTypeHint)
	if hint == "" {
		hint = "refresh_token"
	}
	body := openai.RevokeTokenRequest{
		Token:         token,
		TokenTypeHint: hint,
	}
	if hint == "refresh_token" {
		if clientID = strings.TrimSpace(clientID); clientID == "" {
			clientID = openai.ClientID
		}
		body.ClientID = clientID
	}
	userAgent, originator, _ = resolveOpenAIOAuthIdentity(userAgent, originator, "")
	// 官方 auth/revoke.rs 用 REVOKE_HTTP_TIMEOUT = 10s 单独约束吊销请求，
	// 避免卡死的吊销把登出/删号阻塞到凭据面的 120s。
	revokeCtx, cancelRevoke := context.WithTimeout(ctx, openAICodexRevokeTimeout)
	defer cancelRevoke()
	request := client.R().
		SetContext(revokeCtx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Originator", originator).
		SetHeader("Content-Type", "application/json")
	if value := openai.CodexResidencyFromContext(ctx); value != "" {
		request = request.SetHeader(openai.CodexResidencyHeader, value)
	}
	resp, err := request.
		SetBodyJsonMarshal(body).
		Post(revokeURL)
	if err != nil {
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REVOKE_FAILED", "revoke request failed: %v", err)
	}
	if resp != nil && !resp.IsSuccessState() {
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REVOKE_FAILED", "revoke failed: status %d, body: %s", resp.StatusCode, resp.String())
	}
	return nil
}

func (s *openaiOAuthService) StartDeviceCode(ctx context.Context, proxyURL, clientID string) (*openai.DeviceUserCodeResponse, error) {
	if clientID = strings.TrimSpace(clientID); clientID == "" {
		clientID = openai.ClientID
	}
	deviceAuthAPIBase := strings.TrimSpace(s.deviceAuthAPIBase)
	if deviceAuthAPIBase == "" {
		deviceAuthAPIBase = openai.DeviceAuthAPIBase
	}
	// device 流程走 raw client（无 cookie jar）。
	client, err := createOpenAIRawReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	var result openai.DeviceUserCodeResponse
	resp, err := omitOpenAIRawAuthUserAgent(client.R()).
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBodyJsonMarshal(openai.DeviceUserCodeRequest{ClientID: clientID}).
		SetSuccessResult(&result).
		Post(deviceAuthAPIBase + openai.DeviceAuthUserCodePath)
	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, newOpenAINoProxyHintError(err)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "device code request failed: %v", err)
	}
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_DEVICE_CODE_DISABLED", "device code login is not enabled for this OpenAI auth server")
	}
	if resp != nil && !resp.IsSuccessState() {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_DEVICE_CODE_FAILED", "device code request failed: status %d, body: %s", resp.StatusCode, resp.String())
	}
	if strings.TrimSpace(result.DeviceAuthID) == "" || strings.TrimSpace(result.UserCode) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_DEVICE_CODE_FAILED", "device code response missing device_auth_id or user_code")
	}
	if result.Interval <= 0 {
		result.Interval = 5
	}
	return &result, nil
}

func (s *openaiOAuthService) PollDeviceCode(ctx context.Context, proxyURL, deviceAuthID, userCode string) (*openai.DeviceTokenPollResponse, bool, error) {
	deviceAuthAPIBase := strings.TrimSpace(s.deviceAuthAPIBase)
	if deviceAuthAPIBase == "" {
		deviceAuthAPIBase = openai.DeviceAuthAPIBase
	}
	// device 轮询走 raw client（无 cookie jar）。
	client, err := createOpenAIRawReqClient(proxyURL)
	if err != nil {
		return nil, false, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	var result openai.DeviceTokenPollResponse
	resp, err := omitOpenAIRawAuthUserAgent(client.R()).
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBodyJsonMarshal(openai.DeviceTokenPollRequest{
			DeviceAuthID: deviceAuthID,
			UserCode:     userCode,
		}).
		SetSuccessResult(&result).
		Post(deviceAuthAPIBase + openai.DeviceAuthTokenPath)
	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, false, newOpenAINoProxyHintError(err)
		}
		return nil, false, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "device code poll failed: %v", err)
	}
	if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound) {
		return nil, true, nil
	}
	if resp != nil && !resp.IsSuccessState() {
		return nil, false, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_DEVICE_CODE_FAILED", "device code poll failed: status %d, body: %s", resp.StatusCode, resp.String())
	}
	if strings.TrimSpace(result.AuthorizationCode) == "" || strings.TrimSpace(result.CodeVerifier) == "" {
		return nil, false, infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_DEVICE_CODE_FAILED", "device code poll response missing authorization_code or code_verifier")
	}
	return &result, false, nil
}
