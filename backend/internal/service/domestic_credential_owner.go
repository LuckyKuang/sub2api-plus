package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

const domesticOAuthLoginIDCredential = "oauth_login_id"

// A new authorization owns a new session namespace. Routine access/refresh
// token rotation retains its login ID. Legacy grants keep their old row scope
// until the next authorization, without deriving a lifetime from a short token.
func domesticCredentialOwnerAccount(ctx context.Context, selected *Account) *Account {
	if selected == nil || selected.CredentialGeneration != "" || selected.CNQuotaGeneration != "" {
		return nil
	}
	owner := selected
	if ctx != nil {
		if sent, ok := ctx.Value(requestCredentialTransportKey{}).(requestCredentialTransportEvidence); ok {
			if sent.owner == nil || sent.owner.CredentialGeneration != "" || sent.owner.CNQuotaGeneration != "" || credentialOwnerKey(sent.owner) != credentialOwnerKey(selected) {
				return nil
			}
			return sent.owner
		}
		if requestCredentialOwnerBound(ctx, selected) {
			acquired, known := requestCredentialOwner(ctx, selected)
			if !known || acquired == nil || acquired.CredentialGeneration != "" || acquired.CNQuotaGeneration != "" {
				return nil
			}
			return acquired
		}
	}
	return owner
}

func domesticCredentialOwnerScope(ctx context.Context, selected *Account) string {
	owner := domesticCredentialOwnerAccount(ctx, selected)
	if owner == nil {
		return ""
	}
	id := owner.ID
	if owner.IsShadow() && owner.ParentAccountID != nil {
		id = *owner.ParentAccountID
	}
	grant := owner.GetCredential(domesticOAuthLoginIDCredential)
	if owner.Type == AccountTypeAPIKey {
		key := sha256.Sum256([]byte(owner.GetOpenAIProtocolAPIKey()))
		grant = "apikey:" + hex.EncodeToString(key[:])
	} else if grant == "" {
		grant = "legacy-login"
	}
	value := owner.Platform + "\x00" + owner.Type + "\x00" + strconv.FormatInt(id, 10) + "\x00" + grant
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
