package service

import (
	"context"
	"log/slog"
)

// A successful refresh proves the new token works; it does not authorize
// deleting a newer cooldown, replacing a later login or clearing other grants.
// The runtime snapshot is captured before the database CAS and can be removed
// only if its own sequence and deadline also remain unchanged.
type credentialRefreshRuntimeCleanup interface {
	peekOpenAIAccountRuntimeBlock(*Account) openAIAccountRuntimeBlockSnapshot
	clearOpenAIAccountRuntimeBlockIfUnchanged(int64, openAIAccountRuntimeBlockSnapshot)
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
	var runtimeSnapshot openAIAccountRuntimeBlockSnapshot
	runtime, canClearRuntime := s.runtimeBlocker.(credentialRefreshRuntimeCleanup)
	if canClearRuntime {
		runtimeSnapshot = runtime.peekOpenAIAccountRuntimeBlock(owner)
	}
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
	if canClearRuntime {
		runtime.clearOpenAIAccountRuntimeBlockIfUnchanged(owner.ID, runtimeSnapshot)
	}
	// TempUnschedCache is an admin display cache keyed only by row ID. The
	// fenced providers read that display from the authoritative row, so cleanup
	// neither deletes nor republishes a possibly newer grant's cache entry.
}
