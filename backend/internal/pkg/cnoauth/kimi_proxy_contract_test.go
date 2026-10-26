//go:build unit

package cnoauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// The literal URL and form contracts come from MoonshotAI/kimi-code
// packages/oauth/src/{constants,oauth}.ts, independently of this client's helpers.
func TestKimiCNLoginExactWireContract(t *testing.T) {
	identity := outboundidentity.Identity{Preset: "kimi", UserAgent: "kimi-code-cli/2.1.1", Version: "2.1.1", Source: "account", Headers: map[string]string{
		"User-Agent": "kimi-code-cli/2.1.1", "X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": "2.1.1",
		"X-Msh-Device-Name": "contract-host", "X-Msh-Device-Model": "Linux 6.8 x64", "X-Msh-Os-Version": "6.8", "X-Msh-Device-Id": "00000000-0000-4000-8000-000000000071",
	}}
	cases := []struct {
		name, target string
		form         url.Values
	}{
		{"start", "https://auth.kimi.com/api/oauth/device_authorization", url.Values{"client_id": {"17e5f671-d194-4dfb-9706-5516cb48c098"}}},
		{"poll", "https://auth.kimi.com/api/oauth/token", url.Values{"client_id": {"17e5f671-d194-4dfb-9706-5516cb48c098"}, "device_code": {"private-device"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}},
		{"refresh", "https://auth.kimi.com/api/oauth/token", url.Values{"client_id": {"17e5f671-d194-4dfb-9706-5516cb48c098"}, "refresh_token": {"private-refresh"}, "grant_type": {"refresh_token"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &Client{HTTP: doerFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, tc.target, r.URL.String())
				require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				require.NoError(t, r.ParseForm())
				require.Equal(t, tc.form, r.PostForm)
				require.Equal(t, "kimi-code-cli/2.1.1", r.UserAgent())
				require.Equal(t, "kimi_code_cli", r.Header.Get("X-Msh-Platform"))
				require.Equal(t, "2.1.1", r.Header.Get("X-Msh-Version"))
				require.Equal(t, "contract-host", r.Header.Get("X-Msh-Device-Name"))
				require.Equal(t, "Linux 6.8 x64", r.Header.Get("X-Msh-Device-Model"))
				require.Equal(t, "6.8", r.Header.Get("X-Msh-Os-Version"))
				require.Equal(t, "00000000-0000-4000-8000-000000000071", r.Header.Get("X-Msh-Device-Id"))
				for _, header := range []string{"Authorization", "X-Api-Key", "Originator", "Version", "X-Stainless-Package-Version"} {
					require.Empty(t, r.Header.Get(header), header)
				}
				if tc.name == "start" {
					return reply(map[string]any{"device_code": "private-device", "user_code": "public-code", "verification_uri_complete": "https://auth.kimi.com/verify?user_code=public-code", "expires_in": 300}), nil
				}
				return reply(map[string]any{"access_token": "private-access", "refresh_token": "private-refresh", "expires_in": 3600}), nil
			})}
			ctx := outboundidentity.WithIdentity(context.Background(), identity)
			var err error
			switch tc.name {
			case "start":
				_, err = client.Start(ctx, "kimi", "cn", "")
			case "poll":
				_, err = client.Poll(ctx, &Flow{Platform: "kimi", Region: "cn", DeviceCode: "private-device", ExpiresAt: time.Now().Add(time.Minute)}, "")
			case "refresh":
				_, err = client.Refresh(ctx, "kimi", "cn", "private-refresh", "")
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestKimiProductionClientProxyFailuresNeverDialDirect(t *testing.T) {
	for _, operation := range []string{"start", "poll", "refresh"} {
		for _, failure := range []string{"timeout", "refused", "authentication-required"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				var proxyRequests, proxyDials, directDials atomic.Int64
				release := make(chan struct{})
				var closeRelease sync.Once
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyRequests.Add(1)
					if r.Method != http.MethodConnect || r.Host != "auth.kimi.com:443" {
						t.Errorf("unexpected proxy request: %s %s", r.Method, r.Host)
					}
					if failure == "authentication-required" {
						w.Header().Set("Proxy-Authenticate", `Basic realm="contract"`)
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					}
					<-release
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { closeRelease.Do(func() { close(release) }) })
				parsed, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				parsed.User = url.UserPassword("owner", "private-proxy-password")
				if failure == "refused" {
					proxy.Close()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{ConnectStart: func(_, addr string) {
					if addr == parsed.Host {
						proxyDials.Add(1)
					} else {
						directDials.Add(1)
						cancel()
					}
				}})
				client := &Client{} // The production pooled HTTP client and proxy dialer.
				switch operation {
				case "start":
					_, err = client.Start(ctx, "kimi", "cn", parsed.String())
				case "poll":
					_, err = client.Poll(ctx, &Flow{Platform: "kimi", Region: "cn", DeviceCode: "private-device", ExpiresAt: time.Now().Add(time.Minute)}, parsed.String())
				case "refresh":
					_, err = client.Refresh(ctx, "kimi", "cn", "private-refresh", parsed.String())
				}
				require.Error(t, err)
				require.Positive(t, proxyDials.Load(), "the production transport must attempt the selected proxy")
				if failure != "refused" {
					require.Positive(t, proxyRequests.Load())
				}
				require.Zero(t, directDials.Load(), "proxy failure cannot authorize a direct TCP connection")
				for _, secret := range []string{"private-proxy-password", "private-device", "private-refresh"} {
					require.NotContains(t, err.Error(), secret)
				}
			})
		}
	}
}
