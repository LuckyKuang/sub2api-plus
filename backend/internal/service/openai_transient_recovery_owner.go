package service

import (
	"context"
	"log/slog"
)

// A reset does not cancel an upstream request. New API-key failure evidence
// belongs to the recovery state read after that response, provided the exact
// credential owner and saved route still match the request that failed.
func (s *OpenAIGatewayService) openAIAPIKeyTransientObservationOwner(ctx context.Context, account *Account) *Account {
	if s == nil || s.accountRepo == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil
	}
	expected, err := cnControlOwnerFingerprint(account)
	if err != nil {
		return nil
	}
	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || current == nil {
		slog.Warn("openai_model_transient_observation_not_recorded", "account_id", account.ID, "reason", "owner_read_failed")
		return nil
	}
	owner, err := CaptureOAuthCredentialOwner(current)
	if err != nil {
		return nil
	}
	actual, err := cnControlOwnerFingerprint(owner)
	if err != nil || actual != expected {
		slog.Warn("openai_model_transient_observation_not_recorded", "account_id", account.ID, "reason", "credential_or_proxy_owner_changed")
		return nil
	}
	return owner
}
