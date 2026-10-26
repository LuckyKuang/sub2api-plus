//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type cnAPIKeyBoundaryUpstream struct {
	body io.ReadCloser
}

func (u cnAPIKeyBoundaryUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: u.body}, nil
}

func (u cnAPIKeyBoundaryUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// A complete JSON prefix does not prove a complete HTTP response. The provider
// must finish within the documented body cap before its observation is usable.
func TestCNAPIKeyControlRequiresCompleteBoundedResponse(t *testing.T) {
	for _, probe := range []struct {
		platform, mode, base, body string
		quota                      bool
	}{
		{PlatformDeepseek, AccountModePayG, "https://api.deepseek.com", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.5"}]}`, false},
		{PlatformKimi, AccountModePayG, "https://api.moonshot.cn/v1", `{"code":0,"status":true,"data":{"available_balance":12.5}}`, false},
		{PlatformKimi, AccountModeCoding, "https://api.kimi.com/coding/v1", `{"usage":{"limit":100,"remaining":75}}`, true},
		{PlatformZhipu, AccountModeCoding, "https://open.bigmodel.cn/api/coding/paas/v4", `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}]}}`, true},
		{PlatformMiniMax, AccountModeCoding, "https://api.minimaxi.com/v1", `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":75}]}`, true},
	} {
		for _, boundary := range []string{"complete", "exact-limit", "one-byte-over-limit", "read-failure"} {
			t.Run(probe.platform+"/"+probe.mode+"/"+boundary, func(t *testing.T) {
				account := &Account{ID: 841, Platform: probe.platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "boundary-owner-key", "account_mode": probe.mode, "base_url": probe.base},
					Extra:       map[string]any{"retained": "last-known-observation", probe.platform + "_balance_low": true}}
				repo := &apiKeyObservationRepo{account: account}
				body := probe.body
				if boundary == "exact-limit" || boundary == "one-byte-over-limit" {
					body += strings.Repeat(" ", 256*1024-len(body))
					if boundary == "one-byte-over-limit" {
						body += " "
					}
				}
				var reader io.Reader = strings.NewReader(body)
				if boundary == "read-failure" {
					reader = io.MultiReader(reader, cnAPIKeyBoundaryReadError{})
				}
				upstream := cnAPIKeyBoundaryUpstream{body: io.NopCloser(reader)}
				var success, persisted bool
				var message string
				if probe.quota {
					result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsageForAccount(context.Background(), account)
					require.NoError(t, err)
					success, persisted, message = result.Success, result.Persisted, result.Error
					if boundary == "one-byte-over-limit" || boundary == "read-failure" {
						require.False(t, result.CredentialValid)
					}
				} else {
					result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalanceForAccount(context.Background(), account)
					require.NoError(t, err)
					success, persisted, message = result.Success, result.Persisted, result.Error
				}
				if boundary == "complete" || boundary == "exact-limit" {
					require.True(t, success)
					require.True(t, persisted)
					require.Equal(t, 1, repo.writes)
					return
				}
				require.False(t, success, "incomplete or oversized transport data is not an observation")
				require.False(t, persisted)
				require.NotEmpty(t, message)
				require.Zero(t, repo.attempts, "no CAS, scheduler outbox or cache write may follow a failed read")
				require.Zero(t, repo.writes)
				require.Equal(t, map[string]any{"retained": "last-known-observation", probe.platform + "_balance_low": true}, account.Extra)
			})
		}
	}
}

type cnAPIKeyBoundaryReadError struct{}

func (cnAPIKeyBoundaryReadError) Read([]byte) (int, error) {
	return 0, errors.New("injected provider response read failure")
}

// DeepSeek has multiple currencies: one unparseable sibling cannot certify
// that all wallets are below the threshold, nor clear a previous low marker.
// https://api-docs.deepseek.com/api/get-user-balance/ defines currency as a
// string enum (CNY/USD); an absent or foreign value cannot be relabeled CNY.
func TestDeepSeekAPIKeyWalletRejectsIncompleteObservationBeforeScheduling(t *testing.T) {
	for name, body := range map[string]string{
		"truncated-json":                `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"50"}]`,
		"text-availability":             `{"is_available":"unknown","balance_infos":[{"currency":"CNY","total_balance":"50"}]}`,
		"numeric-availability":          `{"is_available":0,"balance_infos":[{"currency":"CNY","total_balance":"50"}]}`,
		"null-availability":             `{"is_available":null,"balance_infos":[{"currency":"CNY","total_balance":"50"}]}`,
		"mixed-invalid-currency-wallet": `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0"},{"currency":"USD","total_balance":"unknown"}]}`,
		"mixed-missing-balance":         `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0"},{"currency":"USD"}]}`,
		"mixed-non-object":              `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0"},false]}`,
		"mixed-infinite-balance":        `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0"},{"currency":"USD","total_balance":"Inf"}]}`,
		"valid-healthy-dual-wallet":     `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"0"},{"currency":"USD","total_balance":"50"}]}`,
		"valid-unavailable-zero":        `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`,
		"missing-currency":              `{"is_available":true,"balance_infos":[{"total_balance":"0"}]}`,
		"wrong-type-currency":           `{"is_available":true,"balance_infos":[{"currency":123,"total_balance":"0"}]}`,
		"unsupported-currency":          `{"is_available":true,"balance_infos":[{"currency":"EUR","total_balance":"0"}]}`,
	} {
		for _, wasPaused := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/active", true: "/recovering"}[wasPaused], func(t *testing.T) {
				account := paygAccount(PlatformDeepseek)
				account.Schedulable = true
				account.Extra = map[string]any{"deepseek_balance": float64(29), "deepseek_balance_low": true}
				if wasPaused {
					until := time.Now().Add(time.Hour)
					account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, cnBalanceLowReason("previous wallet")
				}
				repo := &nativeBalanceScheduleRepo{current: account}
				svc := &CNProviderBalanceCheckService{accountRepo: repo,
					balanceService: NewCNProviderBalanceService(repo, nil, &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: body}, nil)}
				outcome := svc.checkOne(context.Background(), account, 5)
				if name == "valid-healthy-dual-wallet" {
					if wasPaused {
						require.Equal(t, cnBalanceCleared, outcome)
					} else {
						require.Equal(t, cnBalanceNoChange, outcome)
					}
					return
				}
				if name == "valid-unavailable-zero" {
					if wasPaused {
						require.Equal(t, cnBalanceNoChange, outcome)
					} else {
						require.Equal(t, cnBalancePaused, outcome)
					}
					return
				}
				require.Equal(t, cnBalanceNoChange, outcome)
				require.Zero(t, repo.conditionalWrites, "unknown wallets cannot pause or recover scheduling")
				require.Zero(t, repo.legacyWrites)
				require.Equal(t, wasPaused, account.TempUnschedulableUntil != nil)
				// Independently assert that invalid observations never reach persistence.
				observationRepo := &apiKeyObservationRepo{account: account}
				result, err := NewCNProviderBalanceService(observationRepo, nil, &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: body}, nil).QueryBalanceForAccount(context.Background(), account)
				require.NoError(t, err)
				require.False(t, result.Success)
				require.False(t, result.Persisted)
				require.NotEmpty(t, result.Error)
				require.Zero(t, observationRepo.attempts)
				require.Equal(t, map[string]any{"deepseek_balance": float64(29), "deepseek_balance_low": true}, account.Extra)
			})
		}
	}
}
