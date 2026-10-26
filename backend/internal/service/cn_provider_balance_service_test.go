//go:build unit || !integration

package service

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type cnBalanceResponseUpstream struct {
	statusCode int
	body       string
}

func (u *cnBalanceResponseUpstream) Do(
	_ *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	return &http.Response{
		StatusCode: u.statusCode,
		Body:       io.NopCloser(strings.NewReader(u.body)),
		Header:     make(http.Header),
	}, nil
}

func (u *cnBalanceResponseUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type cnBalanceProbeRepo struct {
	AccountRepository
	mu                      sync.Mutex
	account                 *Account
	extraWrites             []map[string]any
	stateWrites             []CredentialStateMutation
	unconditionalStateCalls int
	unconditionalExtraCalls int
}

func (r *cnBalanceProbeRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotRequestCredentialAccount(r.account), nil
}

func (r *cnBalanceProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unconditionalExtraCalls++
	r.extraWrites = append(r.extraWrites, updates)
	return nil
}

func (r *cnBalanceProbeRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unconditionalStateCalls++
	r.account.TempUnschedulableUntil, r.account.TempUnschedulableReason = &until, reason
	return nil
}

func (r *cnBalanceProbeRepo) ClearTempUnschedulable(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unconditionalStateCalls++
	r.account.TempUnschedulableUntil, r.account.TempUnschedulableReason = nil, ""
	return nil
}

func (r *cnBalanceProbeRepo) ApplyCredentialStateMutation(_ context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.account
	if current == nil || owner == nil || current.ID != owner.ID || current.Platform != owner.Platform || current.Type != owner.Type || current.Status != owner.Status ||
		!reflect.DeepEqual(current.Credentials, owner.Credentials) || !reflect.DeepEqual(current.ProxyID, owner.ProxyID) || !reflect.DeepEqual(current.Proxy, owner.Proxy) {
		return false, nil
	}
	if mutation.RequireTempUnschedulableUnchanged || mutation.ClearTempUnschedulable {
		if current.TempUnschedulableUntil == nil || mutation.ObservedTempUntil == nil {
			if current.TempUnschedulableUntil != nil || mutation.ObservedTempUntil != nil {
				return false, nil
			}
		} else if !current.TempUnschedulableUntil.Equal(*mutation.ObservedTempUntil) {
			return false, nil
		}
		if current.TempUnschedulableReason != mutation.ObservedTempReason {
			return false, nil
		}
	}
	if len(mutation.Extra) > 0 {
		r.extraWrites = append(r.extraWrites, cloneRequestCredentialMap(mutation.Extra))
		if current.Extra == nil {
			current.Extra = map[string]any{}
		}
		for key, value := range mutation.Extra {
			current.Extra[key] = value
		}
	}
	if mutation.TempUnschedulableUntil != nil {
		until := *mutation.TempUnschedulableUntil
		current.TempUnschedulableUntil, current.TempUnschedulableReason = &until, mutation.TempUnschedulableReason
	}
	if mutation.ClearTempUnschedulable {
		current.TempUnschedulableUntil, current.TempUnschedulableReason = nil, ""
	}
	r.stateWrites = append(r.stateWrites, mutation)
	return true, nil
}

func (r *cnBalanceProbeRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	return r.ApplyCredentialStateMutation(ctx, expected, CredentialStateMutation{Extra: updates,
		RequireTempUnschedulableUnchanged: true, ObservedTempUntil: expected.TempUnschedulableUntil, ObservedTempReason: expected.TempUnschedulableReason})
}

func newDeepSeekBalanceProbeAccount() *Account {
	return &Account{
		ID:       42,
		Platform: PlatformDeepseek,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"account_mode": AccountModePayG,
			"api_key":      "sk-test",
			"base_url":     "https://relay.example.com",
		},
	}
}

func TestCNProviderBalanceService_DeepSeekInvalidBalancePayloadDoesNotBecomeZero(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name:      "missing balance infos",
			body:      `{"data":{"models":["deepseek-v4-flash"]}}`,
			wantError: "missing balance_infos",
		},
		{
			name:      "empty balance infos",
			body:      `{"is_available":true,"balance_infos":[]}`,
			wantError: "no valid balance entries",
		},
		{
			name:      "invalid balance value",
			body:      `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"not-a-number"}]}`,
			wantError: "no valid balance entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &cnBalanceProbeRepo{account: newDeepSeekBalanceProbeAccount()}
			upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: tt.body}
			svc := NewCNProviderBalanceService(repo, nil, upstream, nil)

			result, err := svc.QueryBalance(context.Background(), repo.account.ID)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.Success)
			require.Contains(t, result.Error, tt.wantError)
			require.Empty(t, result.Balances)
			require.Empty(t, repo.extraWrites, "invalid relay balance payload must not persist a synthetic zero balance")
		})
	}
}

func TestCNProviderBalanceService_DeepSeekValidZeroBalanceRemainsSuccessful(t *testing.T) {
	repo := &cnBalanceProbeRepo{account: newDeepSeekBalanceProbeAccount()}
	upstream := &cnBalanceResponseUpstream{
		statusCode: http.StatusOK,
		body:       `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`,
	}
	svc := NewCNProviderBalanceService(repo, nil, upstream, nil)

	result, err := svc.QueryBalance(context.Background(), repo.account.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success)
	require.False(t, result.Available)
	require.Equal(t, "CNY", result.Currency)
	require.Zero(t, result.Balance)
	require.Len(t, result.Balances, 1)
	require.Len(t, repo.extraWrites, 1, "a valid upstream zero balance must still be persisted")
}

func (r *cnBalanceProbeRepo) UpdateCNBalanceScheduleIfUnchanged(ctx context.Context, owner *Account, until *time.Time, reason string) (bool, error) {
	r.mu.Lock()
	unchanged := r.account != nil && owner != nil && r.account.Schedulable == owner.Schedulable
	r.mu.Unlock()
	if !unchanged {
		return false, nil
	}
	mutation := CredentialStateMutation{RequireTempUnschedulableUnchanged: true, ObservedTempUntil: owner.TempUnschedulableUntil, ObservedTempReason: owner.TempUnschedulableReason,
		TempUnschedulableUntil: until, TempUnschedulableReason: reason, ClearTempUnschedulable: until == nil}
	return r.ApplyCredentialStateMutation(ctx, owner, mutation)
}
