//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokRecoveryCommitOrderRepo struct {
	*accountRepoStubForClearAccountError
	afterCommit func()
}

func (r *grokRecoveryCommitOrderRepo) RunOpenAIOAuthPolicyTransaction(ctx context.Context, fn func(context.Context, AccountRepository) error) error {
	if err := fn(ctx, r); err != nil {
		return err
	}
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return nil
}

func (r *grokRecoveryCommitOrderRepo) ClearAccountRecoveryState(ctx context.Context, id int64) error {
	if err := r.accountRepoStubForClearAccountError.ClearAccountRecoveryState(ctx, id); err != nil {
		return err
	}
	r.account.Extra[RuntimeRecoveryGenerationExtraKey] = "committed-recovery-generation"
	return nil
}

func TestGrokAdminRecoveryKeepsNewFailureCommittedBeforeLocalCleanup(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	account.ID = time.Now().UnixNano()
	account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "pre-recovery-generation"}
	old, err := CaptureOAuthCredentialOwner(account)
	require.NoError(t, err)
	markGrokModelTransientBlock(old, "grok-4.5", time.Now().Add(time.Minute))
	outcomes := &grokOutcomeOwnerRepo{current: account}
	gateway := &OpenAIGatewayService{accountRepo: outcomes}
	repo := &grokRecoveryCommitOrderRepo{accountRepoStubForClearAccountError: &accountRepoStubForClearAccountError{account: account}}
	repo.afterCommit = func() {
		owner, err := CaptureOAuthCredentialOwner(account)
		require.NoError(t, err)
		gateway.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), owner, http.StatusTooManyRequests, nil, []byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`))
		require.Equal(t, 1, outcomes.atomicWrites)
		require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()), "a response committed after reset is a new failure in the saved recovery generation")
	}
	admin := &adminServiceImpl{accountRepo: repo, runtimeBlocker: gateway}
	updated, err := admin.ClearAccountError(context.Background(), account.ID)
	require.NoError(t, err)
	require.False(t, isGrokModelQuotaBlocked(old, "grok-4.5", time.Now()), "recovery still retires historical local evidence")
	require.True(t, isGrokModelQuotaBlocked(updated, "grok-4.5", time.Now()), "the reset's delayed local cleanup cannot erase a new failure committed after that reset")
}

type grokRecoveryOutcomeCommitOrderRepo struct {
	*grokOutcomeOwnerRepo
	beforeCommit func()
}

func (r *grokRecoveryOutcomeCommitOrderRepo) ApplyGrokRequestOutcomeIfUnchanged(ctx context.Context, owner *Account, outcome GrokRequestOutcome) (GrokRequestOutcomeResult, error) {
	if r.beforeCommit != nil {
		r.beforeCommit()
	}
	return r.grokOutcomeOwnerRepo.ApplyGrokRequestOutcomeIfUnchanged(ctx, owner, outcome)
}

func TestGrokOutcomeCommittedAfterRecoveryKeepsItsNewGenerationDespiteEarlierEpoch(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	account.ID = time.Now().UnixNano()
	account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "pre-recovery-generation"}
	owner, err := CaptureOAuthCredentialOwner(account)
	require.NoError(t, err)
	repo := &grokRecoveryOutcomeCommitOrderRepo{grokOutcomeOwnerRepo: &grokOutcomeOwnerRepo{current: account}}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	resetRepo := &grokRecoveryCommitOrderRepo{accountRepoStubForClearAccountError: &accountRepoStubForClearAccountError{account: account}}
	admin := &adminServiceImpl{accountRepo: resetRepo, runtimeBlocker: gateway}
	repo.beforeCommit = func() {
		_, err := admin.ClearAccountError(context.Background(), account.ID)
		require.NoError(t, err)
	}
	gateway.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), owner, http.StatusTooManyRequests, nil, []byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`))
	require.Equal(t, 1, repo.atomicWrites, "the actual failure commits after administrator recovery")
	require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()), "a request started before reset still owns a new response committed after reset; the older in-process epoch cannot cancel it")
}
