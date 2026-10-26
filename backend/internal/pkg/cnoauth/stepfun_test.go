//go:build unit

package cnoauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Expectations come from Step-Code be88ee77 step-provider/login-client.ts.
func TestStepFunOfficialLoginContract(t *testing.T) {
	for _, site := range []struct{ region, host, profile string }{{"cn", "platform.stepfun.com", "step_plan"}, {"global", "platform.stepfun.ai", "step_plan_oversea"}} {
		t.Run(site.region, func(t *testing.T) {
			secret := ""
			calls := 0
			client := &Client{HTTP: doerFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, site.host, r.URL.Host)
				require.Equal(t, "stepcode (linux 6.8.0-31-generic; x64)", r.UserAgent())
				require.Empty(t, r.Header.Get("X-Stainless-Package-Version"))
				calls++
				if calls == 1 {
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, "/api/stdhttp/v1/cli-login/init", r.URL.Path)
					secret = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
					require.Len(t, secret, 64)
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, site.profile, body["profile"])
					require.Equal(t, "https://"+site.host, body["origin"])
					require.Equal(t, map[string]any{"name": "stepcode", "version": "0.84.4", "platform": "linux-x64"}, body["client"])
					return reply(map[string]any{"status": 0, "flow_id": "flow-123", "poll_interval_sec": 2, "expires_at": time.Now().Add(5 * time.Minute).Unix()}), nil
				}
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/api/stdhttp/v1/cli-login/poll/flow-123", r.URL.Path)
				require.Equal(t, "Bearer "+secret, r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), secret)
				if r.Body != nil {
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Empty(t, body)
				}
				if calls == 2 {
					return reply(map[string]any{"state": "pending"}), nil
				}
				return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "model-key", "login": map[string]any{"access_token": "session-only", "refresh_token": "refresh-only", "device_id": "device", "app_id": "app", "expires_in": 300}}}), nil
			})}
			flow, err := client.Start(context.Background(), "stepfun", site.region, "")
			require.NoError(t, err)
			u, err := url.Parse(flow.AuthorizeURL)
			require.NoError(t, err)
			require.Equal(t, "/cli-login-remote", u.Path)
			require.Equal(t, url.Values{"flow_id": {"flow-123"}}, u.Query())
			require.NotContains(t, flow.AuthorizeURL, secret)
			_, err = client.Poll(context.Background(), flow, "")
			require.ErrorIs(t, err, ErrPending)
			grant, err := client.Poll(context.Background(), flow, "")
			require.NoError(t, err)
			require.Equal(t, "model-key", grant.AccessToken)
			require.Empty(t, grant.RefreshToken)
			require.True(t, grant.ExpiresAt.IsZero())
			require.Equal(t, "session-only", grant.StepLogin.AccessToken)
			_, err = client.Exchange(context.Background(), flow, "http://127.0.0.1:53683/callback?api_key=other", "")
			require.Error(t, err)
			require.Equal(t, 3, calls, "obsolete callback must not cause a network request")
			flow.ExpiresAt = time.Now().Add(-time.Second)
			_, err = client.Poll(context.Background(), flow, "")
			require.ErrorIs(t, err, ErrExpired)
			require.Equal(t, 3, calls)
		})
	}
}

func TestStepFunRejectsInvalidCloudProtocolAndKeepsUsableKey(t *testing.T) {
	for _, payload := range []map[string]any{
		{"flow_id": "../../secret", "poll_interval_sec": 1, "expires_at": time.Now().Add(time.Minute).Unix()},
		{"flow_id": "flow", "poll_interval_sec": 0, "expires_at": time.Now().Add(time.Minute).Unix()},
		{"flow_id": "flow", "poll_interval_sec": 1, "expires_at": 1},
		{"status": 1, "flow_id": "flow", "poll_interval_sec": 1, "expires_at": time.Now().Add(time.Minute).Unix()},
	} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) { return reply(payload), nil })}
		_, err := c.Start(context.Background(), "stepfun", "cn", "")
		require.Error(t, err)
	}
	for _, tc := range []struct {
		body  string
		want  error
		valid bool
	}{
		{`{"state":"failed","reason":"denied"}`, ErrDenied, false},
		{`{"state":"unexpected"}`, nil, false},
		{`{"state":"ready","credential":{"api_key":"bad\r\nkey"}}`, nil, false},
		{`{"state":"ready","credential":{"api_key":"usable-key","login":{"access_token":"incomplete"}}}`, nil, true},
	} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		grant, err := c.Poll(context.Background(), &Flow{Platform: "stepfun", Region: "cn", FlowID: "flow", DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute)}, "")
		if tc.valid {
			require.NoError(t, err)
			require.Equal(t, "usable-key", grant.AccessToken)
			require.Nil(t, grant.StepLogin)
		} else {
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
		}
	}
}

func TestStepFunRejectsMalformedInitialization(t *testing.T) {
	for _, response := range []map[string]any{
		{"flow_id": "../escape", "poll_interval_sec": 2, "expires_at": 2000000000},
		{"flow_id": "flow", "poll_interval_sec": 0, "expires_at": 2000000000},
		{"flow_id": "flow", "poll_interval_sec": 2, "expires_at": 1},
		{"status": 1, "flow_id": "flow", "poll_interval_sec": 2, "expires_at": 2000000000},
	} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) { return reply(response), nil })}
		_, err := c.Start(context.Background(), "stepfun", "cn", "")
		require.Error(t, err)
	}
}

func TestStepFunOptionalSessionFailurePreservesModelCredential(t *testing.T) {
	c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "model-key", "login": map[string]any{"access_token": "incomplete"}}}), nil
	})}
	f := &Flow{Platform: "stepfun", Region: "cn", FlowID: "flow", DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute)}
	grant, err := c.Poll(context.Background(), f, "")
	require.NoError(t, err)
	require.Equal(t, "model-key", grant.AccessToken)
	require.Nil(t, grant.StepLogin)
	require.True(t, grant.ExpiresAt.IsZero())
	f.ExpiresAt = time.Now().Add(-time.Second)
	_, err = c.Poll(context.Background(), f, "")
	require.ErrorIs(t, err, ErrExpired)
}

func TestStepFunCancelledResponseCannotDeliverGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "late"}}), nil
	})}
	_, err := c.Poll(ctx, &Flow{Platform: "stepfun", Region: "cn", FlowID: "flow", DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute)}, "")
	require.ErrorIs(t, err, context.Canceled)
}
