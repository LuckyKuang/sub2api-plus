//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// Official Step schema v1: a coding plan always has both windows. Unlimited is
// an explicit null observation, never a zero-percent limited window.
const stepCodingUsageFixture = `{"schema_version":1,"scope":"personal_account","observed_at_ms":1792900000000,"account":{"id":"42"},"plan":{"state":"known","family":"coding","name":"Plan","subscription":null},"quota":{"state":"known","allowed":true,"reason":null,"windows":[{"id":"five_hour","limited":true,"used_percent":25,"resets_at_ms":1793000000000},{"id":"weekly","limited":false,"used_percent":null,"resets_at_ms":null}],"credit_buckets":[]}}`

type nativeControlOwnerUpstream struct {
	HTTPUpstream
	respond func(*http.Request) (*http.Response, error)
}

func (u nativeControlOwnerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.respond(req)
}

func (u nativeControlOwnerUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.respond(req)
}
func nativeControlReply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}
func nativeControlAccount(platform string) *Account {
	return &Account{ID: 74, Platform: platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: "cn"}, &cnoauth.Grant{AccessToken: "request-owner", RefreshToken: "request-refresh", ExpiresAt: time.Now().Add(time.Hour)})}
}

func sameTestCNProxySnapshot(current, expected *Proxy) bool {
	if current == nil || expected == nil {
		return current == nil && expected == nil
	}
	if current.ID != expected.ID || current.Protocol != expected.Protocol || current.Host != expected.Host || current.Port != expected.Port || current.Username != expected.Username || current.Password != expected.Password || current.Status != expected.Status {
		return false
	}
	if current.ExpiresAt == nil || expected.ExpiresAt == nil {
		return current.ExpiresAt == nil && expected.ExpiresAt == nil
	}
	return current.ExpiresAt.Equal(*expected.ExpiresAt)
}

// The fake owns a complete independent comparison, matching the production
// atomic repository contract rather than accepting an unconditional write.
func (r *kimiQuotaTestRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	current := r.account
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) || !sameTestCNProxySnapshot(current.Proxy, expected.Proxy) {
		return false, nil
	}
	currentRoute, currentRouteErr := current.StrictProxyURL()
	expectedRoute, expectedRouteErr := expected.StrictProxyURL()
	if currentRouteErr != nil || expectedRouteErr != nil || currentRoute != expectedRoute {
		return false, nil
	}
	if expected.ProxyID != nil && (current.Proxy.Status != expected.Proxy.Status || !reflect.DeepEqual(current.Proxy.ExpiresAt, expected.Proxy.ExpiresAt)) {
		return false, nil
	}
	currentJSON, currentErr := json.Marshal(current.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected.Credentials)
	if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
		return false, nil
	}
	r.updates = updates
	return true, nil
}

type nativeModelMetadataRepo struct {
	upstreamModelMetadataRepoStub
	owner *Account
}

func (r *nativeModelMetadataRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	current := r.owner
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) || !sameTestCNProxySnapshot(current.Proxy, expected.Proxy) {
		return false, nil
	}
	currentJSON, currentErr := json.Marshal(current.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected.Credentials)
	if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
		return false, nil
	}
	if err := r.UpdateExtra(ctx, expected.ID, updates); err != nil {
		return false, err
	}
	return true, nil
}

func TestNativeOAuthUsageRejectsLateOwnerResponse(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "in-place"}[inPlace], func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			changed := snapshotOAuthRefreshAccount(a)
			changed.Credentials["access_token"] = "new-login"
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
				if inPlace {
					a.Credentials["access_token"] = "new-login"
				} else {
					repo.account = changed
				}
				return nativeControlReply(`{"usages":{"limit_5h":{"used_ratio":0.25}}}`), nil
			}}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsage(context.Background(), a.ID)
			require.NoError(t, err)
			require.False(t, result.Success, "response belongs to the login that issued the request")
			require.False(t, result.Persisted)
			require.Nil(t, repo.updates, "relink must not receive the previous login's usage snapshot")
		})
	}
}

