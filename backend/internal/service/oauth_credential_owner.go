package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

// OAuthCredentialOwner is an opaque comparison value for redacted admin DTOs.
// Unrelated usage observations and names do not identify a login. Scheduling
// changes are included because reauthorization must not clear a newer failure.
// Authorization is still enforced by authenticated management handlers and SQL
// owner CAS; possession of this value never grants access to an account.
func OAuthCredentialOwner(account *Account) string {
	if account == nil || !account.IsOAuth() || account.IsCredentialShadow() {
		return ""
	}
	var route any
	if account.ProxyID != nil && account.Proxy != nil {
		p := account.Proxy
		route = []any{p.ID, p.Protocol, p.Host, p.Port, p.Username, p.Password, p.Status, p.ExpiresAt}
	}
	payload, err := json.Marshal([]any{account.ID, account.Platform, account.Type, account.Credentials, account.ProxyID, route,
		account.Status, account.ErrorMessage, account.Schedulable, account.RateLimitedAt, account.RateLimitResetAt,
		account.TempUnschedulableUntil, account.TempUnschedulableReason, account.OverloadUntil, account.ExpiresAt, account.AutoPauseOnExpired,
		account.Extra["model_rate_limits"], account.Extra["antigravity_quota_scopes"], account.Extra["grok_needs_reauth"],
		account.Extra["grok_needs_reauth_reason"], account.Extra["grok_needs_reauth_at"], runtimeRecoveryGeneration(account),
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// CaptureOAuthCredentialOwner prevents mutable repository/cache objects from
// replacing the login or route while an issuer or persistence call is pending.
func CaptureOAuthCredentialOwner(account *Account) (*Account, error) {
	owner, err := snapshotCNControlAccount(account)
	if err != nil {
		return nil, err
	}
	owner.Extra, err = cloneAccountJSONMap(account.Extra)
	return owner, err
}

type OAuthCredentialOwnerRepository interface {
	UpdateOAuthCredentialsIfUnchanged(context.Context, *Account, string, map[string]any, map[string]any, bool) (bool, error)
}

type OAuthCredentialCommitter interface {
	CommitOAuthCredentials(context.Context, *Account, string, map[string]any, map[string]any, bool) (*Account, error)
}

type OAuthSchedulingRecoveryPreparer interface {
	PrepareOAuthSchedulingRecovery(*Account) func()
}

func (s *adminServiceImpl) CommitOAuthCredentials(ctx context.Context, expected *Account, accountType string, incoming, extra map[string]any, recoverState bool) (*Account, error) {
	if expected == nil || !expected.IsOAuth() || expected.IsCredentialShadow() {
		return nil, infraerrors.BadRequest("NOT_OAUTH", "a credential-owning OAuth account is required")
	}
	if accountType != AccountTypeOAuth && accountType != AccountTypeSetupToken {
		return nil, infraerrors.BadRequest("OAUTH_ACCOUNT_TYPE_INVALID", "invalid OAuth account type")
	}
	if dbent.TxFromContext(ctx) != nil {
		return nil, errors.New("OAuth credential commit requires its own committed persistence boundary")
	}
	if accountType != expected.Type {
		shadows, err := s.accountRepo.ListShadowsByParent(ctx, expected.ID)
		if err != nil {
			return nil, err
		}
		if len(shadows) != 0 {
			return nil, infraerrors.BadRequest("SPARK_SHADOW_PARENT_IMMUTABLE_TYPE", "cannot change credential owner type while a shadow exists")
		}
	}
	credentials := SanitizeStoredCredentials(expected.Platform, MergeCredentials(expected.Credentials, incoming))
	if err := NormalizeHeaderOverrideCredentials(credentials); err != nil {
		return nil, err
	}
	if err := NormalizeProtocolRulesCredentials(credentials); err != nil {
		return nil, err
	}
	if err := s.normalizeOpenAIAccountUserAgent(ctx, expected.Platform, accountType, credentials); err != nil {
		return nil, err
	}
	updates, err := cloneAccountJSONMap(extra)
	if err != nil {
		return nil, err
	}
	if _, exists := updates[OpenAIOAuthSessionPolicyExtraKey]; exists {
		return nil, infraerrors.BadRequest("OAUTH_EXTRA_INVALID", "session policy must be changed through the account editor")
	}
	updates = sanitizedCodexFingerprintExtraUpdates(updates)
	updates = stripOpenAIAutoResetCreditManagedExtra(updates, true)
	delete(updates, RuntimeRecoveryGenerationExtraKey)
	for _, key := range []string{"upstream_billing_probe_enabled", "upstream_billing_rate_sync_enabled", "upstream_billing_probe", OllamaCloudUsageSessionExtraKey, OllamaCloudUsageAutoRefreshExtraKey, OllamaCloudUsageSnapshotExtraKey} {
		delete(updates, key)
	}
	if err := normalizeCodexFingerprintModeUpdateExtra(updates); err != nil {
		return nil, err
	}
	if err := ValidateOpenAILongContextBillingExtra(expected.Platform, updates); err != nil {
		return nil, err
	}
	if err := ValidateUpstreamRequestIDHeaderExtra(updates); err != nil {
		return nil, err
	}
	if err := ValidateEgressCountryExtra(updates); err != nil {
		return nil, err
	}
	if recoverState && expected.Platform == PlatformGrok {
		if updates == nil {
			updates = make(map[string]any)
		}
		updates["grok_needs_reauth"], updates["grok_needs_reauth_reason"], updates["grok_needs_reauth_at"] = false, "", ""
	}
	writer, ok := s.accountRepo.(OAuthCredentialOwnerRepository)
	if !ok {
		return nil, errors.New("atomic OAuth credential persistence is unavailable")
	}
	var recoverRuntime func()
	if recoverState {
		if preparer, ok := s.runtimeBlocker.(OAuthSchedulingRecoveryPreparer); ok {
			recoverRuntime = preparer.PrepareOAuthSchedulingRecovery(expected)
		}
	}
	applied, err := writer.UpdateOAuthCredentialsIfUnchanged(ctx, expected, accountType, credentials, updates, recoverState)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, infraerrors.New(http.StatusConflict, "OAUTH_OWNER_CHANGED", "account credentials, proxy or scheduling state changed; restart authorization")
	}
	if recoverRuntime != nil {
		recoverRuntime()
	}
	return s.accountRepo.GetByID(ctx, expected.ID)
}
