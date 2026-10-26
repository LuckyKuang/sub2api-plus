//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

// Expected wire and states come from ZCode@3c5b2276's zaiStartPlanBilling.ts
// and codingPlanProviderAvailability.ts. They do not call the parser to build
// expectations. This is a new capability; the old unsupported result is not
// classified as a defect regression.
const startPlanActiveBody = `{"code":200,"success":true,"data":{"server_time":2000000000,"plans":[{"user_plan_id":"instance-1","plan_id":"zcode-v3-start-plan","name":"ZCode V3 Start Plan","status":"active","starts_at":1900000000,"ends_at":2100000000}],"balances":[{"user_plan_id":"instance-1","capabilities":["model:glm-5.3-FLASH","MODEL:GLM-5.3-Flash","model:future-glm"]},{"user_plan_id":"instance-1","show_name":"glm-4.7-flashx"}]}}`

type startPlanEntitlementUpstream struct {
	request   *http.Request
	proxy     string
	calls     int
	status    int
	body      string
	header    http.Header
	err       error
	onRequest func()
}

func (u *startPlanEntitlementUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.request, u.proxy = req, proxy
	u.calls++
	if u.onRequest != nil {
		u.onRequest()
	}
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{StatusCode: u.status, Header: u.header, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func (u *startPlanEntitlementUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func newStartPlanEntitlementFixture() (*CNProviderQuotaService, *zhipuProjectQuotaRepo, *startPlanEntitlementUpstream) {
	proxyID := int64(92)
	a := &Account{ID: 971, Platform: PlatformZhipu, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 1,
		Credentials: map[string]any{"account_mode": AccountModeCoding, "oauth_provider": "zai", "plan_kind": ZhipuPlanStartPlan, "zcode_jwt_token": "start-business-jwt", "api_key": "legacy-ignored", "base_url": "https://attacker.example", "header_override_enabled": true, "header_overrides": map[string]any{"Authorization": "Bearer attacker", "User-Agent": "attacker", "X-Api-Key": "attacker", "bigmodel-project": "attacker"}},
		Extra:       map[string]any{"retained": "management", "zhipu_offpeak_model_ids": []any{"offpeak-only"}}, ProxyID: &proxyID,
		Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080}}
	repo := &zhipuProjectQuotaRepo{account: a}
	u := &startPlanEntitlementUpstream{status: 200, body: startPlanActiveBody, header: make(http.Header)}
	return NewCNProviderQuotaService(repo, nil, u, cnProbeAllowlistConfig("zcode.z.ai")), repo, u
}

func startPlanResult(t *testing.T, result *CNProviderQuotaProbeResult) map[string]any {
	t.Helper()
	body, err := json.Marshal(result)
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(body, &envelope))
	state, ok := envelope["start_plan"].(map[string]any)
	require.True(t, ok, "Start observations must have their own four-state result, not quota tiers")
	return state
}

func TestZhipuStartPlanOfficialBalanceWireAndIsolation(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		t.Run(provider, func(t *testing.T) {
			s, repo, u := newStartPlanEntitlementFixture()
			repo.account.Credentials["oauth_provider"] = provider
			result, err := s.QueryUsage(context.Background(), repo.account.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.True(t, result.CredentialValid)
			state := startPlanResult(t, result)
			require.Equal(t, "available", state["state"])
			require.Equal(t, true, state["models_known"])
			require.NotEmpty(t, state["identity_generation"])
			owner, ownerErr := requestCredentialAccount(u.request.Context(), repo.account)
			require.NoError(t, ownerErr)
			require.Equal(t, "start-business-jwt", owner.GetCredential("zcode_jwt_token"))
			require.Equal(t, []any{"GLM-5.3-Flash", "future-glm", "GLM-4.7-FlashX"}, state["models"])
			require.Equal(t, "https://zcode.z.ai/api/v1/zcode-plan/billing/balance?app_version=3.16.2", u.request.URL.String())
			require.Equal(t, http.MethodGet, u.request.Method)
			require.Equal(t, "Bearer start-business-jwt", u.request.Header.Get("Authorization"))
			require.Equal(t, "ZCode/3.16.2", u.request.Header.Get("User-Agent"))
			require.Equal(t, "3.16.2", u.request.Header.Get("X-ZCode-App-Version"))
			require.Empty(t, u.request.Header.Get("X-ZCode-Agent"))
			require.Empty(t, u.request.Header.Get("X-Api-Key"))
			require.Empty(t, u.request.Header.Get("bigmodel-project"))
			require.True(t, HTTPUpstreamRedirectsDisabled(u.request.Context()))
			require.Equal(t, "http://proxy.example:8080", u.proxy)
			require.Len(t, repo.updates, 1, "Start observation must never mutate coding quota/off-peak model or scheduling keys")
			require.Contains(t, repo.updates, "zhipu_start_plan_entitlement")
			require.Empty(t, result.Tiers)
		})
	}
}

func TestZhipuStartPlanAvailabilityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, date, state string
		known                   bool
		models                  []any
		reason                  string
	}{
		{"missing-models", `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}]}}`, "", "available", false, nil, ""},
		{"empty-balances-unknown", `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[]}}`, "", "available", false, nil, ""},
		{"explicit-empty-capability", `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[{"plan_id":"start-plan","capabilities":[]}]}}`, "", "available", true, []any{}, ""},
		{"expired-model-bucket", `{"data":{"server_time":2000000000,"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[{"plan_id":"start-plan","capabilities":["model:GLM-5.3"],"expires_at":2000000000}]}}`, "", "available", true, []any{}, ""},
		{"foreign-model-scope", `{"data":{"plans":[{"user_plan_id":"active-instance","plan_id":"start-plan","status":"active"},{"user_plan_id":"expired-instance","plan_id":"start-plan","status":"expired"}],"balances":[{"user_plan_id":"expired-instance","plan_id":"start-plan","capabilities":["model:GLM-4.7"]},{"user_plan_id":"active-instance","plan_id":"start-plan","capabilities":["model:GLM-5.3"]}]}}`, "", "available", true, []any{"GLM-5.3"}, ""},
		{"pending", `{"data":{"server_time":2000000000,"plans":[{"plan_id":"start-plan","status":"active","entitlements":[{"effective_at":2100000000}]}],"balances":[]}}`, "", "pending", true, []any{}, "not_effective"},
		{"date-beats-old-json-clock", startPlanActiveBody, time.Unix(2100000000, 0).UTC().Format(http.TimeFormat), "unavailable", true, []any{}, "expired"},
		{"no-active-start", `{"data":{"plans":[{"plan_id":"coding-plan","status":"active"}],"balances":[{"capabilities":["model:GLM-5.3"]}]}}`, "", "unavailable", true, []any{}, "not_entitled"},
		{"missing-plans", `{"data":{"balances":[{"capabilities":["model:GLM-5.3"]}]}}`, "", "unknown", false, nil, "invalid_response"},
		{"wrong-plans-type", `{"data":{"plans":{},"balances":[]}}`, "", "unknown", false, nil, "invalid_response"},
		{"damaged-active-plan", `{"data":{"plans":[{"plan_id":"start-plan","status":"active","ends_at":"tomorrow"}],"balances":[{"capabilities":["model:GLM-5.3"]}]}}`, "", "unknown", false, nil, "invalid_response"},
		{"missing-product-identity", `{"data":{"plans":[{"status":"active"}],"balances":[{"capabilities":["model:GLM-5.3"]}]}}`, "", "unknown", false, nil, "invalid_response"},
		{"invalid-server-clock", `{"data":{"server_time":-1,"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[]}}`, "", "unknown", false, nil, "invalid_response"},
		{"missing-future-effective-time", `{"data":{"server_time":2000000000,"plans":[{"plan_id":"start-plan","status":"active","entitlements":[{"effective_at":2100000000},{}]}],"balances":[]}}`, "", "available", false, nil, ""},
		{"damaged-capability", `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[{"capabilities":["model:GLM-5.3",42]}]}}`, "", "unknown", false, nil, "invalid_response"},
		{"business-failure", `{"success":false,"code":1001,"data":{"plans":[]}}`, "", "unavailable", true, []any{}, "auth_failed"},
		{"invalid-json", `{"data":`, "", "unknown", false, nil, "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, u := newStartPlanEntitlementFixture()
			u.body = tc.body
			u.header.Set("Date", tc.date)
			result, err := s.QueryUsage(context.Background(), 971)
			require.NoError(t, err)
			state := startPlanResult(t, result)
			require.Equal(t, tc.state, state["state"])
			require.Equal(t, tc.known, state["models_known"])
			if tc.models != nil {
				require.Equal(t, tc.models, state["models"])
			}
			if tc.reason != "" {
				require.Equal(t, tc.reason, state["reason"])
			}
			require.Equal(t, tc.state != "unknown", result.Persisted)
		})
	}
}

