package zcode

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
	"time"
)

// Provider identifiers accepted by the ZCode platform handshake. The Zhipu / GLM
// platform is reachable through two upstream estates, and the handshake, the
// business-token exchange and the model endpoints all differ between them.
const (
	ProviderBigModel = "bigmodel"
	ProviderZai      = "zai"
)

// FlowStatus* are the poll states the ZCode platform reports.
const (
	FlowStatusPending = "pending"
	FlowStatusFailed  = "failed"
	FlowStatusReady   = "ready"
)

const (
	// DefaultHandshakeBaseURL is the ZCode platform API root used for the
	// account-link handshake (init / poll / token).
	DefaultHandshakeBaseURL = "https://zcode.z.ai/api/v1"

	// EnvHandshakeBaseURL overrides DefaultHandshakeBaseURL. It follows the
	// existing provider-environment convention so a private or test deployment
	// can point the handshake at another origin.
	EnvHandshakeBaseURL = "ZHIPU_OAUTH_BASE_URL"

	// SessionTTL bounds an account-link session. The upstream flow itself
	// publishes a shorter expiry which is honored when it arrives.
	SessionTTL = 15 * time.Minute

	// DefaultRequestTimeout bounds one handshake HTTP call.
	DefaultRequestTimeout = 20 * time.Second

	// pollTokenBytes is the poll credential size the official client uses
	// (`randomBytes(32)`).
	pollTokenBytes = 32
	sessionIDBytes = 16
)

// Providers lists the supported upstream estates in display order.
var Providers = []string{ProviderBigModel, ProviderZai}

// IsProvider reports whether provider is one of the supported estates.
func IsProvider(provider string) bool {
	switch strings.TrimSpace(provider) {
	case ProviderBigModel, ProviderZai:
		return true
	default:
		return false
	}
}

// NormalizeProvider folds a caller value onto a supported provider.
func NormalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if IsProvider(provider) {
		return provider
	}
	return ""
}

// HTTPDoer is the minimal transport the handshake and credential resolvers need.
// The repository layer supplies a proxy-aware client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// envelope is the ZCode platform response shape: a business code plus a data
// payload. code == 0 means success.
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// FlowInit is the response of POST /oauth/cli/init.
type FlowInit struct {
	FlowID          string
	AuthorizeURL    string
	ExpiresAt       time.Time
	PollIntervalSec int64
}

// FlowUser is the linked account identity reported by the flow.
type FlowUser struct {
	ID     string `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar"`
}

// DisplayName returns the best available human label.
func (u FlowUser) DisplayName() string {
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}
	if email := strings.TrimSpace(u.Email); email != "" {
		return email
	}
	return strings.TrimSpace(u.ID)
}

// FlowProviderToken carries the provider access token reported by the flow.
type FlowProviderToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// FlowReady is the credential payload of a ready poll.
type FlowReady struct {
	// ZCodeJWT is the shared ZCode platform token. The start-plan and off-peak
	// plans authenticate model traffic with it.
	ZCodeJWT string
	User     FlowUser
	// Provider is the estate-specific token. It is an OAuth token for Z.ai and a
	// business token for BigModel; the Z.ai value must still be exchanged
	// through ExchangeZaiBusinessToken before any business API call.
	Provider FlowProviderToken
}

// FlowPoll is one poll result.
type FlowPoll struct {
	Status string
	Ready  *FlowReady
}

type initData struct {
	FlowID          string `json:"flow_id"`
	AuthorizeURL    string `json:"authorize_url"`
	ExpiresAt       int64  `json:"expires_at"`
	PollIntervalSec int64  `json:"poll_interval_sec"`
}

type pollData struct {
	Status   string           `json:"status"`
	Token    string           `json:"token"`
	User     FlowUser         `json:"user"`
	BigModel *providerTokenIn `json:"bigmodel"`
	Zai      *providerTokenIn `json:"zai"`
}

type providerTokenIn struct {
	AccessToken      string `json:"access_token"`
	AccessTokenCamel string `json:"accessToken"`
	RefreshToken     string `json:"refresh_token"`
	RefreshCamel     string `json:"refreshToken"`
}

