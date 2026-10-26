package cnoauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var ErrStepAccountSessionExpired = errors.New("step account session is no longer signed in")

// RefreshStepLogin refreshes only the optional Passport session. HTTP failures
// and contract drift retain the long-lived refresh material for a later retry.
func (c *Client) RefreshStepLogin(ctx context.Context, region, proxy string, previous *StepLogin) (*StepLogin, error) {
	region, err := NormalizeRegion("stepfun", region)
	if err != nil {
		return nil, err
	}
	if previous == nil || !validToken(previous.AccessToken) || !validToken(previous.RefreshToken) || !validToken(previous.DeviceID) || !validToken(previous.AppID) || strings.ContainsAny(previous.AccessToken+previous.RefreshToken, ";,\"\\") {
		return nil, errors.New("invalid step account session")
	}
	headers := map[string]string{"Connect-Protocol-Version": "1", "Cookie": "Oasis-Token=" + previous.AccessToken + "..." + previous.RefreshToken, "Oasis-appID": previous.AppID, "Oasis-Platform": "web", "Oasis-Webid": previous.DeviceID}
	value, status, err := c.requestHeaders(snapshot(ctx, "stepfun"), proxy, http.MethodPost, stepOrigin(region)+"/passport/proto.api.passport.v1.PassportService/RefreshToken", strings.NewReader("{}"), "application/json", headers)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("step account refresh HTTP %d", status)
	}
	access, _ := value["accessToken"].(map[string]any)
	if access == nil || !validToken(str(access, "raw")) {
		return nil, errors.New("invalid step account refresh response")
	}
	mode, ok := access["mode"].(float64)
	if !ok {
		return nil, errors.New("invalid step account refresh mode")
	}
	if mode != 2 {
		return nil, ErrStepAccountSessionExpired
	}
	fresh := *previous
	fresh.AccessToken = str(access, "raw")
	if refresh, ok := value["refreshToken"].(map[string]any); ok && validToken(str(refresh, "raw")) {
		fresh.RefreshToken = str(refresh, "raw")
	}
	ttl := number(access, "duration")
	if ttl <= 0 || ttl > 365*24*3600 {
		ttl = 1800
	}
	fresh.ExpiresAt = time.Now().Add(time.Duration(ttl * float64(time.Second)))
	return &fresh, nil
}
