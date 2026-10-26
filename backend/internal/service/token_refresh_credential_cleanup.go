package service

import (
	"context"
	"log/slog"
)

// A successful refresh proves the new token works; it does not authorize
// deleting a newer cooldown, replacing a later login or clearing other grants.
// The runtime snapshot is captured before the refresh attempt and can be removed
// only if its own sequence and deadline also remain unchanged.
type credentialRefreshRuntimeCleanup interface {
	peekOpenAIAccountRuntimeBlock(*Account) openAIAccountRuntimeBlockSnapshot
	clearOpenAIAccountRuntimeBlockIfUnchanged(int64, openAIAccountRuntimeBlockSnapshot)
}

type credentialRefreshRuntimeSnapshotKey struct{}

func (s *TokenRefreshService) withCredentialRefreshRuntimeSnapshot(ctx context.Context, account *Account) context.Context {
	var snapshot openAIAccountRuntimeBlockSnapshot
	if runtime, ok := s.runtimeBlocker.(credentialRefreshRuntimeCleanup); ok {
		snapshot = runtime.peekOpenAIAccountRuntimeBlock(account)
	}
	return context.WithValue(ctx, credentialRefreshRuntimeSnapshotKey{}, snapshot)
}

func (s *TokenRefreshService) clearRefreshedCredentialCooldown(ctx context.Context, account *Account) {
	owner, known := credentialStateMutationOwner(ctx, account)
	if !known || owner.TempUnschedulableUntil == nil {
		return
	}
	repo, ok := s.accountRepo.(CredentialStateMutationRepository)
	if !ok {
		slog.Warn("token_refresh.conditional_cleanup_unavailable", "account_id", owner.ID)
		return
	}
	runtime, canClearRuntime := s.runtimeBlocker.(credentialRefreshRuntimeCleanup)
	runtimeSnapshot, observedRuntime := ctx.Value(credentialRefreshRuntimeSnapshotKey{}).(openAIAccountRuntimeBlockSnapshot)
	applied, err := repo.ApplyCredentialStateMutation(ctx, owner, CredentialStateMutation{
		ClearTempUnschedulable: true,
		ObservedTempUntil:      owner.TempUnschedulableUntil,
		ObservedTempReason:     owner.TempUnschedulableReason,
	})
	if err != nil {
		slog.Warn("token_refresh.conditional_cleanup_failed", "account_id", owner.ID, "error", err)
		return
	}
	if !applied {
		slog.Info("token_refresh.conditional_cleanup_superseded", "account_id", owner.ID)
		return
	}
	if canClearRuntime && observedRuntime {
		runtime.clearOpenAIAccountRuntimeBlockIfUnchanged(owner.ID, runtimeSnapshot)
	}
	// TempUnschedCache is an admin display cache keyed only by row ID. The
	// fenced providers read that display from the authoritative row, so cleanup
	// neither deletes nor republishes a possibly newer grant's cache entry.
}
