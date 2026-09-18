package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
)

// NewOpenAIOAuthClient creates a new OpenAI OAuth client
func NewOpenAIOAuthClient() service.OpenAIOAuthClient {
	return &openaiOAuthService{
		tokenURL:          openai.TokenURL,
		revokeURL:         openai.RevokeURL,
		deviceAuthAPIBase: openai.DeviceAuthAPIBase,
	}
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
	client, err := createOpenAIReqClient(proxyURL)
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
	client, err := createOpenAIReqClient(proxyURL)
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
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Originator", originator).
		SetHeader("Content-Type", "application/json").
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

func createOpenAIReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL: proxyURL,
		Timeout:  120 * time.Second,
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
	client, err := createOpenAIReqClient(proxyURL)
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
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Originator", originator).
		SetHeader("Content-Type", "application/json").
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
	client, err := createOpenAIReqClient(proxyURL)
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
	client, err := createOpenAIReqClient(proxyURL)
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