func TestZhipuStartPlanFailureAndOwnerFence(t *testing.T) {
	for _, tc := range []string{"network", "401", "403", "relink", "plan", "estate", "proxy", "save-failure"} {
		t.Run(tc, func(t *testing.T) {
			s, repo, u := newStartPlanEntitlementFixture()
			switch tc {
			case "network":
				u.err = errors.New("temporary failure containing start-business-jwt")
			case "401":
				u.status = 401
			case "403":
				u.status = 403
			case "relink":
				u.onRequest = func() { repo.account.Credentials["zcode_jwt_token"] = "replacement" }
			case "plan":
				u.onRequest = func() { repo.account.Credentials["plan_kind"] = ZhipuPlanOffPeak }
			case "estate":
				u.onRequest = func() { repo.account.Credentials["oauth_provider"] = "bigmodel" }
			case "proxy":
				u.onRequest = func() { id := int64(93); repo.account.ProxyID = &id }
			case "save-failure":
				s.accountRepo = &startPlanUnavailablePersistenceRepo{AccountRepository: repo}
			}
			result, err := s.QueryUsage(context.Background(), 971)
			require.NoError(t, err)
			require.NotContains(t, result.Error, "start-business-jwt")
			require.Empty(t, result.Tiers)
			if tc == "401" || tc == "403" {
				state := startPlanResult(t, result)
				require.Equal(t, "unavailable", state["state"])
				require.Equal(t, "auth_failed", state["reason"])
				require.Equal(t, 1, u.calls, "business auth failure must not issue a project PAT or fabricate refresh")
				return
			}
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Nil(t, repo.updates)
			if tc == "network" {
				require.Equal(t, "unknown", startPlanResult(t, result)["state"])
			}
		})
	}
}

