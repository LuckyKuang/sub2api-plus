//go:build unit

package cnoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Oracle: Step-Code@81e79bc4b699148d6823fedde9b5e70d9a80255f,
// packages/providers/src/step-provider/login-client.ts. Flow IDs are public;
// poll credentials stay exclusively in the request Authorization header.
func TestStepFunCloudPollingKeepsModelKeySeparateFromAccountSession(t *testing.T) {
	for _, region := range []string{"cn", "global"} {
		t.Run(region, func(t *testing.T) {
			host, profile := "platform.stepfun.com", "step_plan"
			if region == "global" {
				host, profile = "platform.stepfun.ai", "step_plan_oversea"
			}
			calls := 0
			pollToken := ""
			client := &Client{HTTP: doerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, host, req.URL.Host)
				require.Equal(t, "https", req.URL.Scheme)
				require.Equal(t, "stepcode (linux 6.8.0-31-generic; x64)", req.UserAgent())
				require.Empty(t, req.Header.Get("X-Stainless-Package-Version"))
				require.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Bearer "))
				if calls == 1 {
					pollToken = strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
					require.NotEmpty(t, pollToken)
					require.Equal(t, http.MethodPost, req.Method)
					require.Equal(t, "/api/stdhttp/v1/cli-login/init", req.URL.Path)
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					require.Equal(t, profile, body["profile"])
					require.Equal(t, "https://"+host, body["origin"])
					return reply(map[string]any{"status": 0, "flow_id": "public-flow", "poll_interval_sec": 2, "expires_at": time.Now().Add(time.Minute).Unix()}), nil
				}
				require.Equal(t, "Bearer "+pollToken, req.Header.Get("Authorization"))
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/api/stdhttp/v1/cli-login/poll/public-flow", req.URL.Path)
				require.Empty(t, req.URL.RawQuery)
				if calls == 2 {
					return reply(map[string]any{"state": "pending"}), nil
				}
				return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "model-key", "login": map[string]any{"access_token": "account-access", "refresh_token": "account-refresh", "device_id": "device", "app_id": "app", "expires_in": 1800}}}), nil
			})}
			flow, err := client.Start(context.Background(), "stepfun", region, "")
			require.NoError(t, err)
			require.Equal(t, 1, calls, "login must initiate the official server-side flow")
			authorize, err := url.Parse(flow.AuthorizeURL)
			require.NoError(t, err)
			require.Equal(t, "https://"+host+"/cli-login-remote", authorize.Scheme+"://"+authorize.Host+authorize.Path)
			require.Equal(t, url.Values{"flow_id": {"public-flow"}}, authorize.Query())
			require.NotContains(t, flow.AuthorizeURL, pollToken)
			_, err = client.Poll(context.Background(), flow, "")
			require.ErrorIs(t, err, ErrPending)
			grant, err := client.Poll(context.Background(), flow, "")
			require.NoError(t, err)
			require.Equal(t, "model-key", grant.AccessToken)
			require.Empty(t, grant.RefreshToken, "account refresh is not the model key's refresh token")
			require.True(t, grant.ExpiresAt.IsZero(), "account TTL must not expire the static model key")
			serialized, err := json.Marshal(grant)
			require.NoError(t, err)
			require.Contains(t, string(serialized), "account-refresh", "optional account session must survive server-side persistence")
			_, err = client.Exchange(context.Background(), flow, "http://127.0.0.1:53683/callback?state=old&api_key=untrusted", "")
			require.Error(t, err, "retired callback import must not become an alternate grant source")
			require.Equal(t, 3, calls)
		})
	}
}
