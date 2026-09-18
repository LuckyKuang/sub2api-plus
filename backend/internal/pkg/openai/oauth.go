package openai

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenAI OAuth Constants (from CRS project - Codex CLI client)
const (
	// OAuth Client ID for OpenAI (Codex CLI official)
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// OAuth endpoints
	AuthorizeURL = "https://auth.openai.com/oauth/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token"
	RevokeURL    = "https://auth.openai.com/oauth/revoke"

	// Device-code login (codex-rs login/src/device_code_auth.rs)
	DeviceAuthAPIBase      = "https://auth.openai.com/api/accounts"
	DeviceVerificationURL  = "https://auth.openai.com/codex/device"
	DeviceCodeRedirectURI  = "https://auth.openai.com/deviceauth/callback"
	DeviceAuthUserCodePath = "/deviceauth/usercode"
	DeviceAuthTokenPath    = "/deviceauth/token"

	// Default redirect URI (can be customized)
	DefaultRedirectURI = "http://localhost:1455/auth/callback"

	// Scopes match official Codex CLI authorize URL.
	DefaultScopes = "openid profile email offline_access api.connectors.read api.connectors.invoke"

	// Session TTL
	SessionTTL = 30 * time.Minute
)

const (
	// OAuthPlatformOpenAI uses OpenAI Codex-compatible OAuth client.
	OAuthPlatformOpenAI = "openai"
)

// OAuthSession stores OAuth flow state for OpenAI
type OAuthSession struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	ClientID     string `json:"client_id,omitempty"`
	// AccountID is set only for a re-authorization flow. It is intentionally
	// server-side session state so the code exchange cannot be redirected to a
	// different account by a browser request.
	AccountID      *int64    `json:"account_id,omitempty"`
	ProxyURL       string    `json:"proxy_url,omitempty"`
	RedirectURI    string    `json:"redirect_uri"`
	CreatedAt      time.Time `json:"created_at"`
	DeviceAuthID   string    `json:"device_auth_id,omitempty"`
	DeviceUserCode string    `json:"device_user_code,omitempty"`
}

// SessionStore manages OAuth sessions in memory
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*OAuthSession
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewSessionStore creates a new session store
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*OAuthSession),
		stopCh:   make(chan struct{}),
	}
	// Start cleanup goroutine
	go store.cleanup()
	return store
}

// Set stores a session
func (s *SessionStore) Set(sessionID string, session *OAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
}

// Get retrieves a session
func (s *SessionStore) Get(sessionID string) (*OAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}
	// Check if expired
	if time.Since(session.CreatedAt) > SessionTTL {
		return nil, false
	}
	return session, true
}