type startPlanUnavailablePersistenceRepo struct{ AccountRepository }

func (*startPlanUnavailablePersistenceRepo) UpdateCNControlExtraIfUnchanged(context.Context, *Account, map[string]any) (bool, error) {
	return false, errors.New("storage unavailable")
}

func TestZhipuStartPlanSelectionAndPublicCatalog(t *testing.T) {
	for _, tc := range []struct {
		name, state     string
		known           bool
		models          []string
		allowed, listed bool
	}{
		{"no-observation", "", false, nil, true, false},
		{"models-unknown", "available", false, nil, true, false},
		{"explicit-empty", "available", true, []string{}, false, false},
		{"authorized", "available", true, []string{"GLM-5.3"}, true, true},
		{"unauthorized", "available", true, []string{"GLM-4.7"}, false, false},
		{"pending", "pending", true, []string{}, false, false},
		{"unavailable", "unavailable", true, []string{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo, _ := newStartPlanEntitlementFixture()
			a := repo.account
			a.Credentials["model_mapping"] = map[string]any{"client-alias": "GLM-5.3"}
			if tc.state != "" {
				state := &ZhipuStartPlanEntitlement{State: tc.state, ModelsKnown: tc.known, Models: tc.models, ServerTime: 2000000000, ObservedAt: time.Now().Unix()}
				bindStartPlanObservation(context.Background(), a, state)
				a.Extra[zhipuStartPlanExtraKey] = state
			}
			require.Equal(t, tc.allowed, a.IsModelSupported("client-alias"), "actual mapping target must satisfy Start permission")
			ids := startPlanPublicModelIDs(context.Background(), a)
			if tc.listed {
				require.Equal(t, []string{"client-alias"}, ids)
			} else {
				require.Empty(t, ids)
			}
			// Start-only groups cannot publish platform/Claude defaults when their
			// entitlement is absent, unknown or explicitly negative.
			listing := &startPlanListingRepo{accounts: []Account{*a}}
			models := (&GatewayService{accountRepo: listing}).GetAvailableModels(context.Background(), nil, PlatformZhipu)
			require.NotNil(t, models)
			if tc.listed {
				require.Equal(t, []string{"client-alias"}, models)
			} else {
				require.Empty(t, models)
			}
		})
	}
}

type startPlanListingRepo struct {
	AccountRepository
	accounts []Account
}

func (r *startPlanListingRepo) ListSchedulable(context.Context) ([]Account, error) {
	return r.accounts, nil
}

