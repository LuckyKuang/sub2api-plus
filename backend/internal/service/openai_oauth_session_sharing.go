package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
)

// A negative cache group is reserved for OAuth session bindings that are
// intentionally shared across the policy's authorized API-key groups.
const openAIOAuthSharedSessionCacheGroupID int64 = -2

func getAPIKeyGroupIDFromContext(c *gin.Context) *int64 {
	if c == nil {
		return nil
	}
	v, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, ok := v.(*APIKey)
	if !ok || apiKey == nil || apiKey.GroupID == nil {
		return nil
	}
	groupID := *apiKey.GroupID
	return &groupID
}

func getAPIKeyUserIDFromContext(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	v, exists := c.Get("api_key")
	if !exists {
		return 0
	}
	apiKey, ok := v.(*APIKey)
	if !ok || apiKey == nil {
		return 0
	}
	if apiKey.UserID > 0 {
		return apiKey.UserID
	}
	if apiKey.User != nil {
		return apiKey.User.ID
	}
	return 0
}

func openAIRequestUserID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	userID, _ := ctx.Value(ctxkey.UserID).(int64)
	return userID
}

// resolveOpenAIUpstreamSessionID preserves the legacy API-key isolation for
// ordinary accounts. A configured OAuth policy instead uses its account-scoped
// sharing domain, allowing only its authorized groups to obtain the same
// upstream session identifier.
func (s *OpenAIGatewayService) resolveOpenAIUpstreamSessionID(c *gin.Context, account *Account, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if account == nil || !account.IsOpenAIOAuthSessionSharingEnabled() {
		return isolateOpenAISessionID(getAPIKeyIDFromContext(c), raw), nil
	}
	if !account.IsOpenAIOAuthSessionGroupAllowed(getAPIKeyGroupIDFromContext(c)) {
		return "", ErrOpenAIOAuthSessionAccessDenied
	}
	policy, _, valid := account.OpenAIOAuthSessionPolicy()
	if !valid || !policy.Enabled || account.OpenAIOAuthSessionScopeAccountID() <= 0 {
		return "", ErrOpenAIOAuthSessionAccessDenied
	}
	userID := getAPIKeyUserIDFromContext(c)
	if userID <= 0 {
		return "", ErrOpenAIOAuthSessionAccessDenied
	}
	h := xxhash.New()
	_, _ = fmt.Fprintf(h, "openai-oauth-session:v2:account:%d:scope:%s:user:%d:raw:", account.OpenAIOAuthSessionScopeAccountID(), policy.ScopeVersion, userID)
	_, _ = h.WriteString(raw)
	return fmt.Sprintf("%016x", h.Sum64()), nil
}

func (s *OpenAIGatewayService) openAIOAuthSharedSessionCacheKey(userID int64, sessionHash string) string {
	normalized := strings.TrimSpace(sessionHash)
	if userID <= 0 || normalized == "" {
		return ""
	}
	return fmt.Sprintf("openai:oauth-share:v2:user:%d:%s", userID, normalized)
}

func (s *OpenAIGatewayService) getOpenAIOAuthSharedSessionAccountID(ctx context.Context, sessionHash string) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, nil
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	key := s.openAIOAuthSharedSessionCacheKey(userID, sessionHash)
	if key == "" {
		return 0, nil
	}
	return s.cache.GetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, key)
}

func (s *OpenAIGatewayService) bindOpenAIOAuthSharedSession(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	account *Account,
	ttl time.Duration,
) error {
	if s == nil || s.cache == nil || account == nil || !account.IsOpenAIOAuthSessionSharingEnabled() {
		return nil
	}
	if !account.IsOpenAIOAuthSessionGroupAllowed(groupID) {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	key := s.openAIOAuthSharedSessionCacheKey(userID, sessionHash)
	if key == "" {
		return nil
	}
	return s.cache.SetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, key, account.ID, ttl)
}

func (s *OpenAIGatewayService) refreshOpenAIOAuthSharedSession(ctx context.Context, sessionHash string, ttl time.Duration) error {
	if s == nil || s.cache == nil {
		return nil
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	key := s.openAIOAuthSharedSessionCacheKey(userID, sessionHash)
	if key == "" {
		return nil
	}
	return s.cache.RefreshSessionTTL(ctx, openAIOAuthSharedSessionCacheGroupID, key, ttl)
}

func (s *OpenAIGatewayService) deleteOpenAIOAuthSharedSession(ctx context.Context, sessionHash string) error {
	if s == nil || s.cache == nil {
		return nil
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	key := s.openAIOAuthSharedSessionCacheKey(userID, sessionHash)
	if key == "" {
		return nil
	}
	return s.cache.DeleteSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, key)
}

func openAIOAuthSharedResponseOwnerCacheKey(responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return ""
	}
	return "openai:oauth-share:response-owner:v3:" + DeriveSessionHashFromSeed(responseID)
}

func openAIOAuthLegacySharedResponseCacheKey(responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return ""
	}
	return "openai:oauth-share:response:v2:" + DeriveSessionHashFromSeed(responseID)
}

func openAIOAuthSharedResponseCacheKey(userID int64, responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if userID <= 0 || responseID == "" {
		return ""
	}
	return fmt.Sprintf("openai:oauth-share:response:v3:user:%d:%s", userID, DeriveSessionHashFromSeed(responseID))
}

