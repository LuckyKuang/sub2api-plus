//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

type cnQuotaRecoveryRepo struct {
	refreshAPIAccountRepo
	updates map[string]any
}

func (r *cnQuotaRecoveryRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

func (r *cnQuotaRecoveryRepo) UpdateCNProviderQuotaIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	currentCredentials, err := json.Marshal(r.account.Credentials)
	if err != nil {
		return false, err
	}
	expectedCredentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	if r.account.ID != expected.ID || r.account.Platform != expected.Platform || r.account.Type != expected.Type ||
		r.account.Status != expected.Status || string(currentCredentials) != string(expectedCredentials) ||
		!cnOAuthSameProxyID(r.account.ProxyID, expected.ProxyID) || r.account.proxyURLOrEmpty() != expected.proxyURLOrEmpty() {
		return false, nil
	}
	return true, r.UpdateExtra(ctx, expected.ID, updates)
}

func TestKimiManagedUsageFourIndependentWindows(t *testing.T) {
	body := []byte(`{"usages":{"limit_5h":{"used_ratio":0.25,"reset_time":"2026-11-01T01:00:00Z"},"limit_7d":{"used_ratio":"0.75","reset_time":"2026-11-07T00:00:00Z"},"limit_month_total":{"used_ratio":0.9,"reset_time":"2026-12-01T00:00:00Z"},"limit_month_code":{"used_ratio":"0.1","reset_time":"2026-12-02T00:00:00Z"}},"boosterWallet":{"balance":999}}`)
	tiers := parseKimiUsageTiers(body)
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 25, ResetAt: "2026-11-01T01:00:00Z"}, {Window: "weekly", UsedPercent: 75, ResetAt: "2026-11-07T00:00:00Z"}, {Window: "monthly", UsedPercent: 90, ResetAt: "2026-12-01T00:00:00Z"}, {Window: "monthly_code", UsedPercent: 10, ResetAt: "2026-12-02T00:00:00Z"}}, tiers)
	for _, body := range []string{`{}`, `{"usages":{}}`, `{"usages":{"limit_5h":{"used_ratio":"NaN"},"limit_7d":{"used_ratio":"Infinity"},"limit_month_total":{"used_ratio":-1}}}`, `{"limits":[{"detail":{}}],"usage":{}}`, `{"usages":`} {
		require.Empty(t, parseKimiUsageTiers([]byte(body)), body)
	}
	tiers = parseKimiUsageTiers([]byte(`{"usages":{"limit_5h":{"used_ratio":"bad"},"limit_month_code":{"used_ratio":1.1}}}`))
	require.Len(t, tiers, 1)
	require.Equal(t, "monthly_code", tiers[0].Window)
	require.InDelta(t, 110, tiers[0].UsedPercent, 0.0001)
}