func TestZhipuStartPlanObservationCannotCrossOwnerOrReactivateOnTime(t *testing.T) {
	for _, mutation := range []string{"token", "plan", "estate", "proxy", "identity", "stale-positive", "stale-negative", "pending-elapsed", "expired"} {
		t.Run(mutation, func(t *testing.T) {
			_, repo, _ := newStartPlanEntitlementFixture()
			a := repo.account
			state := &ZhipuStartPlanEntitlement{State: "available", ModelsKnown: true, Models: []string{"GLM-5.3"}, ServerTime: 2000000000, ObservedAt: time.Now().Unix()}
			bindStartPlanObservation(context.Background(), a, state)
			a.Extra[zhipuStartPlanExtraKey] = state
			switch mutation {
			case "token":
				a.Credentials["zcode_jwt_token"] = "new-token"
			case "plan":
				a.Credentials["plan_kind"] = ZhipuPlanOffPeak
			case "estate":
				a.Credentials["oauth_provider"] = "bigmodel"
			case "proxy":
				a.Proxy.Port++
			case "identity":
				a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "3.16.3"}
			case "stale-positive":
				state.ObservedAt = time.Now().Add(-time.Hour).Unix()
			case "stale-negative":
				state.State = "unavailable"
				state.ObservedAt = time.Now().Add(-time.Hour).Unix()
			case "pending-elapsed":
				state.State = "pending"
				state.EffectiveAt = 1999999999
			case "expired":
				state.ExpiresAt = 2000000000
			}
			require.Empty(t, startPlanPublicModelIDs(context.Background(), a))
			if mutation == "stale-negative" || mutation == "pending-elapsed" || mutation == "expired" {
				require.False(t, a.IsModelSupported("GLM-5.3"), "time cannot manufacture a new grant")
			}
			if mutation == "stale-positive" {
				require.False(t, a.IsModelSupported("GLM-4.7"), "staleness cannot enlarge the last proven whitelist")
			}
		})
	}
}

func TestZhipuStartPlanCatalogSyncUsesBalanceAndNeverProjectOrRegistry(t *testing.T) {
	for _, body := range []string{startPlanActiveBody, `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[]}}`, `{"data":{"plans":[]}}`} {
		s, repo, u := newStartPlanEntitlementFixture()
		u.body = body
		testService := &AccountTestService{accountRepo: repo, httpUpstream: u, cfg: s.cfg}
		catalog, err := testService.SyncUpstreamModelCatalog(context.Background(), repo.account)
		require.NoError(t, err)
		require.NotNil(t, catalog)
		require.Equal(t, 1, u.calls)
		require.Contains(t, u.request.URL.Path, "/zcode-plan/billing/balance")
		require.Len(t, repo.updates, 1)
		require.Contains(t, repo.updates, zhipuStartPlanExtraKey)
		require.NotContains(t, repo.updates, UpstreamModelMetadataExtraKey, "Start permission must never poison the shared coding/offpeak metadata")
	}
}

func TestZhipuStartPlanIdentityPrecedenceAndRequiredLogin(t *testing.T) {
	for _, mode := range []string{"global-version", "account-version", "legacy-no-mode", "missing-jwt"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, u := newStartPlanEntitlementFixture()
			global, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "zcode", Version: "3.16.4"})
			require.NoError(t, err)
			ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return global })
			version := "3.16.4"
			switch mode {
			case "account-version":
				repo.account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "3.16.3"}
				version = "3.16.3"
			case "legacy-no-mode":
				delete(repo.account.Credentials, "account_mode")
			case "missing-jwt":
				delete(repo.account.Credentials, "zcode_jwt_token")
			}
			result, err := s.QueryUsage(ctx, repo.account.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			if mode == "missing-jwt" {
				require.Equal(t, "unavailable", result.StartPlan.State)
				require.Equal(t, "reauthorization_required", result.StartPlan.Reason)
				require.False(t, result.CredentialValid)
				require.Zero(t, u.calls)
				return
			}
			require.Equal(t, version, u.request.URL.Query().Get("app_version"))
			require.Equal(t, "ZCode/"+version, u.request.UserAgent())
			require.Equal(t, version, u.request.Header.Get("X-ZCode-App-Version"))
		})
	}
}

func TestZhipuStartPlanPublicCatalogRechecksCachedOwnerAndIdentity(t *testing.T) {
	_, repo, _ := newStartPlanEntitlementFixture()
	a := repo.account
	state := &ZhipuStartPlanEntitlement{State: "available", ModelsKnown: true, Models: []string{"GLM-5.3"}, ObservedAt: time.Now().Unix(), ServerTime: time.Now().Unix()}
	bindStartPlanObservation(context.Background(), a, state)
	a.Extra[zhipuStartPlanExtraKey] = state
	listing := &startPlanListingRepo{accounts: []Account{*a}}
	cache := gocache.New(time.Minute, time.Minute)
	gateway := &GatewayService{accountRepo: listing, modelsListCache: cache, modelsListCacheTTL: time.Minute}
	for _, platform := range []string{PlatformZhipu, ""} {
		cache.Set(modelsListCacheKey(nil, platform), []string{"stale-unproven-model"}, time.Minute)
		require.Equal(t, []string{"GLM-5.3"}, gateway.GetAvailableModels(context.Background(), nil, platform))
	}
	listing.accounts[0].Credentials["zcode_jwt_token"] = "new-login"
	require.NotNil(t, gateway.GetAvailableModels(context.Background(), nil, PlatformZhipu))
	require.Empty(t, gateway.GetAvailableModels(context.Background(), nil, PlatformZhipu))
}
