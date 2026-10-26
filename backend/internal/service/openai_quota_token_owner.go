package service

import (
	"context"
	"errors"
)

var errOpenAIQuotaTokenOwnerChanged = errors.New("quota token credential owner changed or is unavailable")

// OpenAIQuotaCredentialSnapshotRepository atomically binds a quota token or
// Agent task rotation to its complete credential owner and proxy route.
type OpenAIQuotaCredentialSnapshotRepository interface {
	UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error)
}

func persistOpenAIQuotaCredentials(ctx context.Context, repo AccountRepository, expected *Account, credentials map[string]any) (bool, error) {
	writer, ok := repo.(OpenAIQuotaCredentialSnapshotRepository)
	if !ok {
		return false, errors.New("atomic quota credential persistence is unavailable")
	}
	owner, err := snapshotCNControlAccount(expected)
	if err != nil {
		return false, err
	}
	return writer.UpdateOpenAIQuotaCredentialsIfUnchanged(ctx, owner, credentials)
}

type openAIQuotaTokenOwnerKey struct{}

// This guard belongs to a single quota operation. Only a rotation issued for
// its captured grant may advance the complete credential fingerprint.
type openAIQuotaTokenOwner struct {
	fingerprint         string
	identityFingerprint string
}

func withOpenAIQuotaTokenOwner(ctx context.Context, account *Account) (context.Context, error) {
	fingerprint, err := cnControlOwnerFingerprint(account)
	if err != nil {
		return ctx, err
	}
	identityFingerprint, err := openAIQuotaTokenIdentityFingerprint(account)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, openAIQuotaTokenOwnerKey{}, &openAIQuotaTokenOwner{
		fingerprint: fingerprint, identityFingerprint: identityFingerprint,
	}), nil
}

func openAIQuotaTokenOwnerFromContext(ctx context.Context) *openAIQuotaTokenOwner {
	owner, _ := ctx.Value(openAIQuotaTokenOwnerKey{}).(*openAIQuotaTokenOwner)
	return owner
}

func (o *openAIQuotaTokenOwner) validate(account *Account) error {
	fingerprint, err := cnControlOwnerFingerprint(account)
	if err != nil || fingerprint != o.fingerprint {
		return errOpenAIQuotaTokenOwnerChanged
	}
	return nil
}

func (o *openAIQuotaTokenOwner) validateRepository(ctx context.Context, repo AccountRepository, id int64) error {
	if repo == nil {
		return errOpenAIQuotaTokenOwnerChanged
	}
	account, err := repo.GetByID(ctx, id)
	if err != nil {
		return errOpenAIQuotaTokenOwnerChanged
	}
	return o.validate(account)
}

func (o *openAIQuotaTokenOwner) validateRotation(account *Account) error {
	fingerprint, err := openAIQuotaTokenIdentityFingerprint(account)
	if err != nil || fingerprint != o.identityFingerprint {
		return errOpenAIQuotaTokenOwnerChanged
	}
	return nil
}

func (o *openAIQuotaTokenOwner) acceptRotation(account *Account) error {
	if err := o.validateRotation(account); err != nil {
		return err
	}
	fingerprint, err := cnControlOwnerFingerprint(account)
	if err != nil {
		return errOpenAIQuotaTokenOwnerChanged
	}
	o.fingerprint = fingerprint
	return nil
}

func openAIQuotaTokenIdentityFingerprint(account *Account) (string, error) {
	owner, err := snapshotCNControlAccount(account)
	if err != nil {
		return "", err
	}
	// The issuer can rotate tokens and refresh subscription/profile metadata.
	// The effective ChatGPT owner, client identity and route remain fixed.
	accountID := owner.GetCredential("chatgpt_account_id")
	if accountID == "" {
		accountID = owner.GetCredential("organization_id")
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "expires_at", "_token_version", "email", "chatgpt_user_id", "organization_id", "plan_type", "subscription_expires_at"} {
		delete(owner.Credentials, key)
	}
	owner.Credentials["chatgpt_account_id"] = accountID
	return cnControlOwnerFingerprint(owner)
}