func (p *providerTokenIn) normalize() FlowProviderToken {
	if p == nil {
		return FlowProviderToken{}
	}
	access := strings.TrimSpace(p.AccessToken)
	if access == "" {
		access = strings.TrimSpace(p.AccessTokenCamel)
	}
	refresh := strings.TrimSpace(p.RefreshToken)
	if refresh == "" {
		refresh = strings.TrimSpace(p.RefreshCamel)
	}
	return FlowProviderToken{AccessToken: access, RefreshToken: refresh}
}

// HandshakeClient speaks the ZCode platform account-link handshake.
type HandshakeClient struct {
	client  HTTPDoer
	baseURL string
	timeout time.Duration
}

// NewHandshakeClient builds a handshake client. An empty baseURL falls back to
// DefaultHandshakeBaseURL; a nil doer falls back to the default HTTP client.
func NewHandshakeClient(client HTTPDoer, baseURL string) *HandshakeClient {
	if client == nil {
		client = http.DefaultClient
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultHandshakeBaseURL
	}
	return &HandshakeClient{client: client, baseURL: baseURL, timeout: DefaultRequestTimeout}
}

// BaseURL reports the effective handshake root.
func (c *HandshakeClient) BaseURL() string {
	if c == nil {
		return DefaultHandshakeBaseURL
	}
	return c.baseURL
}

// GeneratePollToken creates the bearer credential the handshake carries. It is
// held server-side for the whole session and is never returned to a client.
func GeneratePollToken() (string, error) {
	buf := make([]byte, pollTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateSessionID creates the opaque handle the admin panel exchanges for a
// server-held session.
func GenerateSessionID() (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Init starts a flow for provider and returns the authorization URL the operator
// must open in a browser.
func (c *HandshakeClient) Init(ctx context.Context, provider, pollToken string) (*FlowInit, error) {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth provider")
	}
	if strings.TrimSpace(pollToken) == "" {
		return nil, fmt.Errorf("poll token is required")
	}
	body, err := json.Marshal(map[string]string{"provider": provider})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, c.baseURL+"/oauth/cli/init", map[string]string{
		"Authorization": "Bearer " + pollToken,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	var data initData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode oauth init response: %w", err)
	}
	authorizeURL := strings.TrimSpace(data.AuthorizeURL)
	if strings.TrimSpace(data.FlowID) == "" || authorizeURL == "" || data.ExpiresAt <= 0 || data.PollIntervalSec < 1 {
		return nil, fmt.Errorf("invalid oauth init response")
	}
	// The authorization URL is opened by the operator. Only an absolute https URL
	// may be surfaced, so a malformed or hostile response cannot redirect the
	// admin panel to another scheme or host.
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth authorize url")
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid oauth authorize url")
	}
	return &FlowInit{
		FlowID:          strings.TrimSpace(data.FlowID),
		AuthorizeURL:    parsed.String(),
		ExpiresAt:       time.Unix(data.ExpiresAt, 0),
		PollIntervalSec: data.PollIntervalSec,
	}, nil
}

// Poll reports the current flow state. The poll token must be the same one used
// for Init; the platform binds the flow to it.
func (c *HandshakeClient) Poll(ctx context.Context, provider, flowID, pollToken string) (*FlowPoll, error) {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth provider")
	}
	flowID = strings.TrimSpace(flowID)
	if flowID == "" || strings.TrimSpace(pollToken) == "" {
		return nil, fmt.Errorf("invalid oauth session")
	}
	raw, err := c.do(ctx, http.MethodGet, c.baseURL+"/oauth/cli/poll/"+url.PathEscape(flowID), map[string]string{
		"Authorization": "Bearer " + pollToken,
	}, nil)
	if err != nil {
		return nil, err
	}
	var data pollData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode oauth poll response: %w", err)
	}
	switch strings.TrimSpace(data.Status) {
	case FlowStatusPending:
		return &FlowPoll{Status: FlowStatusPending}, nil
	case FlowStatusFailed:
		return &FlowPoll{Status: FlowStatusFailed}, nil
	case FlowStatusReady:
	default:
		return nil, fmt.Errorf("invalid oauth poll response")
	}
	providerToken := FlowProviderToken{}
	if provider == ProviderZai {
		providerToken = data.Zai.normalize()
	} else {
		providerToken = data.BigModel.normalize()
	}
	ready := &FlowReady{
		ZCodeJWT: strings.TrimSpace(data.Token),
		User:     data.User,
		Provider: providerToken,
	}
	if ready.ZCodeJWT == "" || providerToken.AccessToken == "" || strings.TrimSpace(ready.User.ID) == "" {
		return nil, fmt.Errorf("invalid oauth ready response")
	}
	return &FlowPoll{Status: FlowStatusReady, Ready: ready}, nil
}

