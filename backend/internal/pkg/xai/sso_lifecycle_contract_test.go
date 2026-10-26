//go:build unit

package xai

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ssoLifecycleClient func(*http.Request) (*http.Response, error)

func (f ssoLifecycleClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

// RFC 8628 section 3.5 defines pending, additive five-second slow_down,
// terminal denial/expiry, and the prohibition on polling after grant expiry.
func TestSSODevicePollingBusinessStates(t *testing.T) {
	for _, mode := range []string{"pending-slow-down-success", "repeated-slow-down", "access-denied", "expired-token", "upstream-secret-echo", "cancel-during-request", "expire-during-sleep", "expire-during-request"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var sleeps []time.Duration
			sequence := []string{"authorization_pending", "slow_down", "authorization_pending", "success"}
			switch mode {
			case "repeated-slow-down":
				sequence = []string{"slow_down", "slow_down", "success"}
			case "access-denied":
				sequence = []string{"access_denied"}
			case "expired-token":
				sequence = []string{"expired_token"}
			case "cancel-during-request", "expire-during-sleep", "expire-during-request":
				sequence = []string{"success"}
			}
			flow := &ssoDeviceFlow{client: ssoLifecycleClient(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, SSOTokenURL, r.URL.String())
				require.Equal(t, http.MethodPost, r.Method)
				form := readSSODeviceForm(t, r)
				require.Equal(t, "private-device", form.Get("device_code"))
				require.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", form.Get("grant_type"))
				index := calls
				calls++
				if index >= len(sequence) {
					return nil, errors.New("unexpected extra token poll")
				}
				if mode == "cancel-during-request" {
					cancel()
				}
				if mode == "expire-during-request" {
					time.Sleep(20 * time.Millisecond)
				}
				if mode == "upstream-secret-echo" {
					return ssoDeviceResponse(400, nil, `{"error":"server_error","error_description":"private-device private-web-cookie"}`), nil
				}
				if sequence[index] == "success" {
					return ssoDeviceResponse(200, nil, `{"access_token":"issued-access","refresh_token":"issued-refresh","expires_in":3600}`), nil
				}
				return ssoDeviceResponse(400, nil, `{"error":"`+sequence[index]+`"}`), nil
			}), sleep: func(_ context.Context, interval time.Duration) error {
				sleeps = append(sleeps, interval)
				if mode == "expire-during-sleep" {
					time.Sleep(20 * time.Millisecond)
				}
				return nil
			}}
			expiry := time.Minute
			if mode == "expire-during-sleep" || mode == "expire-during-request" {
				expiry = 5 * time.Millisecond
			}
			token, err := flow.pollToken(ctx, "private-device", time.Second, expiry)
			switch mode {
			case "pending-slow-down-success":
				require.NoError(t, err)
				require.Equal(t, "issued-access", token.AccessToken)
				require.Equal(t, []time.Duration{time.Second, time.Second, 6 * time.Second, 6 * time.Second}, sleeps)
				require.Equal(t, 4, calls)
			case "repeated-slow-down":
				require.NoError(t, err)
				require.Equal(t, "issued-access", token.AccessToken)
				require.Equal(t, []time.Duration{time.Second, 6 * time.Second, 11 * time.Second}, sleeps)
				require.Equal(t, 3, calls)
			case "access-denied", "expired-token":
				require.ErrorIs(t, err, ErrSSOAuthorizationDenied)
				require.Nil(t, token)
				require.Equal(t, 1, calls)
				require.Equal(t, []time.Duration{time.Second}, sleeps)
			case "cancel-during-request":
				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, token)
				require.Equal(t, 1, calls)
			case "upstream-secret-echo":
				require.Error(t, err)
				require.Nil(t, token)
				require.Equal(t, 1, calls)
				require.NotContains(t, err.Error(), "private-device")
				require.NotContains(t, err.Error(), "private-web-cookie")
			case "expire-during-sleep":
				require.Error(t, err)
				require.Nil(t, token)
				require.Zero(t, calls, "an expired grant cannot be sent after the polling wait")
			case "expire-during-request":
				require.Error(t, err)
				require.Nil(t, token)
				require.Equal(t, 1, calls, "a late successful response cannot revive an expired grant")
			}
		})
	}
}

func TestSSODeviceGrantExpiryIncludesConsentStages(t *testing.T) {
	base := &ssoDeviceFakeClient{t: t}
	client := ssoLifecycleClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == SSOApproveURL {
			time.Sleep(1100 * time.Millisecond)
		}
		response, err := base.Do(r)
		if r.URL.String() == SSODeviceURL {
			_ = response.Body.Close()
			return ssoDeviceResponse(200, nil, `{"device_code":"device-1","user_code":"USER-1","verification_uri_complete":"https://auth.x.ai/oauth2/device/complete","interval":1,"expires_in":1}`), nil
		}
		return response, err
	})
	token, err := ConvertSSOToBuild(context.Background(), "private-web-cookie", &SSODeviceOptions{HTTPClient: client, Sleep: func(context.Context, time.Duration) error { return nil }})
	require.Error(t, err)
	require.Nil(t, token)
	require.Zero(t, base.tokenCalls, "consent time consumes the device grant lifetime")
}