func TestKimiManagedQuotaUsesFixedRegionalGrantAnd401Recovery(t *testing.T) {
	for _, region := range []string{"cn", "global"} {
		t.Run(region, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformKimi)
			a.Credentials["oauth_region"] = region
			a.Credentials["base_url"] = "https://attacker.invalid"
			a.Credentials["api_key"] = "forbidden-api-key"
			repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
				refreshes++
				return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`), nil
			})}
			calls := 0
			var identity, device string
			upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
				calls++
				host := "api.kimi.com"
				if region == "global" {
					host = "api.kimi.ai"
				}
				require.Equal(t, "https://"+host+"/coding/v1/usages", req.URL.String())
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				require.Empty(t, req.Header.Get("X-Stainless-Package-Version"))
				require.NotEmpty(t, req.Header.Get("X-Msh-Device-Id"))
				if calls == 1 {
					identity = req.UserAgent()
					device = req.Header.Get("X-Msh-Device-Id")
					require.Equal(t, "Bearer old", req.Header.Get("Authorization"))
					return cnRecoveryResponse(401, `{}`), nil
				}
				require.Equal(t, "Bearer new", req.Header.Get("Authorization"))
				require.Equal(t, identity, req.UserAgent())
				require.Equal(t, device, req.Header.Get("X-Msh-Device-Id"))
				return cnRecoveryResponse(200, `{"usages":{"limit_5h":{"used_ratio":0.25,"reset_time":"2026-11-01T00:00:00Z"}}}`), nil
			}}
			svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com", "api.kimi.ai"))
			svc.cnOAuthService = oauth
			selectedGeneration := cnQuotaGeneration(a)
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Equal(t, float64(25), repo.updates["kimi_5h_used_percent"])
			require.Equal(t, cnQuotaGeneration(repo.account), repo.updates["kimi_quota_generation"], "persist the actual grant used after 401 recovery")
			require.NotEqual(t, selectedGeneration, repo.updates["kimi_quota_generation"], "the selected pre-refresh grant must not own the snapshot")
			require.Equal(t, 1, refreshes)
			require.Equal(t, 2, calls)
		})
	}
}

func TestKimiInvalidQuotaResponseNeverOverwritesSnapshot(t *testing.T) {
	for _, body := range []string{`{}`, `{"usages":{"limit_5h":{"used_ratio":"NaN"}}}`, `{"usages":`, strings.Repeat(" ", cnQuotaMaxBodyBytes+1)} {
		t.Run(body[:min(16, len(body))], func(t *testing.T) {
			a := codingAccount(PlatformKimi)
			repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
			upstream := &cnRecoveryUpstream{send: func(*http.Request) (*http.Response, error) { return cnRecoveryResponse(200, body), nil }}
			svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com"))
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.NotEmpty(t, result.Error)
			require.Nil(t, repo.updates)
		})
	}
}

func TestKimiQuotaNaturalRefreshBindsFreshGrantForThresholds(t *testing.T) {
	a := cnRecoveryAccount(PlatformKimi)
	a.Credentials["expires_at"] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
	repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
	oauth := NewCNOAuthService(nil, repo, nil)
	oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	refreshes := 0
	oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		refreshes++
		return cnRecoveryResponse(200, `{"access_token":"natural-new","refresh_token":"natural-rotated","expires_in":3600}`), nil
	})}
	auth := make(chan string, 1)
	upstream := cnQuotaGenerationHTTPUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth <- req.Header.Get("Authorization")
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":0.95,"reset_time":%q}}}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	}))
	svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("api.kimi.com"))
	svc.cnOAuthService = oauth
	selectedGeneration := cnQuotaGeneration(a)
	result, err := svc.QueryUsageForAccount(context.Background(), a)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, "Bearer natural-new", <-auth)
	require.Equal(t, 1, refreshes)
	current, err := snapshotCNQuotaAccount(repo.account)
	require.NoError(t, err)
	current.Extra = repo.updates
	require.Equal(t, 95.0, current.Extra["kimi_5h_used_percent"])
	require.NotEqual(t, selectedGeneration, current.Extra["kimi_quota_generation"])
	decision := EvaluateAccountSchedulingThreshold(current, map[string]int{PlatformKimi: 80}, time.Now())
	require.True(t, decision.ShouldPause, "the refreshed grant's exhausted window must still protect its own account")
	require.Equal(t, "5h", decision.Window)
}

func TestZhipuLinkedQuotaUsesMemoryPATRawAuthAndTeamScope(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan} {
			t.Run(provider+"/"+plan, func(t *testing.T) {
				a := projectTokenTestAccount(42)
				a.Credentials["oauth_provider"] = provider
				a.Credentials["plan_kind"] = plan
				a.Credentials["account_mode"] = AccountModeCoding
				a.Credentials["base_url"] = "https://attacker.invalid"
				a.Credentials["zhipu_project_key_id"] = "location"
				a.Credentials["zhipu_organization"] = "org"
				a.Credentials["zhipu_project"] = "project"
				delete(a.Credentials, "api_key")
				client := &projectTokenTestClient{}
				client.businessToken = "zai-business"
				manager := newProjectTokenTestService(t, client)
				repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
				calls := 0
				var rejected string
				upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
					calls++
					host := "open.bigmodel.cn"
					if provider == "zai" {
						host = "api.z.ai"
					}
					wantURL := "https://" + host + "/api/monitor/usage/quota/limit"
					if plan == ZhipuPlanTeamCodingPlan {
						wantURL += "?type=2"
						require.Equal(t, "org", req.Header.Get("bigmodel-organization"))
						require.Equal(t, "project", req.Header.Get("bigmodel-project"))
					} else {
						require.Empty(t, req.Header.Get("bigmodel-organization"))
						require.Empty(t, req.Header.Get("bigmodel-project"))
					}
					require.Equal(t, wantURL, req.URL.String())
					require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
					auth := req.Header.Get("Authorization")
					require.True(t, strings.HasPrefix(auth, "pat-"))
					require.NotContains(t, auth, "Bearer")
					require.NotContains(t, auth, "forbidden")
					if calls == 1 {
						rejected = auth
						return cnRecoveryResponse(401, `{}`), nil
					}
					require.NotEqual(t, rejected, auth)
					return cnRecoveryResponse(200, `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":30,"nextResetTime":1793491200000}]}}`), nil
				}}
				svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("open.bigmodel.cn", "api.z.ai"))
				svc.projectTokens = manager
				result, err := svc.QueryUsageForAccount(context.Background(), a)
				require.NoError(t, err)
				require.True(t, result.Success)
				require.True(t, result.Persisted)
				require.Equal(t, 2, calls)
				require.Equal(t, 2, client.calls)
				require.NotContains(t, a.Credentials, "api_key")
				require.NotContains(t, a.Credentials, "pat-login")
			})
		}
	}
}

