//go:build unit

package cnoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/kimi"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func reply(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}
}
func TestOfficialDeviceProtocols(t *testing.T) {
	for _, platform := range []string{"kimi", "minimax"} {
		for _, region := range []string{"cn", "global"} {
			for _, accountShape := range []bool{false, true} {
				if platform == "kimi" && accountShape {
					continue
				}
				t.Run(platform+region+map[bool]string{true: "account", false: "standard"}[accountShape], func(t *testing.T) {
					calls := 0
					challenge := ""
					c := &Client{HTTP: doerFunc(func(r *http.Request) (*http.Response, error) {
						require.Equal(t, origin(platform, region), r.URL.Scheme+"://"+r.URL.Host)
						require.NotEmpty(t, r.UserAgent())
						require.Empty(t, r.Header.Get("Authorization"))
						require.NoError(t, r.ParseForm())
						calls++
						if calls == 1 {
							require.NotEmpty(t, r.Form.Get("client_id"))
							if platform == "minimax" {
								challenge = r.Form.Get("code_challenge")
								require.Equal(t, "S256", r.Form.Get("code_challenge_method"))
								require.Equal(t, "agent.default", r.Form.Get("scope"))
								require.Equal(t, "agent-backend", r.Form.Get("audience"))
							}
							if accountShape {
								return reply(map[string]any{"user_code": "code", "verification_url": origin(platform, region) + "/verify", "expired_in": time.Now().Add(5 * time.Minute).UnixMilli(), "interval": 2000}), nil
							}
							return reply(map[string]any{"device_code": "secret-device", "user_code": "code", "verification_uri_complete": origin(platform, region) + "/verify?user_code=code", "expires_in": 300, "interval": 2}), nil
						}
						if calls == 2 {
							require.Equal(t, deviceGrant, r.Form.Get("grant_type"))
							if accountShape {
								require.Equal(t, "code", r.Form.Get("user_code"))
								require.Empty(t, r.Form.Get("device_code"))
							} else {
								require.Equal(t, "secret-device", r.Form.Get("device_code"))
							}
							if platform == "minimax" {
								hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
								require.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(hash[:]))
							}
						} else {
							require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
							require.Equal(t, "refresh", r.Form.Get("refresh_token"))
						}
						return reply(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "token_type": "Bearer", "scope": []string{"agent.default"}, "status": "authorized"}), nil
					})}
					flow, err := c.Start(context.Background(), platform, region, "")
					require.NoError(t, err)
					require.Equal(t, 2, flow.Interval)
					grant, err := c.Poll(context.Background(), flow, "")
					require.NoError(t, err)
					require.Equal(t, "access", grant.AccessToken)
					grant, err = c.Refresh(context.Background(), platform, region, grant.RefreshToken, "")
					require.NoError(t, err)
					require.Equal(t, "access", grant.AccessToken)
					require.Equal(t, 3, calls)
				})
			}
		}
	}
}
func TestDeepSeekPKCEAndState(t *testing.T) {
	var challenge, state string
	calls := 0
	c := &Client{HTTP: doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "platform.deepseek.com", r.URL.Host)
		require.NotEmpty(t, r.Header.Get("x-client-version"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		value := map[string]any{}
		if calls == 1 {
			challenge = str(body, "code_challenge")
			state = str(body, "state")
			require.Equal(t, DeepSeekRedirect, body["redirect_uri"])
			value = map[string]any{"authorize_id": "authorization", "authorize_url": "https://platform.deepseek.com/dsh/authorize?id=authorization", "expires_in": 300}
		} else {
			hash := sha256.Sum256([]byte(str(body, "code_verifier")))
			require.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(hash[:]))
			require.Equal(t, "code", str(body, "code"))
			require.NotEmpty(t, str(body, "device_id"))
			require.Equal(t, deviceModel(), str(body, "device_model"))
			value = map[string]any{"token": "opaque-grant"}
		}
		return reply(map[string]any{"code": 0, "data": map[string]any{"biz_code": 0, "biz_data": value}}), nil
	})}
	flow, err := c.Start(context.Background(), "deepseek", "cn", "")
	require.NoError(t, err)
	for _, callback := range []string{"code", DeepSeekRedirect + "?code=code&state=wrong", DeepSeekRedirect + "?code=code&state=" + state + "&state=" + state, "https://evil.test/oauth/callback?code=code&state=" + state} {
		_, err = c.Exchange(context.Background(), flow, callback, "")
		require.Error(t, err)
	}
	require.Equal(t, 1, calls)
	grant, err := c.Exchange(context.Background(), flow, DeepSeekRedirect+"?"+url.Values{"code": {"code"}, "state": {state}}.Encode(), "")
	require.NoError(t, err)
	require.Equal(t, "opaque-grant", grant.AccessToken)
	require.Empty(t, grant.RefreshToken)
	_, err = c.Refresh(context.Background(), "deepseek", "cn", "", "")
	require.Error(t, err)
	require.Equal(t, 2, calls)
}
func TestOAuthErrorsAndIdentitySnapshot(t *testing.T) {
	identity := kimi.DefaultIdentity()
	identity.UserAgent = "kimi-code/9.9.9"
	identity.Version = "9.9.9"
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	for _, tc := range []struct {
		body map[string]any
		err  error
	}{
		{map[string]any{"error": "authorization_pending"}, ErrPending}, {map[string]any{"status": "slow_down"}, ErrSlowDown}, {map[string]any{"status": "denied"}, ErrDenied}, {map[string]any{"error": "expired_token"}, ErrExpired}, {map[string]any{"error": "invalid_grant"}, ErrInvalidGrant},
	} {
		c := &Client{HTTP: doerFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, identity.UserAgent, r.UserAgent())
			return reply(tc.body), nil
		})}
		_, err := c.Poll(ctx, &Flow{Platform: "minimax", Region: "cn", ExpiresAt: time.Now().Add(time.Minute)}, "")
		require.ErrorIs(t, err, tc.err)
	}
	for _, expiry := range []any{"NaN", "+Inf", -1, 0, 1e30} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
			return reply(map[string]any{"access_token": "a", "refresh_token": "r", "expires_in": expiry}), nil
		})}
		_, err := c.Refresh(ctx, "kimi", "cn", "r", "")
		require.Error(t, err)
	}
}
