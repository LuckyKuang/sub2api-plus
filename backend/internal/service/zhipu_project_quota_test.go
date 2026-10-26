//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

const zhipuProjectQuotaBody = `{"code":200,"success":true,"data":{"level":"MAX","limits":[{"type":"TOKENS_LIMIT","unit":6,"percentage":77,"nextResetTime":1893456000000},{"type":"TOKENS_LIMIT","unit":3,"percentage":25,"nextResetTime":1893542400000}]}}`

type zhipuProjectQuotaRepo struct {
	AccountRepository
	account *Account
	updates map[string]any
}

func (r *zhipuProjectQuotaRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *zhipuProjectQuotaRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

func (r *zhipuProjectQuotaRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	if r.account == nil || expected == nil {
		return false, nil
	}
	expectedCredentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	currentCredentials, err := json.Marshal(r.account.Credentials)
	if err != nil {
		return false, err
	}
	if expected.ID != r.account.ID || expected.Platform != r.account.Platform || expected.Type != r.account.Type || string(expectedCredentials) != string(currentCredentials) || !reflect.DeepEqual(expected.ProxyID, r.account.ProxyID) {
		return false, nil
	}
	r.updates = updates
	return true, nil
}

type zhipuProjectQuotaUpstream struct {
	request   *http.Request
	proxy     string
	calls     int
	status    int
	body      string
	onRequest func()
}

func (u *zhipuProjectQuotaUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.request, u.proxy = req, proxy
	u.calls++
	if u.onRequest != nil {
		u.onRequest()
	}
	return &http.Response{StatusCode: u.status, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func (u *zhipuProjectQuotaUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

type zhipuProjectQuotaFixture struct {
	service  *CNProviderQuotaService
	repo     *zhipuProjectQuotaRepo
	upstream *zhipuProjectQuotaUpstream
	issuer   *projectIssuerStub
	tokens   *ZhipuProjectTokenManager
}

func newZhipuProjectQuotaFixture() *zhipuProjectQuotaFixture {
	a := projectTokenAccount()
	a.Credentials["account_mode"] = AccountModeCoding
	a.Credentials["zhipu_project_kind"] = "personal"
	// OAuth control endpoints belong to the credential issuer, even when a
	// legacy account still contains unrelated gateway/secret metadata.
	a.Credentials["base_url"] = "https://relay.attacker.example/coding"
	a.Credentials["api_key"] = "legacy-copied-secret"
	a.Credentials["header_override_enabled"] = true
	a.Credentials["header_overrides"] = map[string]any{
		"Authorization": "Bearer untrusted", "User-Agent": "untrusted-client",
		"bigmodel-organization": "wrong-org", "bigmodel-project": "wrong-project",
	}
	proxyID := int64(8)
	a.ProxyID = &proxyID
	a.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080}
	repo := &zhipuProjectQuotaRepo{account: a}
	upstream := &zhipuProjectQuotaUpstream{status: http.StatusOK, body: zhipuProjectQuotaBody}
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: "short-lived-project-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	fixture := &zhipuProjectQuotaFixture{
		service: NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("open.bigmodel.cn", "api.z.ai")),
		repo:    repo, upstream: upstream, issuer: issuer,
		tokens: NewZhipuProjectTokenManager(issuer, repo),
	}
	fixture.service.projectTokens = fixture.tokens
	return fixture
}

func TestZhipuProjectQuotaOfficialPersonalAndTeam(t *testing.T) {
	for _, tc := range []struct {
		name, provider, plan, kind, target string
		team                               bool
	}{
		{"bigmodel-personal-with-project-metadata", "bigmodel", ZhipuPlanIndividualCodingPlan, "personal", "https://open.bigmodel.cn/api/monitor/usage/quota/limit", false},
		{"zai-personal", "zai", ZhipuPlanIndividualCodingPlan, "personal", "https://api.z.ai/api/monitor/usage/quota/limit", false},
		{"bigmodel-team", "bigmodel", ZhipuPlanTeamCodingPlan, "team", "https://open.bigmodel.cn/api/monitor/usage/quota/limit?type=2", true},
		{"zai-team", "zai", ZhipuPlanTeamCodingPlan, "team", "https://api.z.ai/api/monitor/usage/quota/limit?type=2", true},
		{"offpeak-personal", "bigmodel", ZhipuPlanOffPeak, "personal", "https://open.bigmodel.cn/api/monitor/usage/quota/limit", false},
		{"offpeak-team", "bigmodel", ZhipuPlanOffPeak, "team", "https://open.bigmodel.cn/api/monitor/usage/quota/limit?type=2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			a := f.repo.account
			a.Credentials["oauth_provider"], a.Credentials["plan_kind"], a.Credentials["zhipu_project_kind"] = tc.provider, tc.plan, tc.kind
			result, err := f.service.QueryUsage(context.Background(), a.ID)
			require.NoError(t, err, "linked OAuth quota must use a short-lived project token")
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.True(t, result.CredentialValid)
			require.Equal(t, "MAX", result.PlanLevel)
			require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 25, ResetAt: "2030-01-02T00:00:00Z"}, {Window: "weekly", UsedPercent: 77, ResetAt: "2030-01-01T00:00:00Z"}}, result.Tiers, "explicit units beat reset-time sorting")
			require.Equal(t, tc.target, f.upstream.request.URL.String())
			require.Equal(t, "short-lived-project-token", f.upstream.request.Header.Get("Authorization"), "monitor authorization never adds Bearer or uses a Key ID/copied secret")
			require.Equal(t, "http://proxy.example:8080", f.upstream.proxy)
			require.True(t, HTTPUpstreamRedirectsDisabled(f.upstream.request.Context()))
			require.Equal(t, "ZCode/3.16.2", f.upstream.request.Header.Get("User-Agent"))
			require.Equal(t, "3.16.2", f.upstream.request.Header.Get("X-ZCode-App-Version"))
			require.Empty(t, f.upstream.request.Header.Get("X-ZCode-Agent"), "inference-only identity declaration is omitted by monitor")
			if tc.team {
				require.Equal(t, "org", f.upstream.request.Header.Get("bigmodel-organization"))
				require.Equal(t, "project", f.upstream.request.Header.Get("bigmodel-project"))
			} else {
				require.Empty(t, f.upstream.request.Header.Get("bigmodel-organization"))
				require.Empty(t, f.upstream.request.Header.Get("bigmodel-project"))
			}
			require.EqualValues(t, 1, f.issuer.calls.Load())
			require.Equal(t, float64(25), f.repo.updates["zhipu_5h_used_percent"])
			require.Equal(t, float64(77), f.repo.updates["zhipu_weekly_used_percent"])
		})
	}
}