func TestNativeOAuthWalletRejectsLateOwnerResponse(t *testing.T) {
	a := nativeControlAccount(PlatformDeepseek)
	repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
	changed := snapshotOAuthRefreshAccount(a)
	changed.Credentials["access_token"] = "new-login"
	upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
		repo.account = changed
		return nativeControlReply(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"25"}],"bonus_wallets":[]}}}`), nil
	}}
	result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(context.Background(), a.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, result.Persisted)
	require.Nil(t, repo.updates)
}

func TestNativeQuotaMissingWindowsCannotRefreshOldObservation(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformStepFun} {
		t.Run(platform, func(t *testing.T) {
			a := nativeControlAccount(platform)
			a.Extra = map[string]any{platform + "_weekly_used_percent": 99.0, platform + "_weekly_reset_at": "2026-10-26T00:00:00Z"}
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			body := `{"usages":{"limit_5h":{"used_ratio":0.25},"limit_7d":{"used_ratio":"unknown"}}}`
			if platform == PlatformStepFun {
				body = stepCodingUsageFixture
			}
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) { return nativeControlReply(body), nil }}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsage(context.Background(), a.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Equal(t, 25.0, repo.updates[platform+"_5h_used_percent"])
			require.Contains(t, repo.updates, platform+"_weekly_used_percent")
			require.Nil(t, repo.updates[platform+"_weekly_used_percent"], "missing or unlimited is unknown")
			require.Contains(t, repo.updates, platform+"_weekly_reset_at")
			require.Nil(t, repo.updates[platform+"_weekly_reset_at"], "old reset time must not survive as fresh")
		})
	}
}

func TestStepExplicitlyUnlimitedSnapshotClearsAllPreviousWindows(t *testing.T) {
	a := nativeControlAccount(PlatformStepFun)
	a.Extra = map[string]any{
		"stepfun_5h_used_percent": 99.0, "stepfun_5h_reset_at": "2026-10-26T00:00:00Z",
		"stepfun_weekly_used_percent": 99.0, "stepfun_weekly_reset_at": "2026-10-26T00:00:00Z",
		"stepfun_usage_updated_at": "2026-10-24T00:00:00Z",
	}
	repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
	body := `{"schema_version":1,"scope":"personal_account","observed_at_ms":1792900000000,"account":{"id":"42"},"plan":{"state":"known","family":"coding","name":"Plan","subscription":null},"quota":{"state":"known","allowed":true,"reason":null,"windows":[{"id":"five_hour","limited":false,"used_percent":null,"resets_at_ms":null},{"id":"weekly","limited":false,"used_percent":null,"resets_at_ms":null}],"credit_buckets":[]}}`
	upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) { return nativeControlReply(body), nil }}
	result, err := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsage(context.Background(), a.ID)
	require.NoError(t, err)
	require.True(t, result.Success, "both explicitly unlimited windows are a valid observed coding snapshot")
	require.True(t, result.Persisted)
	require.Empty(t, result.Tiers, "unlimited must never be fabricated as zero-percent limited quota")
	for _, key := range []string{"stepfun_5h_used_percent", "stepfun_5h_reset_at", "stepfun_weekly_used_percent", "stepfun_weekly_reset_at"} {
		require.Contains(t, repo.updates, key)
		require.Nil(t, repo.updates[key], "previous limited observation must not survive an explicit unlimited observation")
	}
	require.NotEmpty(t, repo.updates["stepfun_usage_updated_at"])
	require.NotEqual(t, a.Extra["stepfun_usage_updated_at"], repo.updates["stepfun_usage_updated_at"])
}

func TestStepQuotaRejectsMalformedCompleteSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing account", func(v map[string]any) { delete(v, "account") }},
		{"missing observation time", func(v map[string]any) { delete(v, "observed_at_ms") }},
		{"string version", func(v map[string]any) { v["schema_version"] = "1" }},
		{"missing allowed", func(v map[string]any) { delete(v["quota"].(map[string]any), "allowed") }},
		{"missing weekly", func(v map[string]any) { q := v["quota"].(map[string]any); q["windows"] = q["windows"].([]any)[:1] }},
		{"malformed sibling percent", func(v map[string]any) {
			q := v["quota"].(map[string]any)
			w := q["windows"].([]any)[1].(map[string]any)
			w["limited"] = true
			w["used_percent"] = "unknown"
		}},
		{"future observation", func(v map[string]any) { v["observed_at_ms"] = float64(time.Now().Add(time.Hour).UnixMilli()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(stepCodingUsageFixture), &payload))
			tc.mutate(payload)
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			tiers, _, valid := parseStepUsageTiers(body)
			require.False(t, valid, "malformed snapshot cannot be a known unlimited observation")
			require.Empty(t, tiers, "malformed snapshots cannot be advertised as valid available quota")
		})
	}
}

func TestDeepSeekBonusBalanceOnlyCombinesWithinCurrencyForThreshold(t *testing.T) {
	for _, tc := range []struct {
		name          string
		normal, bonus []CNProviderBalanceEntry
		below         bool
	}{
		{"bonus alone sufficient", []CNProviderBalanceEntry{{Currency: "CNY", Balance: 0}}, []CNProviderBalanceEntry{{Currency: "CNY", Balance: 8}}, false},
		{"same currency combines", []CNProviderBalanceEntry{{Currency: "CNY", Balance: 3}}, []CNProviderBalanceEntry{{Currency: "CNY", Balance: 3}}, false},
		{"different currency never combines", []CNProviderBalanceEntry{{Currency: "CNY", Balance: 3}}, []CNProviderBalanceEntry{{Currency: "USD", Balance: 3}}, true},
		{"no bonus retains ordinary behavior", []CNProviderBalanceEntry{{Currency: "CNY", Balance: 3}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := &CNProviderBalanceResult{Balances: tc.normal, BonusBalances: tc.bonus}
			require.Equal(t, tc.below, allCNBalancesBelowThreshold(result, 5))
		})
	}
}

func TestStepPlanOAuthPeriodicQuotaEligibility(t *testing.T) {
	oauth := Account{ID: 71, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"account_mode": "coding", "oauth_provider": "stepfun", "oauth_region": "cn"}}
	apikey := oauth
	apikey.ID = 72
	apikey.Type = AccountTypeAPIKey
	mini := oauth
	mini.ID = 73
	mini.Platform = PlatformMiniMax
	mini.Credentials = map[string]any{"account_mode": "coding", "oauth_provider": "minimax", "oauth_region": "cn"}
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{PlatformStepFun: {oauth, apikey}, PlatformMiniMax: {mini}}}
	prober := &fakeCNQuotaProber{}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, quotaService: prober, cfg: &config.Config{}}
	svc.runOnce()
	require.Equal(t, []int64{71}, prober.Probed(), "only the official Step Plan OAuth account has this usage contract")
}

func TestNativeOAuthQuotaSingleflightDoesNotJoinDifferentLogins(t *testing.T) {
	first := nativeControlAccount(PlatformKimi)
	second := snapshotOAuthRefreshAccount(first)
	second.Credentials["access_token"] = "second-login"
	repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: first}}
	firstEntered, secondEntered, releaseFirst := make(chan struct{}), make(chan struct{}), make(chan struct{})
	upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") == "Bearer request-owner" {
			close(firstEntered)
			<-releaseFirst
		} else {
			close(secondEntered)
		}
		return nativeControlReply(`{"usages":{"limit_5h":{"used_ratio":0.25}}}`), nil
	}}
	svc := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{})
	done := make(chan struct{}, 2)
	go func() { _, _ = svc.QueryUsageForAccount(context.Background(), first); done <- struct{}{} }()
	<-firstEntered
	repo.account = second
	go func() { _, _ = svc.QueryUsageForAccount(context.Background(), second); done <- struct{}{} }()
	separate := false
	select {
	case <-secondEntered:
		separate = true
	case <-time.After(2 * time.Second):
	}
	close(releaseFirst)
	<-done
	<-done
	require.True(t, separate, "different login generations must issue independent requests even under the same account ID")
}

func TestNativeQuotaNonFiniteRatiosRemainUnknown(t *testing.T) {
	for _, ratio := range []string{"NaN", "Infinity", "-Infinity"} {
		t.Run(ratio, func(t *testing.T) {
			tiers := parseKimiUsageTiers([]byte(`{"usages":{"limit_5h":{"used_ratio":"` + ratio + `"}}}`))
			require.Empty(t, tiers, "non-finite upstream ratios cannot become quota observations")
		})
	}
}

func TestNativeDeepSeekWalletRejectsNonDecimalBalance(t *testing.T) {
	for _, value := range []string{"NaN", "Infinity", "+1", " 2 "} {
		t.Run(value, func(t *testing.T) {
			a := nativeControlAccount(PlatformDeepseek)
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			body := `{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"` + value + `"}],"bonus_wallets":[]}}}`
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) { return nativeControlReply(body), nil }}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(context.Background(), a.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.Nil(t, repo.updates)
		})
	}
}

func TestNativeOAuthRefreshCannotSwitchRequestLogin(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, replacedLogin := range []bool{false, true} {
			t.Run(method+map[bool]string{false: "/same-login-rotation", true: "/replacement-login"}[replacedLogin], func(t *testing.T) {
				original := nativeControlAccount(PlatformKimi)
				original.Status = StatusActive
				original.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
				changed := snapshotOAuthRefreshAccount(original)
				changed.Credentials["access_token"] = "another-access"
				changed.Credentials["refresh_token"] = "another-refresh"
				changed.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
				if replacedLogin {
					changed.Credentials[domesticOAuthLoginIDCredential] = "replacement-login-epoch"
				}
				repo := &refreshAPIAccountRepo{account: changed}
				svc := NewCNOAuthService(nil, repo, nil)
				svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
				path := "/coding/v1/usages"
				if method == http.MethodPost {
					path = "/coding/v1/messages"
				}
				req, err := http.NewRequest(method, "https://api.kimi.com"+path, nil)
				require.NoError(t, err)
				err = svc.prepareRequest(req, original)
				if replacedLogin {
					require.Error(t, err, "refresh reread cannot silently switch the request to another login")
					require.Empty(t, req.Header.Get("Authorization"))
				} else {
					require.NoError(t, err, "the same login's concurrent rotation may supply the fresh token")
					require.Equal(t, "Bearer another-access", req.Header.Get("Authorization"))
				}
				require.Zero(t, repo.updateCredentialsCalls, "neither reuse nor rejection rewrites credentials")
			})
		}
	}
}

func TestNativeKimiUsageRejectsForeignOrOverflowObservation(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"foreign schema", `{"limits":[{"detail":{}}]}`},
		{"overflow percentage", `{"usages":{"limit_5h":{"used_ratio":1e308}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) { return nativeControlReply(tc.body), nil }}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsage(context.Background(), a.ID)
			require.NoError(t, err)
			require.False(t, result.Success, "foreign schemas and overflowing percentages are unknown")
			require.False(t, result.Persisted)
			require.Nil(t, repo.updates)
		})
	}
}

