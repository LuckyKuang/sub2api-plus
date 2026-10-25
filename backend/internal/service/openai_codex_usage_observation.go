package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// OpenAICodexUsageRepository accepts quota evidence only for the credential
// owner and proxy that actually sent the request. There is no unguarded fallback.
const OpenAICodexUsageOwnerGenerationExtraKey = "codex_usage_owner_generation"

type OpenAICodexUsageRepository interface {
	UpdateOpenAICodexUsageIfUnchanged(context.Context, int64, *Account, map[string]any) (bool, error)
}

func persistOpenAICodexUsageObservation(ctx context.Context, repo AccountRepository, targetID int64, owner *Account, updates map[string]any, snapshot *OpenAICodexUsageSnapshot, fromSession bool) (bool, error) {
	guarded, ok := repo.(OpenAICodexUsageRepository)
	if !ok || owner == nil || targetID <= 0 {
		return false, errors.New("codex observation requires captured credential owner and conditional repository")
	}
	if updates == nil {
		return false, errors.New("codex observation requires updates")
	}
	updates[OpenAICodexUsageOwnerGenerationExtraKey] = credentialStateGeneration(owner)
	accepted, err := guarded.UpdateOpenAICodexUsageIfUnchanged(ctx, targetID, owner, updates)
	if err != nil || !accepted {
		return false, err
	}
	if targetID == owner.ID && snapshot != nil {
		observeOpenAIWeeklyUsageSnapshot(withCodexObservationOwner(ctx, owner), targetID, snapshot, fromSession)
	}
	if _, sampled := updates["codex_usage_updated_at"]; sampled {
		notifyOpenAIAutoResetForOwner(owner)
	}
	return true, nil
}

type codexObservationOwnerKey struct{}

func withCodexObservationOwner(ctx context.Context, owner *Account) context.Context {
	return context.WithValue(ctx, codexObservationOwnerKey{}, snapshotRequestCredentialAccount(owner))
}
func codexObservationOwner(ctx context.Context, accountID int64) *Account {
	if owner, _ := ctx.Value(codexObservationOwnerKey{}).(*Account); owner != nil && owner.ID == accountID {
		return owner
	}
	selected := &Account{ID: accountID}
	if owner, ok := requestCredentialOwner(ctx, selected); ok {
		return owner
	}
	return nil
}

// The weekly baseline belongs to an upstream principal and proxy. Rotating an
// access token keeps the baseline, but cannot corroborate another grant's
// pending evidence. Unknown principals are isolated by the complete grant.
func OpenAICodexObservationGeneration(owner *Account) string {
	if owner == nil {
		return ""
	}
	// A ChatGPT team/account ID can be shared by several users. Without a
	// reliable user ID, retaining cross-token state would conflate principals.
	if owner.Type == AccountTypeOAuth && !owner.IsOpenAIAgentIdentity() && (strings.TrimSpace(owner.GetChatGPTAccountID()) == "" || strings.TrimSpace(owner.GetCredential("chatgpt_user_id")) == "") {
		return credentialStateGeneration(owner)
	}
	principal := openAICodexStateOwner(context.Background(), owner)
	if principal == "" {
		return credentialStateGeneration(owner)
	}
	raw, err := json.Marshal(struct {
		ID                    int64
		Principal             string
		ProxyID               *int64
		ProxyURL, ProxyStatus string
	}{owner.ID, principal, owner.ProxyID, credentialStateProxyURL(owner), credentialStateProxyStatus(owner)})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func capturedCodexUsageOwner(ctx context.Context, account *Account) *Account {
	owner, ok := credentialStateMutationOwner(ctx, account)
	if !ok {
		return nil
	}
	return owner
}

// IsOpenAICodexObservationExtraKey enumerates upstream-derived state. Configuration
// such as websocket mode, quota policy and identity overrides is never removed.
func IsOpenAICodexObservationExtraKey(key string) bool {
	switch key {
	case "codex_usage_updated_at", OpenAICodexUsageOwnerGenerationExtraKey, "codex_limit_name", "codex_rate_limit_families", OpenAIAutoResetCreditStateExtraKey:
		return true
	}
	for _, prefix := range []string{"codex_primary_", "codex_secondary_", "codex_5h_", "codex_7d_", "codex_credits_", "codex_reset_credit_", "openai_compact_last_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return key == "openai_compact_supported" || key == "openai_compact_checked_at"
}

func clearStaleCodexObservationExtra(account *Account, updates map[string]any) {
	if account == nil || account.Extra == nil {
		return
	}
	generation, ok := updates[OpenAICodexUsageOwnerGenerationExtraKey].(string)
	if !ok || generation == "" || account.Extra[OpenAICodexUsageOwnerGenerationExtraKey] == generation {
		return
	}
	for key := range account.Extra {
		if IsOpenAICodexObservationExtraKey(key) {
			delete(account.Extra, key)
		}
	}
}

// Scheduling may use a quota observation only after proving its credential
// owner. A shadow's observation belongs to its parent grant, never to the
// shadow's empty credentials. Legacy observations can still be displayed.
func openAICodexUsageObservationMatchesOwner(account, owner *Account) bool {
	if account == nil || owner == nil || owner.IsShadow() {
		return false
	}
	if account.IsShadow() {
		if account.ParentAccountID == nil || *account.ParentAccountID != owner.ID || !owner.IsOpenAIOAuth() {
			return false
		}
	} else if account.ID != owner.ID || credentialStateGeneration(account) != credentialStateGeneration(owner) {
		return false
	}
	generation, _ := account.Extra[OpenAICodexUsageOwnerGenerationExtraKey].(string)
	return generation != "" && generation == credentialStateGeneration(owner)
}
