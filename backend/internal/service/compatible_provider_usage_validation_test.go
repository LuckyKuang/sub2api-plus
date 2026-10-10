//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type usageProbeErrorReader struct{ err error }

func (r *usageProbeErrorReader) Read([]byte) (int, error) { return 0, r.err }

type usageProbeStateRepo struct {
	*commandCodeStatefulRepo
	extraWrites []map[string]any
}

func (r *usageProbeStateRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.extraWrites = append(r.extraWrites, updates)
	return nil
}

// Requirement: an invalid balance is not zero credit and cannot overwrite a
// known snapshot or change wallet scheduling (COMPATIBLE_AGGREGATORS.md).
func TestClineInvalidBalancePreservesCreditWallet(t *testing.T) {
	for _, value := range []string{`"unavailable"`, `""`, `"NaN"`, `"Inf"`, `"1e309"`, `1e309`, `null`, `{}`} {
		t.Run(value, func(t *testing.T) {
			account := clineTestAccount(901)
			upstream := newClineAccountUpstream(commandCodeAlphaResponse{status: http.StatusOK, body: clineUsageLimitsBody}, `{"balance":`+value+`}`)
			repo := &cnBalanceProbeRepo{account: account}
			balance, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(context.Background(), account.ID)
			require.NoError(t, err)
			require.False(t, balance.Success)
			require.False(t, balance.Persisted)
			require.Empty(t, repo.extraWrites)

			// A valid subscription response may still update its own windows, but
			// cannot persist the bad balance or clear/cool the credit wallet.
			now := time.Now()
			setAccountModelRateLimitSnapshot(account, clineCreditsRateLimitKey, now.Add(time.Hour), clineCreditsReason, now)
			walletRepo := &clineAccountRepo{accounts: []Account{*account}}
			usage, err := NewCNProviderQuotaService(walletRepo, nil, upstream, clineUsageTestConfig()).QueryUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.True(t, usage.Success)
			require.Nil(t, usage.Balance)
			require.Len(t, walletRepo.extraWrites[account.ID], 1)
			require.NotContains(t, walletRepo.extraWrites[account.ID][0], "cline_balance")
			require.Empty(t, walletRepo.modelLimits)
		})
	}
}

// A nonempty, unparseable subscription response does not prove cancellation.
func TestClineInvalidLimitsPreserveSubscriptionAndCooldown(t *testing.T) {
	for _, limits := range []string{
		`[{"type":"five_hour"}]`,
		`[{"type":"five_hour","percentUsed":"unavailable"}]`,
		`[{"type":"weekly","percentUsed":-1}]`,
		`[{"type":"monthly","percentUsed":1e309}]`,
		`[{"type":"future_window","percentUsed":10}]`,
		`[{"type":"five_hour","percentUsed":5},{"type":"weekly","percentUsed":null}]`,
	} {
		t.Run(limits, func(t *testing.T) {
			account := withClinePassSnapshot(clineTestAccount(902), nil)
			now := time.Now()
			setAccountModelRateLimitSnapshot(account, clinePassRateLimitKey, now.Add(time.Hour), clinePassLimitReason, now)
			repo := &clineAccountRepo{accounts: []Account{*account}}
			upstream := newClineAccountUpstream(commandCodeAlphaResponse{status: http.StatusOK, body: `{"limits":` + limits + `}`}, `{"balance":5000000}`)
			result, err := NewCNProviderQuotaService(repo, nil, upstream, clineUsageTestConfig()).QueryUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Empty(t, repo.extraWrites)
			require.Empty(t, repo.modelLimits)
			require.True(t, repo.accounts[0].isRateLimitActiveForKey(clinePassRateLimitKey))
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestClineValidExhaustedWindowWithUnknownSibling(t *testing.T) {
	account := clineTestAccount(904)
	repo := &clineAccountRepo{accounts: []Account{*account}}
	upstream := newClineAccountUpstream(commandCodeAlphaResponse{status: http.StatusOK, body: `{"limits":[{"type":"five_hour","percentUsed":125},{"type":"future_window","percentUsed":10}]}`}, `{"balance":5000000}`)
	result, err := NewCNProviderQuotaService(repo, nil, upstream, clineUsageTestConfig()).QueryUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 125}}, result.Tiers)
	require.Contains(t, repo.modelLimits[account.ID], clinePassRateLimitKey)
	require.NotContains(t, repo.modelLimits[account.ID], clineCreditsRateLimitKey)
}

// Non-finite "purchased" credit cannot prove a top-up or unlock a blocked account.
func TestCommandCodeNonFiniteCreditsDoNotClearCooldown(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "-Inf", "Infinity", "1e309"} {
		t.Run(value, func(t *testing.T) {
			account := commandCodeUsageAccount()
			until := time.Now().Add(time.Hour)
			account.TempUnschedulableUntil = &until
			account.TempUnschedulableReason = commandCodeUsageLimitReason
			repo := &usageProbeStateRepo{commandCodeStatefulRepo: &commandCodeStatefulRepo{account: account}}
			upstream := newCommandCodeAlphaUpstream(`{"org":null}`)
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			upstream.responses["/alpha/billing/credits"] = commandCodeAlphaResponse{status: http.StatusOK, body: `{"credits":{"purchasedCredits":` + string(encoded) + `}}`}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Nil(t, result.Balance)
			require.Zero(t, repo.clearCalls)
			require.Empty(t, repo.extraWrites)
			require.Equal(t, &until, account.TempUnschedulableUntil)
		})
	}
}

// Complete JSON followed by a read error, or an oversized body with a valid
// prefix, is not a complete successful probe. Neither may reach persistence.
func TestCompatibleUsageProbeRejectsIncompleteOrOversizedBodies(t *testing.T) {
	const maxProbeBody = 256 * 1024 // Documented body limit; independent of the implementation constant.
	for _, provider := range []string{PlatformCline, PlatformCommandCode} {
		for _, tc := range []struct {
			name        string
			size        int
			readErr     error
			wantSuccess bool
		}{
			{name: "read error", readErr: io.ErrUnexpectedEOF},
			{name: "one byte oversized", size: maxProbeBody + 1},
			{name: "exact size limit", size: maxProbeBody, wantSuccess: true},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				var account *Account
				var upstream *commandCodeAlphaUpstream
				path, body := "/alpha/whoami", `{"org":null}`
				if provider == PlatformCline {
					account = clineTestAccount(903)
					upstream = newClineAccountUpstream(commandCodeAlphaResponse{}, `{"balance":5000000}`)
					path, body = "/api/v1/users/me", `{"id":"u-1"}`
				} else {
					account = commandCodeUsageAccount()
					upstream = newCommandCodeAlphaUpstream(body)
				}
				response := commandCodeAlphaResponse{status: http.StatusOK, body: body, readErr: tc.readErr}
				if tc.size > 0 {
					response.body += strings.Repeat(" ", tc.size-len(body))
				}
				upstream.responses[path] = response
				repo := &cnBalanceProbeRepo{account: account}
				result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(context.Background(), account.ID)
				if tc.wantSuccess {
					require.NoError(t, err)
					require.True(t, result.Success)
					require.True(t, result.Persisted)
					require.Len(t, repo.extraWrites, 1)
					require.Len(t, upstream.requests, 2)
					return
				}
				require.True(t, err != nil || (result != nil && !result.Success))
				require.Empty(t, repo.extraWrites)
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}