// ExchangeCode redeems an authorization code from the legacy deep-link flow. It
// is the documented fallback for deployments where the polling handshake is
// unavailable; redirectURI must be byte-identical to the value used in the
// authorization request.
func (c *HandshakeClient) ExchangeCode(ctx context.Context, provider, code, redirectURI, state string) (*FlowReady, error) {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth provider")
	}
	code, redirectURI, state = strings.TrimSpace(code), strings.TrimSpace(redirectURI), strings.TrimSpace(state)
	if code == "" || state == "" {
		return nil, fmt.Errorf("missing authorization code or state")
	}
	body, err := json.Marshal(map[string]string{
		"provider":     provider,
		"code":         code,
		"redirect_uri": redirectURI,
		"state":        state,
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, c.baseURL+"/oauth/token", map[string]string{
		"Content-Type": "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	var data struct {
		Token    string           `json:"token"`
		User     FlowUser         `json:"user"`
		BigModel *providerTokenIn `json:"bigmodel"`
		Zai      *providerTokenIn `json:"zai"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode oauth token response: %w", err)
	}
	providerToken := FlowProviderToken{}
	if provider == ProviderZai {
		providerToken = data.Zai.normalize()
	} else {
		providerToken = data.BigModel.normalize()
	}
	ready := &FlowReady{ZCodeJWT: strings.TrimSpace(data.Token), User: data.User, Provider: providerToken}
	if ready.ZCodeJWT == "" || providerToken.AccessToken == "" {
		return nil, fmt.Errorf("invalid oauth token response")
	}
	return ready, nil
}

// do performs one handshake call and unwraps the platform envelope.
func (c *HandshakeClient) do(ctx context.Context, method, target string, headers map[string]string, body []byte) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// The handshake payload is a small JSON envelope; cap the read so a hostile or
	// misconfigured endpoint cannot exhaust memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth endpoint returned status %d", resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode oauth envelope: %w", err)
	}
	if env.Code != 0 {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = "oauth endpoint rejected the request"
		}
		return nil, fmt.Errorf("%s (code %d)", msg, env.Code)
	}
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("oauth endpoint returned no data")
	}
	return env.Data, nil
}

// DesktopRedirectURI mirrors the official client's account-link redirect target:
// the ZCode website page that finishes authorization and then hands the code back
// through the app's custom scheme. A server-hosted deployment cannot receive that
// hand-off, which is exactly why the polling handshake is the primary flow and
// this value is only needed by the paste-the-callback-URL fallback.
func DesktopRedirectURI(origin, appVersion string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		origin = "https://zcode.z.ai"
	}
	if !IsSupportedVersion(appVersion) {
		appVersion = DefaultVersion
	}
	u, err := url.Parse(origin + "/app/oauth/login")
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("redirect", "zcode://oauth/callback")
	query.Set("app_version", appVersion)
	u.RawQuery = query.Encode()
	return u.String()
}

// ParseCallback extracts the authorization code and state from a pasted callback
// URL or a bare code. The legacy BigModel callback historically used `authCode`
// and may now use `code`, so both are accepted.
func ParseCallback(input string) (code, state string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return input, ""
	}
	query := parsed.Query()
	code = strings.TrimSpace(query.Get("code"))
	if code == "" {
		code = strings.TrimSpace(query.Get("authCode"))
	}
	return code, strings.TrimSpace(query.Get("state"))
}
