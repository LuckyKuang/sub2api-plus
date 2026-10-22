package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
)

// CNProviderQuotaSnapshotRepository atomically binds a quota observation to the
// exact credential owner and proxy that supplied it. A missing implementation
// must never fall back to an unconditional Extra write.
type CNProviderQuotaSnapshotRepository interface {
	UpdateCNProviderQuotaIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
}

const cnQuotaGenerationSuffix = "quota_generation"

func cnQuotaRequiresGeneration(account *Account) bool {
	if account == nil {
		return false
	}
	if account.IsOpenCodeGo() || account.IsCommandCode() || account.IsCline() {
		return false
	}
	switch account.Platform {
	case PlatformKimi, PlatformZhipu, PlatformMiniMax:
		return true
	default:
		return false
	}
}

func cnQuotaGeneration(account *Account) string {
	if account == nil {
		return ""
	}
	if account.CNQuotaGeneration != "" {
		return account.CNQuotaGeneration
	}
	proxyStatus := ""
	if account.Proxy != nil {
		proxyStatus = account.Proxy.Status
	}
	encoded, err := json.Marshal(struct {
		ID                    int64
		Platform, Type        string
		Credentials           map[string]any
		ProxyID               *int64
		ProxyURL, ProxyStatus string
	}{account.ID, account.Platform, account.Type, account.Credentials, account.ProxyID, account.proxyURLOrEmpty(), proxyStatus})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// CNQuotaGeneration projects the pre-existing CN credential/proxy-status hash
// into a scheduler row without retaining the authorization secrets.
func CNQuotaGeneration(account *Account) string {
	if !cnQuotaRequiresGeneration(account) {
		return ""
	}
	return cnQuotaGeneration(account)
}

func cnQuotaSnapshotMatches(account *Account) bool {
	if !cnQuotaRequiresGeneration(account) {
		return true
	}
	generation, _ := account.Extra[cnExtraKey(account.Platform, cnQuotaGenerationSuffix)].(string)
	return generation != "" && generation == cnQuotaGeneration(account)
}

func snapshotCNQuotaAccount(account *Account) (*Account, error) {
	if account == nil {
		return nil, ErrAccountNilInput
	}
	if account.CredentialGeneration != "" || account.CNQuotaGeneration != "" {
		return nil, errors.New("quota request requires a hydrated credential owner")
	}
	snapshot := snapshotOAuthRefreshAccount(account)
	var err error
	snapshot.Credentials, err = cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return nil, err
	}
	snapshot.Extra, err = cloneAccountJSONMap(account.Extra)
	return snapshot, err
}

type cnQuotaRequestOwnerKey struct{}

func withCNQuotaRequestOwner(req *http.Request, account *Account) error {
	snapshot, err := snapshotCNQuotaAccount(account)
	if err != nil {
		return err
	}
	*req = *req.WithContext(context.WithValue(req.Context(), cnQuotaRequestOwnerKey{}, snapshot))
	return nil
}

func cnQuotaRequestOwner(req *http.Request, fallback *Account) *Account {
	if account, ok := req.Context().Value(cnQuotaRequestOwnerKey{}).(*Account); ok && account != nil {
		return account
	}
	return fallback
}

func cnQuotaBoundExtraUpdates(account *Account, updates map[string]any) {
	provider := account.Platform
	updates[cnExtraKey(provider, cnQuotaGenerationSuffix)] = cnQuotaGeneration(account)
	// A valid response replaces this owner's complete recognized snapshot.
	// An omitted window cannot inherit another grant's percent or reset time.
	for _, window := range []string{"5h", "weekly", "monthly", "monthly_code"} {
		for _, suffix := range []string{"_used_percent", "_reset_at", "_unlimited"} {
			key := cnExtraKey(provider, window+suffix)
			if _, exists := updates[key]; !exists {
				updates[key] = nil
			}
		}
	}
}