// Delete removes a session
func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// Stop stops the cleanup goroutine
func (s *SessionStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// cleanup removes expired sessions periodically
func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			for id, session := range s.sessions {
				if time.Since(session.CreatedAt) > SessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

// GenerateRandomBytes generates cryptographically secure random bytes
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// GenerateState generates a random state string for OAuth.
// Official Codex encodes 32 random bytes as base64url without padding.
func GenerateState() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateSessionID generates a unique session ID
func GenerateSessionID() (string, error) {
	bytes, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateCodeVerifier generates a PKCE code verifier.
// Official Codex encodes 64 random bytes as base64url without padding.
func GenerateCodeVerifier() (string, error) {
	bytes, err := GenerateRandomBytes(64)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateCodeChallenge generates a PKCE code challenge using S256 method
// Uses base64url encoding as per RFC 7636
func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

// base64URLEncode encodes bytes to base64url without padding
func base64URLEncode(data []byte) string {
	encoded := base64.URLEncoding.EncodeToString(data)
	// Remove padding
	return strings.TrimRight(encoded, "=")
}

// BuildAuthorizationURL builds the OpenAI OAuth authorization URL
func BuildAuthorizationURL(state, codeChallenge, redirectURI string) string {
	return BuildAuthorizationURLForPlatform(state, codeChallenge, redirectURI, OAuthPlatformOpenAI)
}

// BuildAuthorizationURLForPlatform builds authorization URL by platform.
func BuildAuthorizationURLForPlatform(state, codeChallenge, redirectURI, platform string) string {
	return BuildAuthorizationURLWithOriginator(state, codeChallenge, redirectURI, platform, CodexDefaultOriginator)
}

// BuildAuthorizationURLWithOriginator builds the authorize URL with the official
// query shape, including the process originator Codex sends today.
func BuildAuthorizationURLWithOriginator(state, codeChallenge, redirectURI, platform, originator string) string {
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}

	clientID, codexFlow := OAuthClientConfigByPlatform(platform)
	if originator = strings.TrimSpace(originator); originator == "" {
		originator = CodexDefaultOriginator
	}

	pairs := [][2]string{
		{"response_type", "code"},
		{"client_id", clientID},
		{"redirect_uri", redirectURI},
		{"scope", DefaultScopes},
		{"code_challenge", codeChallenge},
		{"code_challenge_method", "S256"},
		{"id_token_add_organizations", "true"},
	}
	if codexFlow {
		pairs = append(pairs, [2]string{"codex_cli_simplified_flow", "true"})
	}
	pairs = append(pairs,
		[2]string{"state", state},
		[2]string{"originator", originator},
	)
	return AuthorizeURL + "?" + encodeOfficialOAuthQuery(pairs)
}

// encodeOfficialOAuthQuery matches official Codex urlencoding: space is %20, not +.
func encodeOfficialOAuthQuery(pairs [][2]string) string {
	var b strings.Builder
	for i, pair := range pairs {
		if i > 0 {
			_, _ = b.WriteString("&")
		}
		_, _ = b.WriteString(url.QueryEscape(pair[0]))
		_, _ = b.WriteString("=")
		_, _ = b.WriteString(strings.ReplaceAll(url.QueryEscape(pair[1]), "+", "%20"))
	}
	return b.String()
}

// OAuthClientConfigByPlatform returns oauth client_id and whether codex simplified flow should be enabled.
func OAuthClientConfigByPlatform(platform string) (clientID string, codexFlow bool) {
	return ClientID, true
}

// DeviceUserCodeRequest is the official /deviceauth/usercode JSON body.
type DeviceUserCodeRequest struct {
	ClientID string `json:"client_id"`
}

// DeviceUserCodeResponse is the official /deviceauth/usercode payload.
type DeviceUserCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     int64  `json:"interval"`
}

// UnmarshalJSON accepts official string or numeric interval, and user_code/usercode aliases.
func (r *DeviceUserCodeResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Usercode     string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.DeviceAuthID = strings.TrimSpace(raw.DeviceAuthID)
	r.UserCode = strings.TrimSpace(raw.UserCode)
	if r.UserCode == "" {
		r.UserCode = strings.TrimSpace(raw.Usercode)
	}
	r.Interval = parseFlexibleInt64(raw.Interval)
	return nil
}

func parseFlexibleInt64(raw json.RawMessage) int64 {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0
	}
	if strings.HasPrefix(trimmed, "\"") {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// DeviceTokenPollResponse is the official /deviceauth/token success payload.
type DeviceTokenPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// DeviceTokenPollRequest is the official /deviceauth/token JSON body.
type DeviceTokenPollRequest struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
}

// TokenResponse represents the token response from OpenAI OAuth
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// RefreshTokenRequest represents the refresh token request.
// Official Codex sends JSON {client_id, grant_type, refresh_token} with no scope.
type RefreshTokenRequest struct {
	ClientID     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

// RevokeTokenRequest matches official Codex logout JSON: token, token_type_hint,
// and client_id only when revoking a refresh token.
type RevokeTokenRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint"`
	ClientID      string `json:"client_id,omitempty"`
}

// IDTokenClaims represents the claims from OpenAI ID Token
type IDTokenClaims struct {
	// Standard claims
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Iss           string   `json:"iss"`
	Aud           []string `json:"aud"` // OpenAI returns aud as an array
	Exp           int64    `json:"exp"`
	Iat           int64    `json:"iat"`

	// OpenAI specific claims (nested under https://api.openai.com/auth)
	OpenAIAuth *OpenAIAuthClaims `json:"https://api.openai.com/auth,omitempty"`
}

// OpenAIAuthClaims represents the OpenAI specific auth claims
type OpenAIAuthClaims struct {
	ChatGPTAccountID string              `json:"chatgpt_account_id"`
	ChatGPTUserID    string              `json:"chatgpt_user_id"`
	ChatGPTPlanType  string              `json:"chatgpt_plan_type"`
	UserID           string              `json:"user_id"`
	POID             string              `json:"poid"` // organization ID in access_token JWT
	Organizations    []OrganizationClaim `json:"organizations"`
}

// OrganizationClaim represents an organization in the ID Token
type OrganizationClaim struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	IsDefault bool   `json:"is_default"`
}

// EncodeAuthorizationCodeTokenBody matches official Codex token exchange:
// grant_type, code, redirect_uri, client_id, code_verifier with urlencoding %20.
func EncodeAuthorizationCodeTokenBody(code, redirectURI, clientID, codeVerifier string) string {
	return encodeOfficialOAuthQuery([][2]string{
		{"grant_type", "authorization_code"},
		{"code", code},
		{"redirect_uri", redirectURI},
		{"client_id", clientID},
		{"code_verifier", codeVerifier},
	})
}

// DecodeIDToken decodes the ID Token JWT payload without validating expiration.
// Use this for best-effort extraction (e.g., during data import) where the token may be expired.
func DecodeIDToken(idToken string) (*IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (second part)
	payload := parts[1]
	// Add padding if necessary
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try standard encoding
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
		}
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &claims, nil
}