func TestZhipuProjectQuotaRejectsUnknownOrMalformedObservations(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"invalid-json", `{"data":`, 200},
		{"business-failure", `{"code":1001,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":0}]}}`, 200},
		{"explicit-failure", `{"code":200,"success":false,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":0}]}}`, 200},
		{"no-data", `{"code":200}`, 200},
		{"unknown-shape", `{"code":200,"data":{"unrecognized":true}}`, 200},
		{"missing-percentage", `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3}]}}`, 200},
		{"string-percentage", `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":"0"}]}}`, 200},
		{"negative-percentage", `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":-1}]}}`, 200},
		{"unknown-window-unit", `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":9,"percentage":0}]}}`, 200},
		{"malformed-sibling", `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20},{"type":"TOKENS_LIMIT","unit":6}]}}`, 200},
		{"unauthorized", zhipuProjectQuotaBody, 401},
		{"forbidden", zhipuProjectQuotaBody, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			f.upstream.status, f.upstream.body = tc.status, tc.body
			result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.NotEmpty(t, result.Error)
			require.Nil(t, f.repo.updates, "unknown usage must preserve the previous valid snapshot")
			wantCalls := 1
			if tc.status == http.StatusUnauthorized {
				wantCalls = 2
			}
			require.Equal(t, wantCalls, f.upstream.calls, "only an explicit 401 permits one retry with a newly issued PAT")
			if tc.status == 401 || tc.status == 403 {
				require.False(t, result.CredentialValid)
			}
		})
	}
}

func TestZhipuProjectQuotaRejectedPATRequiresFreshIssuance(t *testing.T) {
	for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanOffPeak} {
		t.Run(plan, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			f.repo.account.Credentials["plan_kind"] = plan
			f.upstream.status = http.StatusUnauthorized
			first, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.False(t, first.CredentialValid)
			require.Equal(t, 2, f.upstream.calls, "a persistent 401 ends after one same-owner replay")
			require.EqualValues(t, 2, f.issuer.calls.Load())
			f.issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
				return &zcode.ProjectAccessToken{Token: "replacement-project-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			f.upstream.status = http.StatusOK
			second, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.True(t, second.Success)
			require.EqualValues(t, 3, f.issuer.calls.Load(), "a provider-rejected PAT cannot remain cached, including the final replay")
			require.Equal(t, 3, f.upstream.calls)
			require.Equal(t, "replacement-project-token", f.upstream.request.Header.Get("Authorization"))
		})
	}
}

