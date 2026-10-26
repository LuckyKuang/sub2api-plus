//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type apiKeyObservationRepo struct {
	AccountRepository
	account          *Account
	writes, attempts int
	failure          error
}

func (r *apiKeyObservationRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *apiKeyObservationRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.writes++
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return r.failure
}
func sameAPIKeyObservationOwner(current, expected *Account) bool {
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) {
		return false
	}
	actual, actualErr := json.Marshal(current.Credentials)
	captured, capturedErr := json.Marshal(expected.Credentials)
	return actualErr == nil && capturedErr == nil && string(actual) == string(captured) && (current.ProxyID == nil || sameTestCNProxySnapshot(current.Proxy, expected.Proxy))
}

func (r *cnAPIKeyProxyProbeAccountRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	if !sameAPIKeyObservationOwner(r.account, expected) {
		return false, nil
	}
	return true, r.UpdateExtra(ctx, expected.ID, updates)
}
func (r *apiKeyObservationRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	r.attempts++
	if r.failure != nil {
		return false, r.failure
	}
	if !sameAPIKeyObservationOwner(r.account, expected) {
		return false, nil
	}
	return true, r.UpdateExtra(ctx, expected.ID, updates)
}

// Replacing an API key or its captured route revokes the old observation just
// as reauthorizing an OAuth login does. It cannot overwrite the new owner's
// balance, quota, availability, cooldown or unrelated metadata.
func TestCNAPIKeyObservationRejectsLateOwnerAndPersistenceFailure(t *testing.T) {
	for _, probe := range cnAPIKeyProxyProbeCases() {
		for _, change := range []string{"api-key", "nested-credentials", "proxy-address", "proxy-auth", "proxy-status", "proxy-expiry", "unbound", "storage-failure", "same-owner"} {
			t.Run(probe.name+"/"+change, func(t *testing.T) {
				account := probe.account()
				repo := &apiKeyObservationRepo{account: account}
				upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
					require.Equal(t, probe.authorization, req.Header.Get("Authorization"))
					switch change {
					case "api-key":
						account.Credentials["api_key"] = "replacement-private-api-key"
					case "nested-credentials":
						account.Credentials["owner_settings"].(map[string]any)["owner"] = "replacement-owner"
					case "proxy-address":
						account.Proxy.Host = "replacement.proxy.example"
					case "proxy-auth":
						account.Proxy.Password = "replacement-proxy-password"
					case "proxy-status":
						account.Proxy.Status = StatusDisabled
					case "proxy-expiry":
						expires := time.Now().Add(time.Minute)
						account.Proxy.ExpiresAt = &expires
					case "unbound":
						account.ProxyID = nil
						account.Proxy = nil
					case "storage-failure":
						repo.failure = errors.New("injected storage failure")
					}
					return nativeControlReply(probe.body), nil
				}}
				before, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				observation, err := probe.query(account, repo, nil, upstream)
				require.NoError(t, err)
				var successful, persisted bool
				var message string
				switch result := observation.(type) {
				case *CNProviderBalanceResult:
					successful, persisted, message = result.Success, result.Persisted, result.Error
				case *CNProviderQuotaProbeResult:
					successful, persisted, message = result.Success, result.Persisted, result.Error
				default:
					t.Fatalf("unexpected observation %T", observation)
				}
				if change == "same-owner" {
					require.True(t, successful)
					require.True(t, persisted)
					require.Equal(t, 1, repo.writes)
					return
				}
				require.False(t, successful, "an old or non-durable observation cannot certify the current owner")
				require.False(t, persisted)
				require.NotEmpty(t, message)
				require.Zero(t, repo.writes, "no unconditional observation write after owner changes")
				after, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}

// https://platform.moonshot.cn/docs/api/balance specifies code=0, status=true
// and a finite data.available_balance in the successful envelope.
// data.available_balance. Unknown/error/truncated responses must preserve the
// last known balance and its low-balance scheduling state, never fabricate 0.
func TestKimiAPIKeyWalletRejectsUnknownOrInvalidBalance(t *testing.T) {
	for name, body := range map[string]string{
		"missing": `{}`, "missing-data": `{"code":0,"status":true}`, "missing-balance": `{"code":0,"status":true,"data":{}}`,
		"null":                 `{"code":0,"status":true,"data":{"available_balance":null}}`,
		"text":                 `{"code":0,"status":true,"data":{"available_balance":"unknown"}}`,
		"boolean":              `{"code":0,"status":true,"data":{"available_balance":false}}`,
		"nan":                  `{"code":0,"status":true,"data":{"available_balance":"NaN"}}`,
		"infinity":             `{"code":0,"status":true,"data":{"available_balance":"Inf"}}`,
		"overflow":             `{"code":0,"status":true,"data":{"available_balance":1e999}}`,
		"business-error":       `{"code":401,"status":true,"data":{"available_balance":8}}`,
		"truncated":            `{"code":0,"status":true,"data":{"available_balance":8}`,
		"missing-status":       `{"code":0,"data":{"available_balance":5}}`,
		"false-status":         `{"code":0,"status":false,"data":{"available_balance":5}}`,
		"valid-zero":           `{"code":0,"status":true,"data":{"available_balance":0}}`,
		"valid-numeric-string": `{"code":0,"status":true,"data":{"available_balance":"12.5"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			probe := cnAPIKeyProxyProbeCases()[1]
			probe.body = body
			account := probe.account()
			account.Extra["kimi_balance"] = float64(29)
			account.Extra["kimi_balance_low"] = true
			repo := &apiKeyObservationRepo{account: account}
			observation, err := probe.query(account, repo, nil, &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: body})
			require.NoError(t, err)
			result := observation.(*CNProviderBalanceResult)
			if name == "valid-zero" || name == "valid-numeric-string" {
				require.True(t, result.Success)
				require.True(t, result.Persisted)
				require.Equal(t, 1, repo.writes)
				expected := float64(0)
				if name == "valid-numeric-string" {
					expected = 12.5
				}
				require.Equal(t, expected, result.Balance)
				return
			}
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.NotEmpty(t, result.Error)
			require.Zero(t, repo.writes)
			require.Equal(t, float64(29), account.Extra["kimi_balance"])
			require.Equal(t, true, account.Extra["kimi_balance_low"])
		})
	}
}

func TestCNAPIKeyWalletSchedulingRejectsChangesAfterObservation(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi} {
		for _, healthy := range []bool{false, true} {
			for _, mutation := range []string{"api-key", "other-cooldown", "manual-pause", "same-owner"} {
				t.Run(platform+map[bool]string{false: "/low/", true: "/healthy/"}[healthy]+mutation, func(t *testing.T) {
					account := paygAccount(platform)
					account.Schedulable = true
					account.Extra = map[string]any{}
					if healthy {
						until := time.Now().Add(time.Hour)
						account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, cnBalanceLowReason("previous API-key wallet")
					}
					repo := &nativeBalanceScheduleRepo{current: account}
					repo.afterObservation = func() {
						next := snapshotOAuthRefreshAccount(account)
						switch mutation {
						case "api-key":
							next.Credentials["api_key"] = "replacement-wallet-key"
						case "other-cooldown":
							until := time.Now().Add(2 * time.Hour)
							next.TempUnschedulableUntil, next.TempUnschedulableReason = &until, "transport-failure"
						case "manual-pause":
							next.Schedulable = false
						}
						repo.current = next
					}
					amount := "0"
					if healthy {
						amount = "50"
					}
					body := `{"code":0,"status":true,"data":{"available_balance":` + amount + `}}`
					if platform == PlatformDeepseek {
						body = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"` + amount + `"}]}`
					}
					svc := &CNProviderBalanceCheckService{accountRepo: repo, balanceService: NewCNProviderBalanceService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200, body: body}, nil)}
					outcome := svc.checkOne(context.Background(), account, 5)
					if mutation == "same-owner" {
						if healthy {
							require.Equal(t, cnBalanceCleared, outcome)
							require.Nil(t, repo.current.TempUnschedulableUntil)
						} else {
							require.Equal(t, cnBalancePaused, outcome)
							require.NotNil(t, repo.current.TempUnschedulableUntil)
						}
					} else {
						require.Equal(t, cnBalanceNoChange, outcome)
						if mutation == "other-cooldown" {
							require.Equal(t, "transport-failure", repo.current.TempUnschedulableReason)
						}
						if mutation == "manual-pause" {
							require.False(t, repo.current.Schedulable)
						}
						if mutation == "api-key" {
							require.Equal(t, "replacement-wallet-key", repo.current.GetCredential("api_key"))
						}
					}
					require.Zero(t, repo.legacyWrites, "API-key wallet scheduling must use the same atomic owner boundary")
				})
			}
		}
	}
}

