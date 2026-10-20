package cnoauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

type StepLogin struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	DeviceID     string `json:"device_id"`
	AppID        string `json:"app_id"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

func stepAuthOrigin(region string) string {
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
	v, status, err := c.requestWithMethod(ctx, proxy, method, stepAuthOrigin(f.Region)+stepLoginPath+path, reader, contentType, map[string]string{"Authorization": "Bearer " + f.DeviceCode})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 || (v["status"] != nil && v["status"] != float64(0)) {
		return nil, errors.New("step login request rejected")
	}
	return v, nil
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
	v, err := c.stepLoginRequest(ctx, f, proxy, http.MethodPost, "/init", map[string]any{
		"profile": profile, "origin": stepAuthOrigin(f.Region),
		"client": map[string]string{"name": "stepcode", "version": stepfun.AgentVersion, "platform": "linux-x64"},
	})
	if err != nil {
		return nil, err
	}
	f.State = str(v, "flow_id")
	interval, ok := v["poll_interval_sec"].(float64)
	expires, expiryOK := v["expires_at"].(float64)
	if !stepFlowID.MatchString(f.State) || !ok || interval < 1 || !expiryOK || expires <= float64(time.Now().Unix()) {
		return nil, errors.New("invalid Step login initialization")
	}
	f.Interval = int(min(interval, 60))
	if expiry := time.Unix(int64(expires), 0); expiry.Before(f.ExpiresAt) {
		f.ExpiresAt = expiry
	}
	f.AuthorizeURL = stepAuthOrigin(f.Region) + "/cli-login-remote?" + url.Values{"flow_id": {f.State}}.Encode()
	return f, nil
}

func (c *Client) pollStepFun(ctx context.Context, f *Flow, proxy string) (*Grant, error) {
	if !stepFlowID.MatchString(f.State) || !validToken(f.DeviceCode) {
		return nil, errors.New("invalid Step login session")
	}
	v, err := c.stepLoginRequest(ctx, f, proxy, http.MethodGet, "/poll/"+url.PathEscape(f.State), nil)
	if err != nil {
		return nil, err
	}
	switch str(v, "state") {
	case "pending":
		return nil, ErrPending
	case "failed":
		return nil, ErrDenied
	case "ready":
		credential, _ := v["credential"].(map[string]any)
		key := str(credential, "api_key")
		if !validToken(key) {
			return nil, errors.New("invalid Step model credential")
		}
		grant := &Grant{AccessToken: key}
		// Optional account sessions must never become a model token or determine
		// its expiry. Invalid optional session data does not discard a valid key.
		if login, ok := credential["login"].(map[string]any); ok {
			state := &StepLogin{AccessToken: str(login, "access_token"), RefreshToken: str(login, "refresh_token"), DeviceID: str(login, "device_id"), AppID: str(login, "app_id")}
			if validToken(state.AccessToken) && validToken(state.RefreshToken) && validToken(state.DeviceID) && validToken(state.AppID) {
				if expiry, ok := login["expires_in"].(float64); ok && expiry > 0 && expiry <= 365*24*3600 {
					state.ExpiresIn = int64(expiry)
				}
				grant.StepLogin = state
			}
		}
		return grant, nil
	default:
		return nil, errors.New("invalid Step login state")
	}
}
