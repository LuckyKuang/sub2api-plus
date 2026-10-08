//go:build unit

package cnoauth

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStepFunOfficialLoginContract(t *testing.T) {
	for _, site := range []struct{ region, host string }{{"cn", "platform.stepfun.com"}, {"global", "platform.stepfun.ai"}} {
		t.Run(site.region, func(t *testing.T) {
			client := &Client{}
			flow, err := client.Start(context.Background(), "stepfun", site.region, "")
			require.NoError(t, err)
			u, err := url.Parse(flow.AuthorizeURL)
			require.NoError(t, err)
			require.Equal(t, "https", u.Scheme)
			require.Equal(t, site.host, u.Host)
			require.Equal(t, "/cli-login", u.Path)
			require.Equal(t, "53683", u.Query().Get("port"))
			require.NotEmpty(t, u.Query().Get("state"))
			require.Len(t, u.Query(), 2)
			require.Empty(t, flow.DeviceCode)
			require.Empty(t, flow.Verifier)
			_, err = client.Poll(context.Background(), flow, "")
			require.ErrorIs(t, err, ErrPending, "Step does not have a device token polling endpoint")
			for _, field := range []string{"api_key", "apiKey", "access_token", "accessToken"} {
				grant, err := client.Exchange(context.Background(), flow, "http://127.0.0.1:53683/callback?state="+flow.State+"&"+field+"=official-key", "")
				require.NoError(t, err)
				require.Equal(t, "official-key", grant.AccessToken)
				require.Empty(t, grant.RefreshToken)
				require.True(t, grant.ExpiresAt.IsZero(), "static credential must not acquire a fabricated lifetime")
			}
			_, err = client.Refresh(context.Background(), "stepfun", site.region, "not-a-real-refresh-token", "")
			require.Error(t, err)
		})
	}
}

func TestStepFunCallbackMustMatchSessionBeforeAcceptingCredential(t *testing.T) {
	flow := &Flow{Platform: "stepfun", Region: "cn", State: "expected", ExpiresAt: time.Now().Add(time.Minute)}
	for _, raw := range []string{
		"key-only", "http://127.0.0.1:53683/callback?api_key=k",
		"http://127.0.0.1:53683/callback?state=wrong&api_key=k",
		"http://127.0.0.1:53683/callback?state=expected&state=expected&api_key=k",
		"http://evil.test:53683/callback?state=expected&api_key=k",
		"http://127.0.0.1:9999/callback?state=expected&api_key=k",
		"http://127.0.0.1:53683/other?state=expected&api_key=k",
		"http://user@127.0.0.1:53683/callback?state=expected&api_key=k",
		"http://127.0.0.1:53683/callback?state=expected&api_key=k#fragment",
		"http://127.0.0.1:53683/callback?state=expected&api_key=k&access_token=other",
		"http://127.0.0.1:53683/callback?state=expected&api_key=k&api_key=k",
		"http://127.0.0.1:53683/callback?state=expected&api_key=k%0D%0AInjected:x",
		"http://127.0.0.1:53683/callback?state=expected&api_key=SuB2ApI-secret",
		"http://127.0.0.1:53683/callback?state=expected&code=unexchangeable",
		"http://127.0.0.1:53683/callback?state=expected&error=access_denied&api_key=k",
		"http://127.0.0.1:53683/callback?state=expected&api_key=k&expires_in=-1",
	} {
		t.Run(raw, func(t *testing.T) { _, err := exchangeStepFun(flow, raw); require.Error(t, err) })
	}
	grant, err := exchangeStepFun(flow, "http://127.0.0.1:53683/callback?state=expected&api_key=k&expires_in=3600")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), grant.ExpiresAt, time.Second)
	flow.ExpiresAt = time.Now().Add(-time.Second)
	_, err = exchangeStepFun(flow, "http://127.0.0.1:53683/callback?state=expected&api_key=k")
	require.ErrorIs(t, err, ErrExpired)
}