func TestZhipuProjectQuotaRecoversExplicit401Once(t *testing.T) {
	for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanOffPeak} {
		t.Run(plan, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			f.repo.account.Credentials["plan_kind"] = plan
			f.issuer.issue = func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
				require.Equal(t, "owner-login", login)
				return &zcode.ProjectAccessToken{Token: fmt.Sprintf("quota-pat-%d", f.issuer.calls.Load()), ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			var sent []string
			f.upstream.onRequest = func() {
				sent = append(sent, f.upstream.request.Header.Get("Authorization"))
				require.Equal(t, "http://proxy.example:8080", f.upstream.proxy)
				if f.upstream.calls == 1 {
					f.upstream.status, f.upstream.body = http.StatusUnauthorized, `{"error":"expired project access token"}`
				} else {
					f.upstream.status, f.upstream.body = http.StatusOK, zhipuProjectQuotaBody
				}
			}
			result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.True(t, result.CredentialValid)
			require.Equal(t, []string{"quota-pat-1", "quota-pat-2"}, sent, "monitor uses the newly issued raw PAT without a Bearer prefix")
			require.EqualValues(t, 2, f.issuer.calls.Load())
			require.Equal(t, 2, f.upstream.calls)
		})
	}
}

func TestZhipuProjectQuotaForbiddenDoesNotReplayOrEvict(t *testing.T) {
	for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanOffPeak} {
		t.Run(plan, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			f.repo.account.Credentials["plan_kind"] = plan
			f.upstream.status = http.StatusForbidden
			result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.False(t, result.CredentialValid)
			require.Nil(t, f.repo.updates)
			require.Equal(t, 1, f.upstream.calls, "a permission denial does not authorize immediate token rotation or replay")
			require.EqualValues(t, 1, f.issuer.calls.Load())
			f.upstream.status = http.StatusOK
			result, err = f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Equal(t, 2, f.upstream.calls)
			require.EqualValues(t, 1, f.issuer.calls.Load(), "a monitor permission denial does not reject its PAT")
			require.Equal(t, "short-lived-project-token", f.upstream.request.Header.Get("Authorization"))
		})
	}
}

func TestZhipuProjectQuotaReusesAccountIdentityAcrossIssuanceAndMonitor(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	f.repo.account.Credentials[outboundIdentityCredential] = map[string]any{"preset": "zcode", "version": "0.16.9"}
	f.issuer.issue = func(ctx context.Context, _ string) (*zcode.ProjectAccessToken, error) {
		identity, ok := outboundidentity.FromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "account", identity.Source)
		require.Equal(t, "0.16.9", identity.Version)
		require.Equal(t, f.repo.account.ID, identity.AccountID)
		return &zcode.ProjectAccessToken{Token: "selected-identity-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "ZCode/0.16.9", f.upstream.request.Header.Get("User-Agent"))
	require.Equal(t, "0.16.9", f.upstream.request.Header.Get("X-ZCode-App-Version"))
	identity, ok := outboundidentity.FromContext(f.upstream.request.Context())
	require.True(t, ok)
	require.Equal(t, "account", identity.Source)
	require.Equal(t, f.repo.account.ID, identity.AccountID)
}

func TestZhipuProjectQuotaDoesNotCrossChangedOwner(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	f.upstream.onRequest = func() { f.repo.account.Credentials["access_token"] = "replacement-owner" }
	result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
	require.NoError(t, err)
	require.False(t, result.Success, "old-owner quota cannot be returned as a usable scheduling observation")
	require.False(t, result.Persisted, "old-owner quota cannot overwrite the relinked account")
	require.NotEmpty(t, result.Error)
	require.Nil(t, f.repo.updates)
}

func TestZhipuProjectQuotaPartialSnapshotClearsOldWindows(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	f.upstream.body = `{"code":0,"data":{"limits":[{"type":"CREDIT_LIMIT","unit":3,"percentage":0}]}}`
	result, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, float64(0), f.repo.updates["zhipu_5h_used_percent"], "an explicit zero remains a valid observation")
	for _, key := range []string{"zhipu_5h_reset_at", "zhipu_weekly_used_percent", "zhipu_weekly_reset_at", "zhipu_monthly_used_percent", "zhipu_monthly_reset_at"} {
		value, exists := f.repo.updates[key]
		require.True(t, exists, "clear stale %s rather than marking it freshly observed", key)
		require.Nil(t, value)
	}
}

func TestZhipuProjectQuotaRejectsPolicyOrUnprovenOffPeakScopeBeforeIssuance(t *testing.T) {
	for _, tc := range []string{"url-policy", "legacy-offpeak-without-explicit-scope"} {
		t.Run(tc, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			if tc == "url-policy" {
				f.service.cfg = cnProbeAllowlistConfig("other.example")
			} else {
				f.repo.account.Credentials["plan_kind"] = ZhipuPlanOffPeak
				delete(f.repo.account.Credentials, "zhipu_project_kind")
			}
			_, err := f.service.QueryUsage(context.Background(), f.repo.account.ID)
			require.Error(t, err)
			require.Zero(t, f.issuer.calls.Load())
			require.Zero(t, f.upstream.calls)
			require.Nil(t, f.repo.updates)
		})
	}
}
