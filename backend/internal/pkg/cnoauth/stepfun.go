package cnoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
)

// StepLogin authenticates account operations, never model inference. Its lifetime
// and refresh credential are independent of the static Step Plan model key.
type StepLogin struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	DeviceID     string    `json:"device_id"`
	AppID        string    `json:"app_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

const stepClientVersion = "0.84.4"

var stepFlowID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func stepOrigin(region string) string {
	if region == "global" {
		return "https://platform.stepfun.ai"
	}
	return "https://platform.stepfun.com"
}

func (c *Client) startStepFun(ctx context.Context, f *Flow, proxy string) (*Flow, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	profile := "step_plan"
	if f.Region == "global" {
		profile = "step_plan_oversea"
	}
	body, _ := json.Marshal(map[string]any{"profile": profile, "origin": stepOrigin(f.Region), "client": map[string]string{"name": "stepcode", "version": stepClientVersion, "platform": "linux-x64"}})
	v, status, err := c.requestHeaders(ctx, proxy, http.MethodPost, stepOrigin(f.Region)+"/api/stdhttp/v1/cli-login/init", strings.NewReader(string(body)), "application/json", map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return nil, err
	}
	if err = stepResponse(v, status); err != nil {
		return nil, err
	}
	id, interval, expiry := str(v, "flow_id"), number(v, "poll_interval_sec"), number(v, "expires_at")
	if !stepFlowID.MatchString(id) || interval < 1 || expiry <= float64(time.Now().Unix()) {
		return nil, errors.New("invalid step login initialization")
	}
	f.FlowID, f.DeviceCode = id, token
	f.Interval = int(math.Ceil(min(interval, 60)))
	f.ExpiresAt = time.Unix(int64(min(expiry, float64(time.Now().Add(TTL).Unix()))), 0)
	f.AuthorizeURL = stepOrigin(f.Region) + "/cli-login-remote?" + url.Values{"flow_id": {id}}.Encode()
	return f, nil
}

func stepResponse(v map[string]any, status int) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("step login HTTP %d", status)
	}
	if code, ok := v["status"]; ok && code != float64(0) {
		return errors.New("step login rejected")
	}
	return nil
}

func (c *Client) pollStepFun(ctx context.Context, f *Flow, proxy string) (*Grant, error) {
	if !stepFlowID.MatchString(f.FlowID) || !validToken(f.DeviceCode) {
		return nil, errors.New("invalid step login flow")
	}
	v, status, err := c.requestHeaders(ctx, proxy, http.MethodGet, stepOrigin(f.Region)+"/api/stdhttp/v1/cli-login/poll/"+f.FlowID, nil, "", map[string]string{"Authorization": "Bearer " + f.DeviceCode})
	if err != nil {
		return nil, err
	}
	if err = stepResponse(v, status); err != nil {
		return nil, err
	}
	switch str(v, "state") {
	case "pending":
		return nil, ErrPending
	case "failed":
		return nil, ErrDenied
	case "ready":
	default:
		return nil, errors.New("invalid step login state")
	}
	credential, _ := v["credential"].(map[string]any)
	token := str(credential, "api_key")
	if !validToken(token) || brandidentity.ContainsBrand(token) {
		return nil, errors.New("invalid step model credential")
	}
	grant := &Grant{AccessToken: token}
	login, _ := credential["login"].(map[string]any)
	if login != nil {
		session := &StepLogin{AccessToken: str(login, "access_token"), RefreshToken: str(login, "refresh_token"), DeviceID: str(login, "device_id"), AppID: str(login, "app_id")}
		if validToken(session.AccessToken) && validToken(session.RefreshToken) && validToken(session.DeviceID) && validToken(session.AppID) {
			ttl := number(login, "expires_in")
			if ttl <= 0 || ttl > 365*24*3600 {
				ttl = 1800
			}
			session.ExpiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
			grant.StepLogin = session
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return grant, nil
}
