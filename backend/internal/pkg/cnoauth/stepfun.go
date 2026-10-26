package cnoauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/stepfun"
)

// Step-Code step-provider/login-client.ts: flow_id is public, poll_token is
// server-held bearer material. Model requests use the returned API key only.
const stepLoginPath = "/api/stdhttp/v1/cli-login"

var stepFlowID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// StepLogin authenticates account operations, never model inference. Its lifetime
// and refresh credential are independent of the static Step Plan model key.
type StepLogin struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	DeviceID     string    `json:"device_id"`
	AppID        string    `json:"app_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func stepOrigin(region string) string {
	if region == "global" {
		return "https://platform.stepfun.ai"
	}
	return "https://platform.stepfun.com"
}

func (c *Client) stepLoginRequest(ctx context.Context, f *Flow, proxy, method, path string, body map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var reader io.Reader
	contentType := ""
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
		contentType = "application/json"
	}
	value, status, err := c.requestHeaders(ctx, proxy, method, stepOrigin(f.Region)+stepLoginPath+path, reader, contentType, map[string]string{"Authorization": "Bearer " + f.DeviceCode})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 || (value["status"] != nil && value["status"] != float64(0)) {
		return nil, errors.New("step login request rejected")
	}
	return value, nil
}

func (c *Client) startStepFun(ctx context.Context, f *Flow, proxy string) (*Flow, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	f.DeviceCode = hex.EncodeToString(secret)
	profile := "step_plan"
	if f.Region == "global" {
		profile = "step_plan_oversea"
	}
	value, err := c.stepLoginRequest(ctx, f, proxy, http.MethodPost, "/init", map[string]any{
		"profile": profile, "origin": stepOrigin(f.Region),
		"client": map[string]string{"name": "stepcode", "version": stepfun.AgentVersion, "platform": "linux-x64"},
	})
	if err != nil {
		return nil, err
	}
	id := str(value, "flow_id")
	interval, validInterval := value["poll_interval_sec"].(float64)
	expires, validExpiry := value["expires_at"].(float64)
	if !stepFlowID.MatchString(id) || !validInterval || math.IsNaN(interval) || math.IsInf(interval, 0) || interval < 1 || !validExpiry || math.IsNaN(expires) || math.IsInf(expires, 0) || expires <= float64(time.Now().Unix()) {
		return nil, errors.New("invalid step login initialization")
	}
	f.FlowID = id
	f.Interval = int(math.Ceil(min(interval, 60)))
	if expiry := time.Unix(int64(expires), 0); expiry.Before(f.ExpiresAt) {
		f.ExpiresAt = expiry
	}
	f.AuthorizeURL = stepOrigin(f.Region) + "/cli-login-remote?" + url.Values{"flow_id": {id}}.Encode()
	return f, nil
}

func (c *Client) pollStepFun(ctx context.Context, f *Flow, proxy string) (*Grant, error) {
	if !stepFlowID.MatchString(f.FlowID) || !validToken(f.DeviceCode) {
		return nil, errors.New("invalid step login flow")
	}
	value, err := c.stepLoginRequest(ctx, f, proxy, http.MethodGet, "/poll/"+url.PathEscape(f.FlowID), nil)
	if err != nil {
		return nil, err
	}
	switch str(value, "state") {
	case "pending":
		return nil, ErrPending
	case "failed":
		return nil, ErrDenied
	case "ready":
	default:
		return nil, errors.New("invalid step login state")
	}
	credential, _ := value["credential"].(map[string]any)
	token := str(credential, "api_key")
	if !validToken(token) {
		return nil, errors.New("invalid step model credential")
	}
	grant := &Grant{AccessToken: token}
	// Invalid optional Passport data must not discard a usable model key.
	if login, ok := credential["login"].(map[string]any); ok {
		session := &StepLogin{AccessToken: str(login, "access_token"), RefreshToken: str(login, "refresh_token"), DeviceID: str(login, "device_id"), AppID: str(login, "app_id")}
		if validToken(session.AccessToken) && validToken(session.RefreshToken) && validToken(session.DeviceID) && validToken(session.AppID) {
			ttl := number(login, "expires_in")
			if ttl <= 0 || ttl > 365*24*3600 {
				ttl = 1800
			}
			session.ExpiresAt = time.Now().Add(time.Duration(ttl * float64(time.Second)))
			grant.StepLogin = session
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return grant, nil
}
