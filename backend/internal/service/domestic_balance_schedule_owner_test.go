//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Credential-owner containment applies to every consequence of a wallet
// observation, including the later scheduler write, not just its Extra snapshot.
type nativeBalanceScheduleRepo struct {
	AccountRepository
	current           *Account
	afterObservation  func()
	legacyWrites      int
	conditionalWrites int
}

func (r *nativeBalanceScheduleRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.current, nil
}

func (r *nativeBalanceScheduleRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, owner *Account, _ map[string]any) (bool, error) {
	if !nativeBalanceSameOwner(r.current, owner) {
		return false, nil
	}
	if r.afterObservation != nil {
		r.afterObservation()
	}
	return true, nil
}

func nativeBalanceSameOwner(a, b *Account) bool {
	if a == nil || b == nil || a.ID != b.ID || a.Platform != b.Platform || a.Type != b.Type || !reflect.DeepEqual(a.ProxyID, b.ProxyID) {
		return false
	}
	x, err := json.Marshal(a.Credentials)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b.Credentials)
	return err == nil && string(x) == string(y)
}

func (r *nativeBalanceScheduleRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.legacyWrites++
	r.current.TempUnschedulableUntil = &until
	r.current.TempUnschedulableReason = reason
	return nil
}

func (r *nativeBalanceScheduleRepo) ClearTempUnschedulable(context.Context, int64) error {
	r.legacyWrites++
	r.current.TempUnschedulableUntil = nil
	r.current.TempUnschedulableReason = ""
	return nil
}

func (r *nativeBalanceScheduleRepo) UpdateCNBalanceScheduleIfUnchanged(_ context.Context, owner *Account, until *time.Time, reason string) (bool, error) {
	r.conditionalWrites++
	if !nativeBalanceSameOwner(r.current, owner) || r.current.Status != owner.Status || r.current.Schedulable != owner.Schedulable ||
		!reflect.DeepEqual(r.current.TempUnschedulableUntil, owner.TempUnschedulableUntil) || r.current.TempUnschedulableReason != owner.TempUnschedulableReason {
		return false, nil
	}
	r.current.TempUnschedulableUntil = until
	r.current.TempUnschedulableReason = reason
	return true, nil
}

func TestNativeWalletSchedulingRejectsChangesAfterObservationCommit(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		for _, mutation := range []string{"login", "other cooldown", "manual scheduling"} {
			t.Run(fmt.Sprintf("healthy=%t/%s", healthy, mutation), func(t *testing.T) {
				a := nativeControlAccount(PlatformDeepseek)
				a.Status, a.Schedulable = StatusActive, true
				if healthy {
					until := time.Now().Add(time.Hour)
					a.TempUnschedulableUntil = &until
					a.TempUnschedulableReason = cnBalanceLowReason("previous balance")
				}
				repo := &nativeBalanceScheduleRepo{current: a}
				repo.afterObservation = func() {
					replacement := snapshotOAuthRefreshAccount(a)
					switch mutation {
					case "login":
						replacement.Credentials["access_token"] = "replacement-login"
					case "other cooldown":
						until := time.Now().Add(2 * time.Hour)
						replacement.TempUnschedulableUntil = &until
						replacement.TempUnschedulableReason = "transport failure"
					case "manual scheduling":
						replacement.Schedulable = false
					}
					repo.current = replacement
				}
				balance := "0"
				if healthy {
					balance = "50"
				}
				upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
					return nativeControlReply(fmt.Sprintf(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":%q}],"bonus_wallets":[]}}}`, balance)), nil
				}}
				svc := &CNProviderBalanceCheckService{accountRepo: repo, balanceService: NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}), cfg: &config.Config{}}
				outcome := svc.checkOne(context.Background(), a, 5)
				require.Equal(t, cnBalanceNoChange, outcome, "a committed wallet observation cannot mutate changed owner/scheduler state")
				require.Zero(t, repo.legacyWrites, "native scheduling must never use an unconditional account-ID write")
				if mutation == "other cooldown" {
					require.Equal(t, "transport failure", repo.current.TempUnschedulableReason)
				}
				if mutation == "login" {
					require.Equal(t, "replacement-login", repo.current.GetCredential("access_token"))
					require.Equal(t, a.TempUnschedulableReason, repo.current.TempUnschedulableReason)
					require.Equal(t, a.TempUnschedulableUntil, repo.current.TempUnschedulableUntil)
				}
			})
		}
	}
}