func TestNativeDeepSeekWalletAvailabilityUsesSameCurrencyNetBalance(t *testing.T) {
	for _, tc := range []struct {
		name, normal, bonus string
		available           bool
	}{
		{"bonus repays debt", "-10", "2", false},
		{"zero net balance", "-2", "2", false},
		{"positive net balance", "-2", "3", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := nativeControlAccount(PlatformDeepseek)
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
			body := `{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"` + tc.normal + `"}],"bonus_wallets":[{"currency":"CNY","balance":"` + tc.bonus + `"}]}}}`
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) { return nativeControlReply(body), nil }}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(context.Background(), a.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, tc.available, result.Available)
		})
	}
}

func (r *refreshAPIAccountRepo) UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, expected, credentials)
}

type nativeRefreshWithoutCASRepo struct {
	AccountRepository
	inner *refreshAPIAccountRepo
}

func (r *nativeRefreshWithoutCASRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.inner.GetByID(ctx, id)
}
func (r *nativeRefreshWithoutCASRepo) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	return r.inner.UpdateCredentials(ctx, id, credentials)
}

func TestNativeOAuthRefreshRejectsLateIssuerResponseAfterRelink(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "in-place"}[inPlace], func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: a}
			svc := NewCNOAuthService(nil, repo, nil)
			svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				if inPlace {
					a.Credentials["access_token"] = "relinked-login"
					a.Credentials["refresh_token"] = "relinked-refresh"
				} else {
					changed := snapshotOAuthRefreshAccount(a)
					changed.Credentials["access_token"] = "relinked-login"
					changed.Credentials["refresh_token"] = "relinked-refresh"
					repo.account = changed
				}
				return nativeControlReply(`{"access_token":"late-old-grant","refresh_token":"late-old-refresh","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
			})}
			fresh, err := svc.RefreshAccount(withOAuthRefreshRequestPath(context.Background()), a, true)
			require.Error(t, err, "a provider-issued rotation cannot overwrite a relink committed during refresh")
			require.Nil(t, fresh)
			require.Equal(t, "relinked-login", repo.account.GetCredential("access_token"))
			require.Equal(t, "relinked-refresh", repo.account.GetCredential("refresh_token"))
			require.Zero(t, repo.updateCredentialsCalls)
		})
	}
}

func TestNativeOAuthRefreshFailsClosedWithoutAtomicRepository(t *testing.T) {
	a := nativeControlAccount(PlatformKimi)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	inner := &refreshAPIAccountRepo{account: a}
	repo := &nativeRefreshWithoutCASRepo{inner: inner}
	svc := NewCNOAuthService(nil, repo, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	issuerCalls := 0
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		issuerCalls++
		return nativeControlReply(`{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
	})}
	fresh, err := svc.RefreshAccount(withOAuthRefreshRequestPath(context.Background()), a, true)
	require.Error(t, err)
	require.Nil(t, fresh)
	require.Equal(t, "request-owner", inner.account.GetCredential("access_token"))
	require.Zero(t, inner.updateCredentialsCalls)
	require.Zero(t, issuerCalls, "known missing persistence must not consume or rotate the provider grant")
}