func TestKimiMonthlyThresholdsRetainEachReset(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		total, code float64
		window      string
		until       time.Time
	}{{90, 10, "monthly", now.Add(2 * time.Hour)}, {10, 95, "monthly_code", now.Add(24 * time.Hour)}} {
		t.Run(tc.window, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformKimi)
			a.Extra = cnQuotaExtraUpdates(PlatformKimi, []CNQuotaTier{{Window: "monthly", UsedPercent: tc.total, ResetAt: now.Add(2 * time.Hour).Format(time.RFC3339)}, {Window: "monthly_code", UsedPercent: tc.code, ResetAt: now.Add(24 * time.Hour).Format(time.RFC3339)}}, now)
			bindCNQuotaTestSnapshot(a)
			decision := EvaluateAccountSchedulingThreshold(a, map[string]int{PlatformKimi: 80}, now)
			require.True(t, decision.ShouldPause)
			require.Equal(t, tc.window, decision.Window)
			require.NotNil(t, decision.Until)
			require.True(t, tc.until.Equal(*decision.Until))
		})
	}
}

func TestZhipuQuotaNeverSendsRelayKeyToOfficialEstate(t *testing.T) {
	for _, base := range []string{"https://open.bigmodel.cn.attacker.invalid/api/coding/paas/v4", "https://relay.invalid/path/bigmodel.cn", "https://relay.invalid/?target=api.z.ai", "https://user@api.z.ai/api/coding/paas/v4", "http://open.bigmodel.cn/api/coding/paas/v4", "https://api.z.ai:8443/api/coding/paas/v4"} {
		t.Run(base, func(t *testing.T) {
			a := codingAccount(PlatformZhipu)
			a.Credentials["base_url"] = base
			repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
			calls := 0
			upstream := &cnRecoveryUpstream{send: func(*http.Request) (*http.Response, error) { calls++; return cnRecoveryResponse(200, `{}`), nil }}
			svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("open.bigmodel.cn", "api.z.ai"))
			_, err := svc.QueryUsageForAccount(context.Background(), a)
			require.Error(t, err)
			require.Zero(t, calls)
			require.Empty(t, a.GetCodingPlanProvider())
			require.Empty(t, zhipuQuotaURL(base))
			require.Nil(t, repo.updates)
		})
	}
}

func TestKimiObservedWindowWithoutResetClearsOldReset(t *testing.T) {
	updates := cnQuotaExtraUpdates(PlatformKimi, []CNQuotaTier{{Window: "monthly_code", UsedPercent: 95}}, time.Now())
	reset, exists := updates["kimi_monthly_code_reset_at"]
	require.True(t, exists)
	require.Equal(t, "", reset)
}

func (r *cnQuotaRecoveryRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	return r.UpdateCNProviderQuotaIfUnchanged(ctx, expected, updates)
}

func (r *cnQuotaRecoveryRepo) UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, expected, credentials)
}