func TestNativeWalletSchedulingAppliesOnlyCurrentOwnersObservation(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		t.Run(fmt.Sprintf("healthy=%t", healthy), func(t *testing.T) {
			a := nativeControlAccount(PlatformDeepseek)
			a.Status, a.Schedulable = StatusActive, true
			if healthy {
				until := time.Now().Add(time.Hour)
				a.TempUnschedulableUntil = &until
				a.TempUnschedulableReason = cnBalanceLowReason("previous balance")
			}
			repo := &nativeBalanceScheduleRepo{current: a}
			balance := "0"
			if healthy {
				balance = "50"
			}
			upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
				return nativeControlReply(fmt.Sprintf(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":%q}],"bonus_wallets":[]}}}`, balance)), nil
			}}
			svc := &CNProviderBalanceCheckService{accountRepo: repo, balanceService: NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}), cfg: &config.Config{}}
			expected := cnBalancePaused
			if healthy {
				expected = cnBalanceCleared
			}
			require.Equal(t, expected, svc.checkOne(context.Background(), a, 5))
			require.Zero(t, repo.legacyWrites)
			require.Equal(t, 1, repo.conditionalWrites)
			if healthy {
				require.Nil(t, repo.current.TempUnschedulableUntil)
			} else {
				require.NotNil(t, repo.current.TempUnschedulableUntil)
				require.Contains(t, repo.current.TempUnschedulableReason, cnBalanceLowReasonPrefix)
			}
		})
	}
}

func TestNativeWalletSchedulingUsesQueriedOwnerInsteadOfStaleListing(t *testing.T) {
	listed := nativeControlAccount(PlatformDeepseek)
	listed.Status, listed.Schedulable = StatusActive, false
	current := snapshotOAuthRefreshAccount(listed)
	current.Credentials["access_token"] = "current-wallet-owner"
	current.Schedulable = true
	repo := &nativeBalanceScheduleRepo{current: current}
	upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "current-wallet-owner", req.Header.Get("X-Dsh-Auth-Token"))
		return nativeControlReply(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"0"}],"bonus_wallets":[]}}}`), nil
	}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, balanceService: NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}), cfg: &config.Config{}}
	require.Equal(t, cnBalancePaused, svc.checkOne(context.Background(), listed, 5), "decision and compare-and-set must use the same owner that actually queried the wallet")
	require.Zero(t, repo.legacyWrites)
	require.Equal(t, 1, repo.conditionalWrites)
	require.Equal(t, "current-wallet-owner", repo.current.GetCredential("access_token"))
	require.NotNil(t, repo.current.TempUnschedulableUntil)
}

func TestNativeWalletSchedulingKeepsCooldownValueFrozenDuringQuery(t *testing.T) {
	a := nativeControlAccount(PlatformDeepseek)
	a.Status, a.Schedulable = StatusActive, true
	until := time.Now().Add(time.Hour)
	a.TempUnschedulableUntil = &until
	a.TempUnschedulableReason = cnBalanceLowReason("previous balance")
	repo := &nativeBalanceScheduleRepo{current: a}
	repo.afterObservation = func() { *a.TempUnschedulableUntil = time.Now().Add(2 * time.Hour) }
	upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
		return nativeControlReply(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"50"}],"bonus_wallets":[]}}}`), nil
	}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, balanceService: NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}), cfg: &config.Config{}}
	require.Equal(t, cnBalanceNoChange, svc.checkOne(context.Background(), a, 5), "a cooldown renewed in place after the observation must not be cleared")
	require.NotNil(t, repo.current.TempUnschedulableUntil)
}