func TestNativeOAuthRefreshInvalidGrantCannotAdoptRelinkedLogin(t *testing.T) {
	a := nativeControlAccount(PlatformKimi)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &refreshAPIAccountRepo{account: a}
	svc := NewCNOAuthService(nil, repo, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		changed := snapshotOAuthRefreshAccount(a)
		changed.Credentials["access_token"] = "relinked-login"
		changed.Credentials["refresh_token"] = "relinked-refresh"
		repo.account = changed
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
	})}
	fresh, err := svc.RefreshAccount(withOAuthRefreshRequestPath(context.Background()), a, false)
	require.Error(t, err, "a different refresh token alone is not proof of the same login")
	require.Nil(t, fresh)
	require.Equal(t, "relinked-login", repo.account.GetCredential("access_token"))
	require.Zero(t, repo.updateCredentialsCalls)
}

type nativeRefreshSideEffectRepo struct {
	*refreshAPIAccountRepo
	setErrorCalls int
	setTempCalls  int
}

func (r *nativeRefreshSideEffectRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrorCalls++
	r.account.Status = StatusError
	return nil
}
func (r *nativeRefreshSideEffectRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.setTempCalls++
	return nil
}

func TestNativeOAuthBackgroundRefreshPreservesRelinkAndRequiresCoordinator(t *testing.T) {
	for _, coordinated := range []bool{true, false} {
		t.Run(map[bool]string{true: "late invalid grant", false: "missing coordinator"}[coordinated], func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			a.Status = StatusActive
			a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &nativeRefreshSideEffectRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: a}}
			issuerCalls := 0
			refresher := &CNTokenRefresher{platform: PlatformKimi, client: &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				issuerCalls++
				changed := snapshotOAuthRefreshAccount(a)
				changed.Credentials["access_token"] = "relinked-login"
				changed.Credentials["refresh_token"] = "relinked-refresh"
				repo.account = changed
				return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
			})}}
			invalidator := &tokenCacheInvalidatorStub{}
			worker := &TokenRefreshService{accountRepo: repo, cfg: &config.TokenRefreshConfig{MaxRetries: 1}, refreshPolicy: DefaultBackgroundRefreshPolicy(), cacheInvalidator: invalidator}
			if coordinated {
				worker.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			}
			err := worker.refreshWithRetry(context.Background(), a, refresher, refresher, 2*time.Minute)
			require.Error(t, err)
			require.Zero(t, repo.setErrorCalls, "a late native grant failure cannot disable the newly relinked owner")
			require.Zero(t, repo.setTempCalls)
			require.Zero(t, repo.updateCredentialsCalls)
			require.Zero(t, invalidator.calls)
			require.Equal(t, StatusActive, repo.account.Status)
			if coordinated {
				require.Equal(t, 1, issuerCalls)
				require.Equal(t, "relinked-login", repo.account.GetCredential("access_token"))
			} else {
				require.Zero(t, issuerCalls, "missing coordinator cannot fall back to an unguarded native refresh")
				require.Equal(t, "request-owner", repo.account.GetCredential("access_token"))
			}
		})
	}
}

