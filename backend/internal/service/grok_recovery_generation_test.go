//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokOutcomePublicationUsesActualCommitRecoveryGeneration(t *testing.T) {
	for _, operation := range []string{"account-cooldown", "model-capacity"} {
		for _, order := range []string{"unchanged", "reset-before-commit", "remote-reset-after-commit", "storage-failure"} {
			t.Run(operation+"/"+order, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
				account.ID = time.Now().UnixNano()
				account.Extra = map[string]any{"runtime_recovery_generation": "before-reset"}
				owner, err := CaptureOAuthCredentialOwner(account)
				require.NoError(t, err)
				repo := &grokOutcomeOwnerRepo{current: account, failStorage: order == "storage-failure"}
				if order == "reset-before-commit" {
					account.Extra["runtime_recovery_generation"] = "after-reset"
				}
				if order == "remote-reset-after-commit" {
					repo.afterApplied = func() {
						account.Extra["runtime_recovery_generation"] = "after-reset"
						account.TempUnschedulableUntil, account.TempUnschedulableReason = nil, ""
						account.RateLimitedAt, account.RateLimitResetAt = nil, nil
					}
				}
				gateway := &OpenAIGatewayService{accountRepo: repo}
				status, body := http.StatusUnauthorized, []byte(`{"error":{"message":"isolated auth failure"}}`)
				if operation == "model-capacity" {
					status, body = http.StatusTooManyRequests, []byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`)
				}
				gateway.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), owner, status, nil, body)
				wantBlocked := order == "unchanged" || order == "reset-before-commit"
				if operation == "account-cooldown" {
					require.Equal(t, wantBlocked, gateway.isOpenAIAccountRuntimeBlocked(account), "a response committed after reset remains new evidence; an earlier committed observation cannot block the later reset generation")
				} else {
					require.Equal(t, wantBlocked, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()))
				}
			})
		}
	}
}

func TestGrokModelRecoveryKeepsNewGenerationFailureAndPrunesExpiredHistory(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	account.ID = time.Now().UnixNano()
	account.Extra = map[string]any{"runtime_recovery_generation": "old-generation"}
	old, err := CaptureOAuthCredentialOwner(account)
	require.NoError(t, err)
	markGrokModelTransientBlock(old, "grok-4.5", time.Now().Add(time.Minute))
	account.Extra["runtime_recovery_generation"] = "new-generation"
	require.False(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()), "recovery must isolate a committed historical failure without changing credentials")
	markGrokModelTransientBlock(account, "grok-4.5", time.Now().Add(2*time.Minute))
	require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()), "the recovered generation must still block its own newly observed failure")
	globalGrokModelQuotaBlocks.mu.Lock()
	oldKey := grokModelQuotaBlockKey(old, "grok-4.5")
	history := globalGrokModelQuotaBlocks.items[oldKey]
	history.Until = time.Now().Add(-time.Second)
	globalGrokModelQuotaBlocks.items[oldKey] = history
	globalGrokModelQuotaBlocks.mu.Unlock()
	require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()))
	globalGrokModelQuotaBlocks.mu.Lock()
	_, retained := globalGrokModelQuotaBlocks.items[oldKey]
	globalGrokModelQuotaBlocks.mu.Unlock()
	require.False(t, retained, "current-generation reads reclaim expired history without removing the active new failure")
}

func TestGrokOutcomePublicationRetainsOwnerWhenRepositoryMutatesCaller(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	account.ID = time.Now().UnixNano()
	account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "committed-generation"}
	old, err := CaptureOAuthCredentialOwner(account)
	require.NoError(t, err)
	repo := &grokOutcomeOwnerRepo{current: account}
	repo.afterApplied = func() {
		account.Credentials["api_key"] = "replacement-after-commit"
		account.Extra[RuntimeRecoveryGenerationExtraKey] = "replacement-generation"
	}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	gateway.handleGrokAccountUpstreamError(context.Background(), account, http.StatusUnauthorized, nil, []byte(`{"error":{"message":"isolated auth failure"}}`))
	require.True(t, gateway.isOpenAIAccountRuntimeBlocked(old), "the observation belongs to the credentials captured before the persistence call")
	require.False(t, gateway.isOpenAIAccountRuntimeBlocked(account), "a shared repository return object cannot lend replacement credentials to an earlier observation")
	require.Equal(t, "replacement-generation", account.Extra[RuntimeRecoveryGenerationExtraKey], "committed generation is attached only to the publication snapshot, never to the caller map")
	require.Equal(t, "replacement-after-commit", account.GetCredential("api_key"))
}
