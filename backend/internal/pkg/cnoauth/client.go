// Package cnoauth implements the official DeepSeek, Kimi and MiniMax login protocols.
package cnoauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/deepseek"
	sharedhttp "github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/kimi"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
)

const TTL = 10 * time.Minute
const DeepSeekRedirect = "http://127.0.0.1:53682/oauth/callback"
const kimiClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"
const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

var ErrPending = errors.New("authorization_pending")
var ErrSlowDown = errors.New("slow_down")
var ErrDenied = errors.New("access_denied")
var ErrExpired = errors.New("expired_token")
var ErrInvalidGrant = errors.New("invalid_grant: reauthorize this account")

// Grant remains server-side until persisted to the credential-owning account.
type Grant struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}
type Flow struct {
	Platform      string    `json:"platform"`
	Region        string    `json:"region"`
	DeviceCode    string    `json:"device_code,omitempty"`
	UserCode      string    `json:"user_code,omitempty"`
	PollParameter string    `json:"poll_parameter,omitempty"`
	Verifier      string    `json:"verifier,omitempty"`
	State         string    `json:"state,omitempty"`
	DeviceID      string    `json:"device_id,omitempty"`
	AuthorizeURL  string    `json:"authorize_url"`
	ExpiresAt     time.Time `json:"expires_at"`
	Interval      int       `json:"interval"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client permits transport injection for wire tests. Production endpoints are fixed.
type Client struct{ HTTP HTTPDoer }

func Supported(platform string) bool {
	return platform == "deepseek" || platform == "kimi" || platform == "minimax"
}
func NormalizeRegion(platform, region string) (string, error) {
	if !Supported(platform) {
		return "", errors.New("unsupported platform")
	}
	if region == "" {
		region = "cn"
	}
	if region != "cn" && region != "global" || platform == "deepseek" && region != "cn" {
		return "", errors.New("unsupported region")
	}
	return region, nil
}
func origin(platform, region string) string {
	switch platform {
	case "deepseek":
		return "https://platform.deepseek.com"
	case "kimi":
		if region == "global" {
			return "https://auth.kimi.ai"
		}
		return "https://auth.kimi.com"
	case "minimax":
		if region == "global" {
			return "https://account.minimax.io"
		}
		return "https://account.minimax.cn"
	}
	return ""
}
func ModelBase(platform, region string) string {
	switch platform {
	case "deepseek":
		return "https://api.deepseek.com/anthropic"
	case "kimi":
		if region == "global" {
			return "https://api.kimi.ai/coding"
		}
		return "https://api.kimi.com/coding"
	case "minimax":
		if region == "global" {
			return "https://agent.minimax.io/mavis/api/v1/llm"
		}
		return "https://agent.minimax.cn/mavis/api/v1/llm"
	}
	return ""
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func snapshot(ctx context.Context, platform string) context.Context {
	identity, ok := outboundidentity.Default(ctx, platform)
	if !ok {
		switch platform {
		case "deepseek":
			identity = deepseek.DefaultIdentity()
		case "kimi":
			identity = kimi.DefaultIdentity()
		case "minimax":
			identity = minimax.DefaultIdentity()
		}
	}
	return outboundidentity.WithIdentity(ctx, identity)
}
func (c *Client) request(ctx context.Context, proxy, target string, body io.Reader, contentType string) (map[string]any, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return nil, 0, errors.New("invalid oauth request")
	}
	identity, _ := outboundidentity.FromContext(ctx)
	switch identity.Preset {
	case "deepseek":
		identity = deepseek.ControlIdentity(identity)
	case minimax.Preset:
		identity = minimax.ControlIdentity(identity)
	}
	*req = *req.WithContext(outboundidentity.WithIdentity(ctx, identity))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	outboundidentity.ApplyContext(req)
	doer := c.HTTP
	if doer == nil {
		client, e := sharedhttp.GetClient(sharedhttp.Options{ProxyURL: proxy, Timeout: 30 * time.Second})
		if e != nil {
			return nil, 0, errors.New("oauth proxy unavailable")
		}
		clone := *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		doer = &clone
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, 0, errors.New("oauth transport failed")
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 {
		return nil, resp.StatusCode, errors.New("invalid oauth response size")
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil || value == nil {
		return nil, resp.StatusCode, errors.New("invalid oauth response")
	}
	return value, resp.StatusCode, nil
}
func (c *Client) form(ctx context.Context, proxy, target string, values url.Values) (map[string]any, int, error) {
	return c.request(ctx, proxy, target, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
}
func (c *Client) deepseek(ctx context.Context, proxy, method string, value map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(value)
	v, status, err := c.request(ctx, proxy, origin("deepseek", "cn")+"/auth-api/v0/dsh/"+method, strings.NewReader(string(body)), "application/json")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("deepseek authorization HTTP %d", status)
	}
	data, ok := v["data"].(map[string]any)
	if !ok || v["code"] != float64(0) || data["biz_code"] != float64(0) {
		return nil, errors.New("deepseek authorization rejected")
	}
	result, ok := data["biz_data"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid deepseek authorization envelope")
	}
	return result, nil
}
func (c *Client) Start(ctx context.Context, platform, region, proxy string) (*Flow, error) {
	region, err := NormalizeRegion(platform, region)
	if err != nil {
		return nil, err
	}
	ctx = snapshot(ctx, platform)
	f := &Flow{Platform: platform, Region: region, Interval: 5, ExpiresAt: time.Now().Add(TTL)}
	f.Verifier, err = randomToken()
	if err != nil {
		return nil, err
	}
	challenge := sha256.Sum256([]byte(f.Verifier))
	encoded := base64.RawURLEncoding.EncodeToString(challenge[:])
	if platform == "deepseek" {
		f.State, err = randomToken()
		if err != nil {
			return nil, err
		}
		f.DeviceID = uuid.NewString()
		v, e := c.deepseek(ctx, proxy, "auth_init", map[string]any{"code_challenge": encoded, "code_challenge_method": "S256", "state": f.State, "redirect_uri": DeepSeekRedirect, "locale": "zh_CN", "login_source": "web"})
		if e != nil {
			return nil, e
		}
		f.AuthorizeURL = str(v, "authorize_url")
		if !validAuthorizeURL(f.AuthorizeURL, platform, region) || number(v, "expires_in") <= 0 || str(v, "authorize_id") == "" {
			return nil, errors.New("invalid deepseek authorization response")
		}
		f.ExpiresAt = boundedExpiry(number(v, "expires_in"))
		return f, nil
	}
	values := url.Values{"client_id": {kimiClientID}}
	path := "/api/oauth/device_authorization"
	if platform == "minimax" {
		path = "/oauth2/device/code"
		values = url.Values{"client_id": {"mcode-public"}, "scope": {"agent.default"}, "audience": {"agent-backend"}, "code_challenge": {encoded}, "code_challenge_method": {"S256"}}
	}
	v, status, e := c.form(ctx, proxy, origin(platform, region)+path, values)
	if e != nil {
		return nil, e
	}
	if status != 200 {
		return nil, fmt.Errorf("device authorization HTTP %d", status)
	}
	f.DeviceCode = str(v, "device_code")
	f.UserCode = str(v, "user_code")
	f.AuthorizeURL = str(v, "verification_uri_complete")
	if f.AuthorizeURL == "" {
		f.AuthorizeURL = str(v, "verification_uri")
	}
	if f.AuthorizeURL == "" {
		f.AuthorizeURL = str(v, "verification_url")
	}
	expiry := number(v, "expires_in")
	interval := number(v, "interval")
	if platform == "minimax" && f.DeviceCode == "" && number(v, "expired_in") > 0 {
		f.PollParameter = "user_code"
		f.DeviceCode = f.UserCode
		expiry = number(v, "expired_in")
		if expiry >= 1e12 {
			expiry = (expiry - float64(time.Now().UnixMilli())) / 1000
		}
		interval /= 1000
	}
	if platform == "kimi" && expiry == 0 {
		expiry = TTL.Seconds()
	}
	if f.DeviceCode == "" || f.UserCode == "" || expiry <= 0 || !validAuthorizeURL(f.AuthorizeURL, platform, region) {
		return nil, errors.New("invalid device authorization response")
	}
	if interval > 0 && interval <= 60 {
		f.Interval = max(1, int(math.Ceil(interval)))
	}
	f.ExpiresAt = boundedExpiry(expiry)
	return f, nil
}
func boundedExpiry(seconds float64) time.Time {
	return time.Now().Add(time.Duration(min(seconds, TTL.Seconds()) * float64(time.Second)))
}
func validAuthorizeURL(raw, platform, region string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	expected, _ := url.Parse(origin(platform, region))
	return u.Host == expected.Host && (platform != "deepseek" || u.Path == "/dsh/authorize")
}
func (c *Client) Poll(ctx context.Context, f *Flow, proxy string) (*Grant, error) {
	if time.Now().After(f.ExpiresAt) {
		return nil, ErrExpired
	}
	if f.Platform == "deepseek" {
		return nil, ErrPending
	}
	values := url.Values{"grant_type": {deviceGrant}, "device_code": {f.DeviceCode}, "client_id": {kimiClientID}}
	path := "/api/oauth/token"
	if f.Platform == "minimax" {
		path = "/oauth2/token"
		values.Set("client_id", "mcode-public")
		values.Set("code_verifier", f.Verifier)
		if f.PollParameter == "user_code" {
			values.Del("device_code")
			values.Set("user_code", f.UserCode)
		}
	}
	return c.token(snapshot(ctx, f.Platform), f.Platform, f.Region, proxy, path, values, "")
}
func (c *Client) Exchange(ctx context.Context, f *Flow, callback, proxy string) (*Grant, error) {
	if f.Platform != "deepseek" {
		return nil, errors.New("callback unsupported")
	}
	if time.Now().After(f.ExpiresAt) {
		return nil, ErrExpired
	}
	u, err := url.Parse(strings.TrimSpace(callback))
	expected, _ := url.Parse(DeepSeekRedirect)
	if err != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
		return nil, errors.New("paste the complete DeepSeek callback URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["state"]) != 1 || q.Get("state") != f.State || len(q["code"]) != 1 || q.Get("code") == "" {
		return nil, errors.New("invalid callback state or code")
	}
	ctx = snapshot(ctx, "deepseek")
	v, err := c.deepseek(ctx, proxy, "auth_exchange", map[string]any{"code": q.Get("code"), "code_verifier": f.Verifier, "redirect_uri": DeepSeekRedirect, "device_id": f.DeviceID, "device_model": deviceModel(), "os_version": runtime.GOOS + " " + kimi.ResolveDeviceFacts().OSVersion})
	if err != nil {
		return nil, err
	}
	token := str(v, "token")
	if !validToken(token) {
		return nil, errors.New("invalid deepseek grant")
	}
	return &Grant{AccessToken: token}, nil
}
func (c *Client) Refresh(ctx context.Context, platform, region, refresh, proxy string) (*Grant, error) {
	if platform != "kimi" && platform != "minimax" {
		return nil, errors.New("reauthorization required")
	}
	region, err := NormalizeRegion(platform, region)
	if err != nil {
		return nil, err
	}
	values := url.Values{"client_id": {kimiClientID}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}}
	path := "/api/oauth/token"
	if platform == "minimax" {
		path = "/oauth2/token"
		values.Set("client_id", "mcode-public")
		values.Set("scope", "agent.default")
		values.Set("audience", "agent-backend")
	}
	return c.token(snapshot(ctx, platform), platform, region, proxy, path, values, refresh)
}
func (c *Client) token(ctx context.Context, platform, region, proxy, path string, values url.Values, previous string) (*Grant, error) {
	v, status, err := c.form(ctx, proxy, origin(platform, region)+path, values)
	if err != nil {
		return nil, err
	}
	code := str(v, "error")
	if code == "" && platform == "minimax" {
		code = str(v, "status")
	}
	switch code {
	case "authorization_pending", "pending":
		return nil, ErrPending
	case "slow_down":
		return nil, ErrSlowDown
	case "access_denied", "denied":
		return nil, ErrDenied
	case "expired_token", "expired":
		return nil, ErrExpired
	case "invalid_grant":
		return nil, ErrInvalidGrant
	}
	if str(v, "error") != "" {
		return nil, errors.New("oauth authorization rejected")
	}
	if status == 401 || status == 403 {
		return nil, ErrInvalidGrant
	}
	if status != 200 {
		return nil, fmt.Errorf("oauth token HTTP %d", status)
	}
	access, refresh := str(v, "access_token"), str(v, "refresh_token")
	if refresh == "" && platform == "minimax" {
		refresh = previous
	}
	expiry := number(v, "expires_in")
	if !validToken(access) || !validToken(refresh) || expiry < 1 || expiry > 365*24*3600 {
		return nil, errors.New("invalid oauth token response")
	}
	if platform == "minimax" {
		scope := scopeValue(v["scope"])
		if scope == "" {
			parts := strings.Split(access, ".")
			if len(parts) == 3 {
				raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
				var claims map[string]any
				_ = json.Unmarshal(raw, &claims)
				scope = scopeValue(claims["scope"])
				if scope == "" {
					scope = scopeValue(claims["scp"])
				}
			}
		}
		if !strings.EqualFold(str(v, "token_type"), "bearer") || !containsScope(scope, "agent.default") {
			return nil, errors.New("invalid minimax token scope")
		}
	}
	return &Grant{AccessToken: access, RefreshToken: refresh, ExpiresAt: time.Now().Add(time.Duration(expiry) * time.Second)}, nil
}
func containsScope(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}
func str(v map[string]any, key string) string { s, _ := v[key].(string); return strings.TrimSpace(s) }
func number(v map[string]any, key string) float64 {
	switch n := v[key].(type) {
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n
		}
	case string:
		f, e := strconv.ParseFloat(n, 64)
		if e == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	return 0
}
func validToken(value string) bool {
	if value == "" || len(value) > 16384 {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func deviceModel() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return runtime.GOOS + "-" + arch
}
func scopeValue(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []any:
		var parts []string
		for _, item := range s {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}
