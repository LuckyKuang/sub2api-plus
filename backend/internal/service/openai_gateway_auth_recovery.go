package service

import (
	"context"
	"errors"
	"net/http"
)

// canForceRefreshOpenAIAuthOnUnauthorized reports whether an upstream 401 is
// eligible for the same-account credential recovery. Only OpenAI OAuth
// credential owners qualify: personal access tokens have no refresh lifecycle,
// Agent Identity accounts recover through their own task-registration flow,
// and setup tokens are static bearers. Credential shadows pass the gate and
// recover through their credential owner; ForceRefreshToken rejects owners
// without a refresh token so the request falls through to normal failover.
func (s *OpenAIGatewayService) canForceRefreshOpenAIAuthOnUnauthorized(account *Account) bool {
	if s == nil || account == nil {
		return false
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if account.IsOpenAIAgentIdentity() || account.IsOpenAIPersonalAccessToken() {
		return false
	}
	return true
}

// recoverOpenAIAuthAfterUnauthorized force-refreshes the credential owner's
// token after an upstream 401 and returns the new access token for the
// same-account retry. Shadows resolve to their credential owner first.
func (s *OpenAIGatewayService) recoverOpenAIAuthAfterUnauthorized(ctx context.Context, account *Account) (string, error) {
	if s == nil || account == nil {
		return "", errors.New("account is nil")
	}
	credAccount := account
	if account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return "", err
		}
		if resolved == nil {
			return "", errors.New("credential owner not found for 401 recovery")
		}
		credAccount = resolved
	}
	if s.openAITokenProvider == nil {
		return "", errors.New("token provider is not configured")
	}
	return s.openAITokenProvider.ForceRefreshToken(ctx, credAccount)
}

// isOpenAIUnauthorizedRecoverableStatus keeps the 401-only boundary explicit:
// 403 and other statuses never enter the same-account credential recovery.
func isOpenAIUnauthorizedRecoverableStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized
}

// openAIAuthRecoveryRetryReason is the ops event reason recorded when a 401
// triggers the same-account credential refresh retry.
const openAIAuthRecoveryRetryReason = "auth_refresh_401"