// Subscription controls must observe a finite recognized quota window before
// advertising credential validity or making an old percentage freshly timed.
func TestCNAPIKeyCodingQuotaRejectsUnrecognizedOrMalformedObservation(t *testing.T) {
	cases := []struct {
		probe     cnAPIKeyProxyProbeCase
		malformed []string
	}{
		{cnAPIKeyProxyProbeCases()[2], []string{`{"limits":[{"detail":{}}]}`, `{"usage":{"limit":100}}`, `{"usage":{"limit":0,"remaining":0}}`, `{"usage":{"limit":"NaN","remaining":0}}`}},
		{cnAPIKeyProxyProbeCases()[3], []string{`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3}]}}`, `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":"bad","unit":3}]}}`}},
		{cnAPIKeyProxyProbeCase{name: "minimax-coding", platform: PlatformMiniMax, mode: AccountModeCoding, quota: true, baseURL: "https://api.minimaxi.com/v1", target: "https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains", authorization: "Bearer test-only-owner-api-key", body: `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]}`}, []string{`{"model_remains":[{"model_name":"general"}]}`, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":"Inf"}]}`}},
	}
	for _, test := range cases {
		bodies := append([]string{`{}`, `{"unrelated":"metadata"}`, test.probe.body}, test.malformed...)
		bodies = append(bodies, test.probe.body[:len(test.probe.body)-1])
		for index, body := range bodies {
			t.Run(test.probe.name+"/"+[]string{"missing", "unknown", "recognized"}[min(index, 2)]+"/"+body, func(t *testing.T) {
				account := test.probe.account()
				repo := &apiKeyObservationRepo{account: account}
				upstream := &cnBalanceResponseUpstream{statusCode: 200, body: body}
				before, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				cfg := cnProbeAllowlistConfig("api.kimi.com", "open.bigmodel.cn", "api.minimaxi.com")
				result, err := NewCNProviderQuotaService(repo, nil, upstream, cfg).QueryUsageForAccount(context.Background(), account)
				require.NoError(t, err)
				if index == 2 {
					require.True(t, result.Success)
					require.True(t, result.CredentialValid)
					require.True(t, result.Persisted)
					require.NotEmpty(t, result.Tiers)
					return
				}
				require.False(t, result.Success)
				require.False(t, result.CredentialValid)
				require.False(t, result.Persisted)
				require.NotEmpty(t, result.Error)
				require.Zero(t, repo.writes)
				after, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}

// A fresh partial snapshot must not lend its timestamp to windows or reset
// times that were not observed. Zero and overuse are still real observations.
func TestCNAPIKeyCodingQuotaPartialSnapshotCannotRefreshOldWindows(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			probe := cnAPIKeyProxyProbeCases()[2]
			body := `{"limits":[{"detail":{"limit":100,"remaining":100}}]}`
			switch platform {
			case PlatformZhipu:
				probe = cnAPIKeyProxyProbeCases()[3]
				body = `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":125}]}}`
			case PlatformMiniMax:
				probe = cnAPIKeyProxyProbeCase{name: "minimax-coding", platform: PlatformMiniMax, mode: AccountModeCoding, quota: true, baseURL: "https://api.minimaxi.com/v1"}
				body = `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]}`
			}
			account := probe.account()
			for _, suffix := range []string{cnExtraSuffix5hUsed, cnExtraSuffixWeeklyUsed, cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyCodeUsed} {
				account.Extra[cnExtraKey(platform, suffix)] = float64(89)
			}
			for _, suffix := range []string{cnExtraSuffix5hReset, cnExtraSuffixWeeklyReset, cnExtraSuffixMonthlyReset, cnExtraSuffixMonthlyCodeReset} {
				account.Extra[cnExtraKey(platform, suffix)] = "2026-11-01T00:00:00Z"
			}
			repo := &apiKeyObservationRepo{account: account}
			cfg := cnProbeAllowlistConfig("api.kimi.com", "open.bigmodel.cn", "api.minimaxi.com")
			result, err := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200, body: body}, cfg).QueryUsageForAccount(context.Background(), account)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Len(t, result.Tiers, 1)
			expected := float64(0)
			if platform == PlatformZhipu {
				expected = 125
			}
			require.Equal(t, expected, account.Extra[cnExtraKey(platform, cnExtraSuffix5hUsed)])
			for _, suffix := range []string{cnExtraSuffix5hReset, cnExtraSuffixWeeklyUsed, cnExtraSuffixWeeklyReset, cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyReset, cnExtraSuffixMonthlyCodeUsed, cnExtraSuffixMonthlyCodeReset} {
				require.Nil(t, account.Extra[cnExtraKey(platform, suffix)], "unobserved value must remain unknown: %s", suffix)
			}
		})
	}
}
