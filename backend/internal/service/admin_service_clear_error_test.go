//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountRepoStubForClearAccountError struct {
	mockAccountRepoForGemini
	account                  *Account
	clearErrorCalls          int
	clearRateLimitCalls      int
	clearAntigravityCalls    int
	clearModelRateLimitCalls int
	clearTempUnschedCalls    int
	recoveryCalls            int
}

func (r *accountRepoStubForClearAccountError) RunOpenAIOAuthPolicyTransaction(ctx context.Context, fn func(context.Context, AccountRepository) error) error {
	return fn(ctx, r)
}

func (r *accountRepoStubForClearAccountError) ClearAccountRecoveryState(context.Context, int64) error {
	r.recoveryCalls++
	r.account.Status, r.account.ErrorMessage = StatusActive, ""
	r.account.RateLimitedAt, r.account.RateLimitResetAt, r.account.OverloadUntil = nil, nil, nil
	r.account.TempUnschedulableUntil, r.account.TempUnschedulableReason = nil, ""
	delete(r.account.Extra, "model_rate_limits")
	delete(r.account.Extra, "antigravity_quota_scopes")
	return nil
}

func (r *accountRepoStubForClearAccountError) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.account, nil
}

func (r *accountRepoStubForClearAccountError) ClearError(ctx context.Context, id int64) error {
	r.clearErrorCalls++
	r.account.Status = StatusActive
	r.account.ErrorMessage = ""
	return nil
}

func (r *accountRepoStubForClearAccountError) ClearRateLimit(ctx context.Context, id int64) error {
	r.clearRateLimitCalls++
	r.account.RateLimitedAt = nil
	r.account.RateLimitResetAt = nil
	return nil
}

func (r *accountRepoStubForClearAccountError) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	r.clearAntigravityCalls++
	return nil
}

func (r *accountRepoStubForClearAccountError) ClearModelRateLimits(ctx context.Context, id int64) error {
	r.clearModelRateLimitCalls++
	return nil
}

func (r *accountRepoStubForClearAccountError) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempUnschedCalls++
	r.account.TempUnschedulableUntil = nil
	r.account.TempUnschedulableReason = ""
	return nil
}

func TestAdminService_ClearAccountError_AlsoClearsRecoverableRuntimeState(t *testing.T) {
	until := time.Now().Add(10 * time.Minute)
	resetAt := time.Now().Add(5 * time.Minute)
	repo := &accountRepoStubForClearAccountError{
		account: &Account{
			ID:                      31,
			Platform:                PlatformOpenAI,
			Type:                    AccountTypeOAuth,
			Status:                  StatusError,
			ErrorMessage:            "refresh failed",
			RateLimitResetAt:        &resetAt,
			OverloadUntil:           &until,
			TempUnschedulableUntil:  &until,
			TempUnschedulableReason: "missing refresh token",
			Extra:                   map[string]any{"retained": true, "model_rate_limits": map[string]any{"old": true}, "antigravity_quota_scopes": map[string]any{"old": true}},
		},
	}
	blocker := &runtimeBlockRecorder{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: blocker}

	updated, err := svc.ClearAccountError(context.Background(), 31)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.recoveryCalls)
	require.Zero(t, repo.clearErrorCalls+repo.clearRateLimitCalls+repo.clearAntigravityCalls+repo.clearModelRateLimitCalls+repo.clearTempUnschedCalls, "recovery must use the atomic state writer")
	require.Equal(t, StatusActive, updated.Status)
	require.Empty(t, updated.ErrorMessage)
	require.Nil(t, updated.RateLimitResetAt)
	require.Nil(t, updated.OverloadUntil)
	require.Nil(t, updated.TempUnschedulableUntil)
	require.Empty(t, updated.TempUnschedulableReason)
	require.Equal(t, true, updated.Extra["retained"])
	require.NotContains(t, updated.Extra, "model_rate_limits")
	require.NotContains(t, updated.Extra, "antigravity_quota_scopes")
	require.Equal(t, []int64{31}, blocker.clearedIDs)
}