func TestNativeModelCatalogSyncRejectsResponseAfterRelink(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformDeepseek, PlatformStepFun} {
		for _, inPlace := range []bool{false, true} {
			t.Run(platform+"/"+map[bool]string{false: "replacement", true: "in-place"}[inPlace], func(t *testing.T) {
				a := nativeControlAccount(platform)
				repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
				changed := snapshotOAuthRefreshAccount(a)
				changed.Credentials["access_token"] = "relinked-model-owner"
				body := `{"data":[{"id":"old-owner-model","reasoning":false,"input_modalities":["text"],"context_window":64000}]}`
				if platform == PlatformDeepseek {
					body = `{"data":[{"id":"old-owner-model","input_modalities":["text"],"context_window":64000,"api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.0-rc.2"}}}]}`
				}
				if platform == PlatformStepFun {
					body = `{"data":[{"id":"old-owner-model","enable_reason":false,"enable_vision_input":false,"max_input_tokens":64000}]}`
				}
				upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
					if platform == PlatformDeepseek && req.URL.String() == modelsDevRegistryURL {
						return nativeControlReply(`{"deepseek":{"api":"https://api.deepseek.com","models":{"old-owner-model":{"id":"old-owner-model","reasoning":false,"limit":{"context":64000},"modalities":{"input":["text"]}}}}}`), nil
					}
					if inPlace {
						a.Credentials["access_token"] = "relinked-model-owner"
					} else {
						repo.account = changed
					}
					return nativeControlReply(body), nil
				}}
				svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig(), cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), a)
				require.Error(t, err, "model capabilities fetched with the previous login cannot be saved on the replacement owner")
				require.Nil(t, catalog)
				require.Nil(t, repo.updates, "no stale metadata may cross persistence")
				require.Nil(t, repo.account.GetUpstreamModelMetadataSnapshot())
			})
		}
	}
}
