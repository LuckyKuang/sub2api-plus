package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// CredentialStateMutation is evidence from one immutable transmitted grant.
// A repository applies the whole mutation and its scheduler outbox atomically.
// A false result must never be followed by an unconditional write/notification.
type CredentialStateMutation struct {
	Extra map[string]any
	// OpenAIPlanType is provider-issued metadata; it cannot replace tokens or identity.
	OpenAIPlanType                    string
	RateLimitUntil                    *time.Time
	ExtendRateLimitOnly               bool
	ClearRateLimit                    bool
	ObservedLimitedAt                 *time.Time
	ObservedResetAt                   *time.Time
	TempUnschedulableUntil            *time.Time
	TempUnschedulableReason           string
	ClearTempUnschedulable            bool
	RequireTempUnschedulableUnchanged bool
	ObservedTempUntil                 *time.Time
	ObservedTempReason                string
}

type CredentialStateMutationRepository interface {
	ApplyCredentialStateMutation(context.Context, *Account, CredentialStateMutation) (bool, error)
}

func credentialStateGeneration(account *Account) string {
	if account == nil {
		return ""
	}
	if account.CredentialGeneration != "" {
		return account.CredentialGeneration
	}
	if account.IsGrok() {
		generation, err := grokRequestOwnerFingerprint(account)
		if err != nil {
			return ""
		}
		return generation
	}
	raw, err := json.Marshal(struct {
		ID             int64
		Platform, Type string
		Credentials    map[string]any
		ProxyID        *int64
		ProxyURL       string
		ProxyStatus    string
	}{account.ID, account.Platform, account.Type, account.Credentials, account.ProxyID, credentialStateProxyURL(account), credentialStateProxyStatus(account)})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// CredentialStateGeneration carries the complete grant's digest into a slim
// scheduler projection without copying OAuth credentials or proxy secrets.
func CredentialStateGeneration(account *Account) string {
	return credentialStateGeneration(account)
}

func credentialStateProxyURL(account *Account) string {
	if account != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

func credentialStateProxyStatus(account *Account) string {
	if account != nil && account.Proxy != nil {
		return account.Proxy.Status
	}
	return ""
}

// A bound but ambiguous snapshot must never fall back to a mutable account.
// Unbound non-shadow callers can supply the snapshot they acquired themselves.
func credentialStateMutationOwner(ctx context.Context, account *Account) (*Account, bool) {
	if account == nil || account.CredentialGeneration != "" || account.CNQuotaGeneration != "" {
		return nil, false
	}
	if account.IsShadow() || requestCredentialOwnerBound(ctx, account) {
		owner, ok := requestCredentialOwner(ctx, account)
		if !ok || owner.CredentialGeneration != "" || owner.CNQuotaGeneration != "" {
			return nil, false
		}
		return owner, true
	}
	return snapshotRequestCredentialAccount(account), true
}
