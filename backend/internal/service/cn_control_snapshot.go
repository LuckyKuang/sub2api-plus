package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// CNControlSnapshotRepository atomically keeps a control-plane observation on
// the credential owner that produced it. A missing implementation fails closed.
type CNControlSnapshotRepository interface {
	UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error)
}

// CNOAuthCredentialSnapshotRepository binds a provider-issued rotation to the
// complete native grant that was sent to the issuer.
type CNOAuthCredentialSnapshotRepository interface {
	UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error)
}

// CNOAuthLoginSnapshotRepository commits reauthorization and clears its old
// credential errors and cooldowns in the same owner-conditional transaction.
type CNOAuthLoginSnapshotRepository interface {
	CompleteCNOAuthLoginIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error)
}

func snapshotCNControlAccount(account *Account) (*Account, error) {
	if account == nil {
		return nil, errors.New("control account is missing")
	}
	snapshot := snapshotOAuthRefreshAccount(account)
	credentials, err := cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return nil, errors.New("control account credentials are invalid")
	}
	snapshot.Credentials = credentials
	snapshot.TempUnschedulableUntil = cloneTimePtr(account.TempUnschedulableUntil)
	snapshot.RateLimitedAt = cloneTimePtr(account.RateLimitedAt)
	snapshot.RateLimitResetAt = cloneTimePtr(account.RateLimitResetAt)
	snapshot.OverloadUntil = cloneTimePtr(account.OverloadUntil)
	snapshot.ExpiresAt = cloneTimePtr(account.ExpiresAt)
	if account.ProxyID == nil {
		// Explicit saved unbinding is authoritative over a residual hydrated
		// association. Preserve this direct route in flights and CAS evidence.
		snapshot.Proxy = nil
	} else if account.Proxy != nil {
		proxy := *account.Proxy
		proxy.ExpiresAt = cloneTimePtr(account.Proxy.ExpiresAt)
		snapshot.Proxy = &proxy
	}
	return snapshot, nil
}

func cnControlOwnerFingerprint(account *Account) (string, error) {
	if account == nil {
		return "", errors.New("control account is missing")
	}
	proxyURL, err := account.StrictProxyURL()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		ID          int64
		Platform    string
		Type        string
		Credentials map[string]any
		ProxyID     *int64
		ProxyURL    string
	}{account.ID, account.Platform, account.Type, account.Credentials, account.ProxyID, proxyURL})
	if err != nil {
		return "", errors.New("control account credentials are invalid")
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func persistCNControlExtra(ctx context.Context, repo AccountRepository, expected *Account, updates map[string]any) (bool, error) {
	writer, ok := repo.(CNControlSnapshotRepository)
	if !ok {
		return false, errors.New("atomic control snapshot persistence is unavailable")
	}
	return writer.UpdateCNControlExtraIfUnchanged(ctx, expected, updates)
}

type cnControlRequestOwnerKey struct{}

func withCNControlRequestOwner(ctx context.Context, account *Account) (context.Context, error) {
	snapshot, err := snapshotCNControlAccount(account)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, cnControlRequestOwnerKey{}, snapshot), nil
}

func cnControlRequestOwner(reqCtx context.Context, fallback *Account) *Account {
	if owner, ok := reqCtx.Value(cnControlRequestOwnerKey{}).(*Account); ok {
		return owner
	}
	return fallback
}
