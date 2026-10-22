package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

func grokCredentialGeneration(account *Account) string {
	return credentialStateGeneration(account)
}

func grokAccountBillingSnapshot(account *Account) (*xai.BillingSummary, error) {
	if account == nil {
		return nil, nil
	}
	snapshot, err := grokBillingSnapshotFromExtra(account.Extra)
	if err != nil || snapshot == nil {
		return snapshot, err
	}
	if snapshot.CredentialGeneration == "" || snapshot.CredentialGeneration != grokCredentialGeneration(account) {
		return nil, nil
	}
	return snapshot, nil
}

func grokAccountQuotaSnapshot(account *Account) (*xai.QuotaSnapshot, error) {
	if account == nil {
		return nil, nil
	}
	snapshot, err := grokQuotaSnapshotFromExtra(account.Extra)
	if err != nil || snapshot == nil {
		return snapshot, err
	}
	if snapshot.CredentialGeneration == "" || snapshot.CredentialGeneration != grokCredentialGeneration(account) {
		return nil, nil
	}
	return snapshot, nil
}

func grokResponseOwner(ctx context.Context, account *Account) *Account {
	owner, _ := credentialStateMutationOwner(ctx, account)
	return owner
}

func applyGrokCredentialMutation(ctx context.Context, repository AccountRepository, owner *Account, mutation CredentialStateMutation) (bool, error) {
	repo, ok := repository.(CredentialStateMutationRepository)
	if !ok || owner == nil {
		return false, errors.New("grok conditional account state unavailable")
	}
	applied, err := repo.ApplyCredentialStateMutation(ctx, owner, mutation)
	if err != nil {
		slog.Warn("grok_credential_state_write_failed", "account_id", owner.ID, "error", err)
	}
	if err == nil && !applied {
		slog.Info("grok_credential_state_superseded", "account_id", owner.ID)
	}
	return applied, err
}
