//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A saved API-key owner authorizes only its captured route. Missing or invalid
// bound routes cannot be repaired by a mutable proxy lookup or become direct
// requests, including background balance and coding-plan quota observations.
type cnAPIKeyProxyProbeCase struct {
	name, platform, mode, baseURL, target, authorization, body string
	quota                                                      bool
}

func cnAPIKeyProxyProbeCases() []cnAPIKeyProxyProbeCase {
	return []cnAPIKeyProxyProbeCase{
		{
			name: "deepseek-payg", platform: PlatformDeepseek, mode: AccountModePayG,
			baseURL: "https://api.deepseek.com", target: "https://api.deepseek.com/user/balance",
			authorization: "Bearer test-only-owner-api-key",
			body:          `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.5"}]}`,
		},
		{
			name: "kimi-payg", platform: PlatformKimi, mode: AccountModePayG,
			baseURL: "https://api.moonshot.cn/v1", target: "https://api.moonshot.cn/v1/users/me/balance",
			authorization: "Bearer test-only-owner-api-key",
			body:          `{"code":0,"status":true,"data":{"available_balance":12.5}}`,
		},
		{
			name: "kimi-coding", platform: PlatformKimi, mode: AccountModeCoding, quota: true,
			baseURL: "https://api.kimi.com/coding/v1", target: "https://api.kimi.com/coding/v1/usages",
			authorization: "Bearer test-only-owner-api-key",
			body:          `{"limits":[{"detail":{"limit":100,"remaining":75}}],"usage":{"limit":1000,"remaining":800}}`,
		},
		{
			name: "zhipu-coding", platform: PlatformZhipu, mode: AccountModeCoding, quota: true,
			baseURL: "https://open.bigmodel.cn/api/coding/paas/v4", target: "https://open.bigmodel.cn/api/monitor/usage/quota/limit",
			authorization: "test-only-owner-api-key",
			body:          `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}],"level":"max"}}`,
		},
	}
}

func (tc cnAPIKeyProxyProbeCase) account() *Account {
	proxyID := int64(71)
	return &Account{
		ID: 874, Platform: tc.platform, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 3,
		Credentials: map[string]any{
			"account_mode": tc.mode, "api_key": "test-only-owner-api-key", "base_url": tc.baseURL,
			"owner_settings": map[string]any{"owner": "captured-api-key-owner"},
		},
		Extra: map[string]any{
			"business_marker":      map[string]any{"owner": "existing-observation"},
			"kimi_5h_used_percent": float64(37), "deepseek_balance": float64(29),
		},
		ProxyID: &proxyID,
		Proxy: &Proxy{
			ID: proxyID, Protocol: "http", Host: "captured.proxy.example", Port: 8080,
			Username: "captured-user", Password: "captured-password", Status: StatusActive,
		},
	}
}

type cnAPIKeyProxyProbeAccountRepo struct {
	AccountRepository
	account          *Account
	extraWrites      int
	credentialWrites int
}

func (r *cnAPIKeyProxyProbeAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *cnAPIKeyProxyProbeAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.extraWrites++
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return nil
}

func (r *cnAPIKeyProxyProbeAccountRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	r.credentialWrites++
	r.account.Credentials = credentials
	return nil
}

type cnAPIKeyProxyProbeProxyRepo struct {
	ProxyRepository
	row   *Proxy
	calls int
}

func (r *cnAPIKeyProxyProbeProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.row, nil
}

type cnAPIKeyProxyProbeUpstream struct {
	body        string
	calls       int
	request     *http.Request
	proxyURL    string
	accountID   int64
	concurrency int
}

func (u *cnAPIKeyProxyProbeUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	u.calls++
	u.request, u.proxyURL, u.accountID, u.concurrency = req, proxyURL, accountID, concurrency
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func (u *cnAPIKeyProxyProbeUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func (tc cnAPIKeyProxyProbeCase) query(account *Account, repo AccountRepository, proxies ProxyRepository, upstream HTTPUpstream) (any, error) {
	cfg := cnProbeAllowlistConfig("api.deepseek.com", "api.moonshot.cn", "api.kimi.com", "open.bigmodel.cn")
	if tc.quota {
		return NewCNProviderQuotaService(repo, proxies, upstream, cfg).QueryUsageForAccount(context.Background(), account)
	}
	return NewCNProviderBalanceService(repo, proxies, upstream, cfg).QueryBalanceForAccount(context.Background(), account)
}

func TestCNAPIKeyControlRejectsBrokenProxyBeforeHTTPOrStateWrites(t *testing.T) {
	for _, tc := range cnAPIKeyProxyProbeCases() {
		for _, failure := range []string{
			"missing-bound-snapshot", "missing-bound-snapshot-and-row", "mismatched-bound-owner",
			"malformed-bound-host", "username-without-password", "password-without-username",
		} {
			t.Run(tc.name+"/"+failure, func(t *testing.T) {
				account := tc.account()
				repo := &cnAPIKeyProxyProbeAccountRepo{account: account}
				proxies := &cnAPIKeyProxyProbeProxyRepo{row: &Proxy{
					ID: 71, Protocol: "http", Host: "replacement.proxy.example", Port: 8888,
				}}
				switch failure {
				case "missing-bound-snapshot":
					account.Proxy = nil
				case "missing-bound-snapshot-and-row":
					account.Proxy, proxies.row = nil, nil
				case "mismatched-bound-owner":
					account.Proxy.ID = 72
				case "malformed-bound-host":
					account.Proxy.Host = "captured.proxy.example/foreign-route"
				case "username-without-password":
					account.Proxy.Password = ""
				case "password-without-username":
					account.Proxy.Username = ""
				}
				credentialsBefore, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				extraBefore, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				upstream := &cnAPIKeyProxyProbeUpstream{body: tc.body}
				result, err := tc.query(account, repo, proxies, upstream)
				assert.Error(t, err, "the saved binding must fail before the provider can observe this API key")
				assert.Nil(t, result)
				assert.Zero(t, upstream.calls, "neither the captured malformed route nor direct egress is authorized")
				assert.Zero(t, proxies.calls, "an in-flight owner must not adopt a replacement route from a mutable lookup")
				assert.Zero(t, repo.extraWrites, "a rejected probe cannot overwrite existing balance or quota observations")
				assert.Zero(t, repo.credentialWrites)
				if err != nil {
					assert.NotContains(t, err.Error(), "captured-password")
					assert.NotContains(t, err.Error(), "test-only-owner-api-key")
				}
				credentialsAfter, marshalErr := json.Marshal(account.Credentials)
				require.NoError(t, marshalErr)
				extraAfter, marshalErr := json.Marshal(account.Extra)
				require.NoError(t, marshalErr)
				assert.Equal(t, credentialsBefore, credentialsAfter)
				assert.Equal(t, extraBefore, extraAfter)
			})
		}
	}
}

func TestCNAPIKeyControlUsesCapturedBoundRouteOrSavedUnbinding(t *testing.T) {
	for _, tc := range cnAPIKeyProxyProbeCases() {
		for _, route := range []string{"captured-bound-route", "saved-unbinding"} {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				account := tc.account()
				expectedRoute := "http://captured-user:captured-password@captured.proxy.example:8080"
				if route == "saved-unbinding" {
					account.ProxyID = nil // A stale association cannot undo the saved unbinding.
					expectedRoute = ""
				}
				credentialsBefore, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				repo := &cnAPIKeyProxyProbeAccountRepo{account: account}
				proxies := &cnAPIKeyProxyProbeProxyRepo{row: &Proxy{
					ID: 71, Protocol: "http", Host: "replacement.proxy.example", Port: 8888,
				}}
				upstream := &cnAPIKeyProxyProbeUpstream{body: tc.body}
				result, err := tc.query(account, repo, proxies, upstream)
				require.NoError(t, err)
				require.NotNil(t, result)
				switch result := result.(type) {
				case *CNProviderBalanceResult:
					require.True(t, result.Success)
					require.True(t, result.Persisted)
					require.Equal(t, float64(12.5), result.Balance)
				case *CNProviderQuotaProbeResult:
					require.True(t, result.Success)
					require.True(t, result.CredentialValid)
					require.True(t, result.Persisted)
					require.NotEmpty(t, result.Tiers)
				default:
					t.Fatalf("unexpected probe result %T", result)
				}
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, expectedRoute, upstream.proxyURL)
				require.Equal(t, tc.target, upstream.request.URL.String())
				require.Equal(t, http.MethodGet, upstream.request.Method)
				require.Equal(t, tc.authorization, upstream.request.Header.Get("Authorization"))
				require.Equal(t, int64(874), upstream.accountID)
				require.Equal(t, 3, upstream.concurrency)
				require.Zero(t, proxies.calls)
				require.Equal(t, 1, repo.extraWrites)
				require.Zero(t, repo.credentialWrites)
				require.Equal(t, "existing-observation", account.Extra["business_marker"].(map[string]any)["owner"])
				credentialsAfter, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				require.Equal(t, credentialsBefore, credentialsAfter)
			})
		}
	}
}
