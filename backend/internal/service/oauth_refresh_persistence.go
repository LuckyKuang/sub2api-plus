package service

import (
	"context"
	"fmt"
	"strings"
)

type OAuthRefreshSuccessRepository interface {
	UpdateOAuthRefreshCredentialsIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
}

// Keep only a refresh of the exact attempted grant. A concurrently reauthorized
// account wins and is reread before its token can be returned to the caller.
func (api *OAuthRefreshAPI) persistOAuthRefresh(ctx context.Context, cacheKey string, attempted *Account, credentials map[string]any) (*OAuthRefreshResult, error) {
	prefix := "native_oauth"
	if attempted.Platform == PlatformOpenAI {
		prefix = "openai_oauth"
	}
	repo, ok := api.accountRepo.(OAuthRefreshSuccessRepository)
	if !ok {
		return nil, oauthRefreshPersistenceFailure(prefix+"_persistence_unavailable", nil)
	}
	applied, err := repo.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, attempted, credentials)
	if err != nil {
		return nil, oauthRefreshPersistenceFailure(prefix+"_persistence_failed", err)
	}
	// A committed rotation may invalidate cached access even when the request
	// was canceled immediately after CAS. Finish bounded local cleanup/readback.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultRefreshPostPersistCleanupTimeout)
	defer cancel()
	if applied && attempted.Platform == PlatformOpenAI && api.tokenCache != nil {
		if err := api.tokenCache.DeleteAccessToken(readCtx, cacheKey); err != nil {
			return nil, oauthRefreshPersistenceFailure(prefix+"_cache_invalidation_failed", err)
		}
	}
	current, err := api.accountRepo.GetByID(readCtx, attempted.ID)
	if err != nil || current == nil {
		if err == nil {
			err = fmt.Errorf("account not found after refresh")
		}
		return nil, oauthRefreshPersistenceFailure(prefix+"_state_unavailable", err)
	}
	if current.ID != attempted.ID || current.Platform != attempted.Platform || current.Type != attempted.Type || !current.IsActive() ||
		!oauthRefreshProxyIDsEqual(current.ProxyID, attempted.ProxyID) ||
		attempted.Platform == PlatformOpenAI && !sameOpenAIRefreshOwner(attempted, current) {
		return nil, oauthRefreshPersistenceFailure(prefix+"_owner_changed", nil)
	}
	if attempted.Platform == PlatformOpenAI && !applied && strings.TrimSpace(current.GetCredential("access_token")) == strings.TrimSpace(attempted.GetCredential("access_token")) {
		return nil, oauthRefreshPersistenceFailure(prefix+"_persistence_conflict", nil)
	}
	result := &OAuthRefreshResult{Account: current, Refreshed: applied, AttemptedAccount: attempted}
	if applied {
		result.NewCredentials = credentials
	}
	return result, nil
}

// The provider may already have consumed the refresh token. Contain background
// refresh as well as request-path retry; neither may punish or repeat this grant.
func oauthRefreshPersistenceFailure(reason string, err error) error {
	return NewProviderCredentialFailure(GatewayFailureReason(reason), &providerCycleContainmentRefreshError{err: err})
}

func oauthRefreshProxyIDsEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOpenAIRefreshOwner(selected, current *Account) bool {
	return selected != nil && current != nil && current.IsActive() && selected.ID == current.ID &&
		selected.Platform == current.Platform && current.Platform == PlatformOpenAI &&
		selected.Type == current.Type && current.Type == AccountTypeOAuth &&
		oauthRefreshProxyIDsEqual(selected.ProxyID, current.ProxyID) && selected.proxyURLOrEmpty() == current.proxyURLOrEmpty()
}