func openAIOAuthSharedResponseScopeCacheKey(account *Account, userID int64, responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if account == nil || userID <= 0 || responseID == "" {
		return ""
	}
	policy, configured, valid := account.OpenAIOAuthSessionPolicy()
	accountID := account.OpenAIOAuthSessionScopeAccountID()
	if !configured || !valid || !policy.Enabled || accountID <= 0 {
		return ""
	}
	seed := fmt.Sprintf("account:%d:scope:%s:user:%d:response:%s", accountID, policy.ScopeVersion, userID, responseID)
	return "openai:oauth-share:response-scope:v3:" + DeriveSessionHashFromSeed(seed)
}

func (s *OpenAIGatewayService) bindOpenAIOAuthSharedResponseAccount(ctx context.Context, account *Account, responseID string, ttl time.Duration) error {
	if s == nil || s.cache == nil || account == nil || !account.IsOpenAIOAuthSessionSharingEnabled() {
		return nil
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	key := openAIOAuthSharedResponseCacheKey(userID, responseID)
	if key == "" {
		return nil
	}
	scopeKey := openAIOAuthSharedResponseScopeCacheKey(account, userID, responseID)
	if scopeKey == "" {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	ownerKey := openAIOAuthSharedResponseOwnerCacheKey(responseID)
	if ownerKey == "" {
		return ErrOpenAIOAuthSessionAccessDenied
	}
	// Publish ownership last. Partial scope/account writes are unreachable until
	// the owner marker exists, so a failed bind cannot revive a group-local chain.
	if err := s.cache.SetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, scopeKey, account.ID, ttl); err != nil {
		return err
	}
	if err := s.cache.SetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, key, account.ID, ttl); err != nil {
		return err
	}
	return s.cache.SetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, ownerKey, userID, ttl)
}

func (s *OpenAIGatewayService) getOpenAIOAuthSharedResponseAccount(ctx context.Context, groupID *int64, responseID string) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, nil
	}
	ownerKey := openAIOAuthSharedResponseOwnerCacheKey(responseID)
	if ownerKey == "" {
		return 0, nil
	}
	ownerUserID, ownerErr := s.cache.GetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, ownerKey)
	if ownerErr != nil {
		if errors.Is(ownerErr, ErrGatewayCacheMiss) {
			// A v2 shared marker proves this response came from the old policy
			// domain, which had no user ownership. Reject it instead of allowing a
			// same-group caller to revive the chain through the local fallback.
			legacyKey := openAIOAuthLegacySharedResponseCacheKey(responseID)
			legacyAccountID, legacyErr := s.cache.GetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, legacyKey)
			if legacyErr == nil && legacyAccountID > 0 {
				return 0, ErrOpenAIOAuthSessionAccessDenied
			}
			if errors.Is(legacyErr, ErrGatewayCacheMiss) {
				// No shared marker in either format: preserve genuinely group-local,
				// non-policy continuations created before this feature existed.
				return 0, nil
			}
			return 0, ErrOpenAIOAuthSessionAccessDenied
		}
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	userID := openAIRequestUserID(ctx)
	if userID <= 0 {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	if ownerUserID != userID {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	key := openAIOAuthSharedResponseCacheKey(userID, responseID)
	accountID, err := s.cache.GetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, key)
	if err != nil || accountID <= 0 {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	account, accountErr := s.getSchedulableAccount(ctx, accountID)
	if accountErr != nil || account == nil || !account.IsOpenAIOAuthSessionSharingEnabled() {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	if !account.IsOpenAIOAuthSessionGroupAllowed(groupID) {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	scopeKey := openAIOAuthSharedResponseScopeCacheKey(account, userID, responseID)
	if scopeKey == "" {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	scopedAccountID, scopeErr := s.cache.GetSessionAccountID(ctx, openAIOAuthSharedSessionCacheGroupID, scopeKey)
	if scopeErr != nil {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	if scopedAccountID != accountID {
		return 0, ErrOpenAIOAuthSessionAccessDenied
	}
	return accountID, nil
}

func (s *OpenAIGatewayService) validateOpenAIOAuthSharedPreviousResponseAccess(ctx context.Context, groupID *int64, responseID string) error {
	_, err := s.getOpenAIOAuthSharedResponseAccount(ctx, groupID, responseID)
	return err
}

// bindOpenAIResponseAccount keeps the established group-local previous-response
// binding and adds a policy-scoped lookup only for enabled OAuth accounts.
func (s *OpenAIGatewayService) bindOpenAIResponseAccount(ctx context.Context, store OpenAIWSStateStore, groupID int64, account *Account, responseID string, ttl time.Duration) error {
	if store == nil || account == nil {
		return nil
	}
	if account.IsOpenAIOAuthSessionSharingEnabled() {
		if !account.IsOpenAIOAuthSessionGroupAllowed(&groupID) || openAIRequestUserID(ctx) <= 0 {
			return ErrOpenAIOAuthSessionAccessDenied
		}
		if err := s.bindOpenAIOAuthSharedResponseAccount(ctx, account, responseID, ttl); err != nil {
			return err
		}
	}
	return store.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl)
}