// ParseIDToken parses the ID Token JWT and extracts claims.
// 注意：当前仅解码 payload 并校验 exp，未验证 JWT 签名。
// 生产环境如需用 ID Token 做授权决策，应通过 OpenAI 的 JWKS 端点验证签名：
//
//	https://auth.openai.com/.well-known/jwks.json
func ParseIDToken(idToken string) (*IDTokenClaims, error) {
	claims, err := DecodeIDToken(idToken)
	if err != nil {
		return nil, err
	}

	// 校验 ID Token 是否已过期（允许 2 分钟时钟偏差，防止因服务器时钟略有差异误判刚颁发的令牌）
	const clockSkewTolerance = 120 // 秒
	now := time.Now().Unix()
	if claims.Exp > 0 && now > claims.Exp+clockSkewTolerance {
		return nil, fmt.Errorf("id_token has expired (exp: %d, now: %d, skew_tolerance: %ds)", claims.Exp, now, clockSkewTolerance)
	}

	return claims, nil
}

// UserInfo represents user information extracted from ID Token claims.
type UserInfo struct {
	Email            string
	ChatGPTAccountID string
	ChatGPTUserID    string
	PlanType         string
	UserID           string
	OrganizationID   string
	Organizations    []OrganizationClaim
}

// GetUserInfo extracts user info from ID Token claims
func (c *IDTokenClaims) GetUserInfo() *UserInfo {
	info := &UserInfo{
		Email: c.Email,
	}

	if c.OpenAIAuth != nil {
		info.ChatGPTAccountID = c.OpenAIAuth.ChatGPTAccountID
		info.ChatGPTUserID = c.OpenAIAuth.ChatGPTUserID
		info.PlanType = c.OpenAIAuth.ChatGPTPlanType
		info.UserID = c.OpenAIAuth.UserID
		info.Organizations = c.OpenAIAuth.Organizations

		// Get default organization ID
		for _, org := range c.OpenAIAuth.Organizations {
			if org.IsDefault {
				info.OrganizationID = org.ID
				break
			}
		}
		// If no default, use first org
		if info.OrganizationID == "" && len(c.OpenAIAuth.Organizations) > 0 {
			info.OrganizationID = c.OpenAIAuth.Organizations[0].ID
		}
	}

	return info
}
