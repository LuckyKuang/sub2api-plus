//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

// URLs and authentication are literal contracts from the pinned official Kimi,
// DeepSeek and Step clients, independently of the gateway's base URL helpers.
func TestDomesticOAuthOfficialModelDiscovery(t *testing.T) {
	for _, tc := range []struct{ platform, region, target, auth string }{
		{"kimi", "cn", "https://api.kimi.com/coding/v1/models", "Bearer owner-token"},
		{"kimi", "global", "https://api.kimi.ai/coding/v1/models", "Bearer owner-token"},
		{"deepseek", "cn", "https://api.deepseek.com/models", ""},
	} {
		t.Run(tc.platform+tc.region, func(t *testing.T) {
			a := &Account{ID: 74, Platform: tc.platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: tc.platform, Region: tc.region}, &cnoauth.Grant{AccessToken: "owner-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})}
			a.Credentials["base_url"] = "https://attacker.example"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"official-model","context_window":262144,"input_modalities":["text","image"]}]}`))}}
			tester := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
			req, err := tester.buildUpstreamModelsRequest(context.Background(), a)
			require.NoError(t, err)
			_, err = tester.doUpstreamModelsRequest(req, a)
			require.NoError(t, err)
			require.Equal(t, tc.target, upstream.lastReq.URL.String())
			require.Equal(t, tc.auth, upstream.lastReq.Header.Get("Authorization"))
			if tc.platform == "deepseek" {
				require.Equal(t, "owner-token", upstream.lastReq.Header.Get("x-dsh-auth-token"))
			}
			require.Equal(t, builtInOutboundIdentity(tc.platform).UserAgent, upstream.lastReq.UserAgent())
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			req.URL.Host = "attacker.example"
			_, err = tester.doUpstreamModelsRequest(req, a)
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestDomesticOAuthOfficialQuotaAndUnknownSnapshot(t *testing.T) {
	for _, tc := range []struct{ platform, region, target, body string }{
		{"kimi", "cn", "https://api.kimi.com/coding/v1/usages", `{"usages":{"limit_5h":{"used_ratio":"0.25"}}}`},
		{"kimi", "global", "https://api.kimi.ai/coding/v1/usages", `{"usages":{"limit_5h":{"used_ratio":0.25}}}`},
		{"stepfun", "cn", "https://api.stepfun.com/step_plan/v1/usage", `{"schema_version":1,"scope":"personal_account","observed_at_ms":1792900000000,"account":{"id":"42"},"plan":{"state":"known","family":"coding","name":"Plan","subscription":null},"quota":{"state":"known","allowed":true,"reason":null,"windows":[{"id":"five_hour","limited":true,"used_percent":25,"resets_at_ms":1793000000000},{"id":"weekly","limited":false,"used_percent":null,"resets_at_ms":null}],"credit_buckets":[]}}`},
	} {
		t.Run(tc.platform+tc.region, func(t *testing.T) {
			a := &Account{ID: 74, Platform: tc.platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: tc.platform, Region: tc.region}, &cnoauth.Grant{AccessToken: "owner-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})}
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com", "api.kimi.ai", "api.stepfun.com"))
			result, err := svc.QueryUsage(context.Background(), a.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.NotEmpty(t, result.Tiers)
			require.Equal(t, 25.0, result.Tiers[0].UsedPercent)
			require.Equal(t, tc.target, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer owner-token", upstream.lastReq.Header.Get("Authorization"))
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			repo.updates = nil
			upstream.resp = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"future_schema":{}}`))}
			result, err = svc.QueryUsage(context.Background(), a.ID)
			require.NoError(t, err)
			require.False(t, result.Success, "unknown must not become sufficient quota")
			require.False(t, result.Persisted)
			require.Nil(t, repo.updates, "unknown response must preserve existing snapshot")
		})
	}
}

func TestDeepSeekOAuthWalletUsesAccountIssuerAndPreservesUnknown(t *testing.T) {
	a := &Account{ID: 74, Platform: PlatformDeepseek, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: PlatformDeepseek, Region: "cn"}, &cnoauth.Grant{AccessToken: "account-token"})}
	repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"10.25"},{"currency":"USD","balance":"1e+2"}],"bonus_wallets":[{"currency":"CNY","balance":"0E-16"}]}}}`))}}
	svc := NewCNProviderBalanceService(repo, nil, upstream, cnProbeAllowlistConfig("platform.deepseek.com"))
	result, err := svc.QueryBalance(context.Background(), 74)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, 10.25, result.Balance)
	require.Equal(t, "CNY", result.Currency)
	require.Len(t, result.Balances, 2)
	require.Equal(t, 100.0, result.Balances[1].Balance)
	require.Equal(t, "https://platform.deepseek.com/api/v0/users/get_user_summary", upstream.lastReq.URL.String())
	require.Equal(t, "account-token", upstream.lastReq.Header.Get("x-dsh-auth-token"))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	repo.updates = nil
	upstream.resp = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"USD","balance":"bad"}],"bonus_wallets":[]}}}`))}
	result, err = svc.QueryBalance(context.Background(), 74)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, result.Persisted)
	require.Nil(t, repo.updates)
}
