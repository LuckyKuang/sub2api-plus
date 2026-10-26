//go:build unit

package repository

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/firstbyte"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type cnControlRedirectRepo struct {
	service.AccountRepository
	writes int
}

func (r *cnControlRedirectRepo) UpdateCNControlExtraIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error) {
	r.writes++
	return true, nil
}

func (r *cnControlRedirectRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.writes++
	return nil
}

// The fixed credential issuer/approved relay is the only authority for control
// observations. Exercise the public services and production HTTPUpstream with
// local TLS servers; only certificate trust and DNS dialing are test supplied.
func TestCNAPIKeyControlProductionNeverFollowsRedirects(t *testing.T) {
	for _, probe := range []struct {
		platform, mode, base, path, body, authorization string
		quota                                           bool
	}{
		{service.PlatformDeepseek, "payg", "https://api.deepseek.com", "/user/balance", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.5"}]}`, "Bearer redirect-owner-key", false},
		{service.PlatformKimi, "payg", "https://api.moonshot.cn/v1", "/v1/users/me/balance", `{"code":0,"status":true,"data":{"available_balance":12.5}}`, "Bearer redirect-owner-key", false},
		{service.PlatformKimi, "coding", "https://api.kimi.com/coding/v1", "/coding/v1/usages", `{"usage":{"limit":100,"remaining":75}}`, "Bearer redirect-owner-key", true},
		{service.PlatformZhipu, "coding", "https://open.bigmodel.cn/api/coding/paas/v4", "/api/monitor/usage/quota/limit", `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}]}}`, "redirect-owner-key", true},
		{service.PlatformMiniMax, "coding", "https://api.minimaxi.com/v1", "/v1/api/openplatform/coding_plan/remains", `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":75}]}`, "Bearer redirect-owner-key", true},
	} {
		for _, status := range []int{http.StatusOK, http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			for _, destination := range []string{"same-origin", "different-origin"} {
				t.Run(fmt.Sprintf("%s/%s/%d/%s", probe.platform, probe.mode, status, destination), func(t *testing.T) {
					var issuerCalls, redirectedCalls atomic.Int64
					var redirectAuthorization atomic.Value
					second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						redirectedCalls.Add(1)
						redirectAuthorization.Store(r.Header.Get("Authorization"))
						_, _ = io.WriteString(w, probe.body)
					}))
					t.Cleanup(second.Close)
					base, err := url.Parse(probe.base)
					require.NoError(t, err)
					first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/redirected-observation" {
							redirectedCalls.Add(1)
							redirectAuthorization.Store(r.Header.Get("Authorization"))
							_, _ = io.WriteString(w, probe.body)
							return
						}
						issuerCalls.Add(1)
						require.Equal(t, probe.path, r.URL.Path)
						require.Equal(t, probe.authorization, r.Header.Get("Authorization"))
						if status == http.StatusOK {
							_, _ = io.WriteString(w, probe.body)
							return
						}
						location := "https://" + base.Host + "/redirected-observation"
						if destination == "different-origin" {
							location = "https://foreign-control.example/redirected-observation"
						}
						http.Redirect(w, r, location, status)
					}))
					t.Cleanup(first.Close)
					firstURL, err := url.Parse(first.URL)
					require.NoError(t, err)
					secondURL, err := url.Parse(second.URL)
					require.NoError(t, err)
					cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowPrivateHosts: true}}}
					upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
					entry, err := upstream.getOrCreateClient("", 879, 1)
					require.NoError(t, err)
					transport := first.Client().Transport.(*http.Transport).Clone()
					transport.TLSClientConfig.ServerName = first.Certificate().DNSNames[0]
					transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
						switch address {
						case base.Hostname() + ":443":
							address = firstURL.Host
						case "foreign-control.example:443":
							address = secondURL.Host
						default:
							return nil, fmt.Errorf("unexpected control destination %s", address)
						}
						return (&net.Dialer{}).DialContext(ctx, network, address)
					}
					entry.client.Transport = firstbyte.WrapTransport(transport)
					t.Cleanup(entry.client.CloseIdleConnections)
					account := &service.Account{ID: 879, Platform: probe.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
						Credentials: map[string]any{"api_key": "redirect-owner-key", "account_mode": probe.mode, "base_url": probe.base},
						Extra:       map[string]any{"retained": "last-known-observation"}}
					if probe.platform == service.PlatformZhipu {
						account.Credentials[service.SettingKeyOutboundIdentity] = service.OutboundIdentitySelection{
							Preset: zcode.Preset, Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"},
						}
					}
					repo := &cnControlRedirectRepo{}
					// The service allowlist deliberately approves only the first host.
					policy := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{base.Hostname()}}}}
					var success, persisted bool
					var observedStatus int
					if probe.quota {
						result, queryErr := service.NewCNProviderQuotaService(repo, nil, upstream, policy).QueryUsageForAccount(context.Background(), account)
						require.NoError(t, queryErr)
						success, persisted, observedStatus = result.Success, result.Persisted, result.StatusCode
					} else {
						result, queryErr := service.NewCNProviderBalanceService(repo, nil, upstream, policy).QueryBalanceForAccount(context.Background(), account)
						require.NoError(t, queryErr)
						success, persisted, observedStatus = result.Success, result.Persisted, result.StatusCode
					}
					require.Equal(t, int64(1), issuerCalls.Load())
					if status == http.StatusOK {
						require.True(t, success, "the same local TLS fixture must accept the issuer's direct response")
						require.True(t, persisted)
						require.Equal(t, 1, repo.writes)
						return
					}
					require.Zero(t, redirectedCalls.Load(), "a provider redirect cannot authorize another control request")
					require.Nil(t, redirectAuthorization.Load(), "no credential may leave on a second request")
					require.False(t, success)
					require.False(t, persisted)
					require.Equal(t, status, observedStatus)
					require.Zero(t, repo.writes, "redirects cannot refresh snapshots or enqueue scheduler changes")
					require.Equal(t, "last-known-observation", account.Extra["retained"])
				})
			}
		}
	}
}
