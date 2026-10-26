package service

import (
	"context"
	"log/slog"
	"net/http"
)

// A connection probe can finish after the operator has replaced credentials.
// Its result can quarantine only the grant that was actually transmitted.
func (s *AccountTestService) setProbeCredentialError(ctx context.Context, selected *Account, sent *http.Request, message string) {
	if s == nil || s.accountRepo == nil || selected == nil {
		return
	}
	if !usesHTTP401CredentialFence(selected) {
		_ = s.accountRepo.SetError(ctx, selected.ID, message)
		return
	}
	owner, known := credentialStateMutationOwner(ctx, selected)
	if sent != nil {
		if evidence, ok := sent.Context().Value(requestCredentialTransportKey{}).(requestCredentialTransportEvidence); ok {
			owner, known = evidence.owner, evidence.owner != nil
		} else if requestCredentialOwnerBound(sent.Context(), selected) {
			owner, known = requestCredentialOwner(sent.Context(), selected)
		}
	}
	if !known {
		slog.Warn("probe_credential_snapshot_unavailable", "account_id", selected.ID)
		return
	}
	repo, ok := s.accountRepo.(credentialFailureRepository)
	if !ok {
		slog.Warn("probe_credential_state_unavailable", "account_id", owner.ID)
		return
	}
	if _, err := repo.SetCredentialErrorIfUnchanged(ctx, owner, message); err != nil {
		slog.Warn("probe_credential_state_failed", "account_id", owner.ID, "error", err)
	}
}
