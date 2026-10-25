package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

type openAIOAuthSharedAffinityContextKey struct{}

type openAIOAuthSharedAffinity struct {
	userID  int64
	digests map[string]string
}

// Keep only digests of explicit conversation identifiers. Content-derived
// fallback and per-connection fallback seeds are not cross-key identities.
// The ordinary hash still includes the API key; sharing is a separate opt-in
// lookup whose account and current policy are checked before use.
func attachOpenAIOAuthSharedAffinity(c *gin.Context, seed, localHash string) {
	if c == nil || c.Request == nil || strings.TrimSpace(seed) == "" || localHash == "" {
		return
	}
	userID := getAPIKeyUserIDFromContext(c)
	if userID <= 0 {
		return
	}
	ctx := c.Request.Context()
	previous, _ := ctx.Value(openAIOAuthSharedAffinityContextKey{}).(openAIOAuthSharedAffinity)
	current := openAIOAuthSharedAffinity{userID: userID, digests: make(map[string]string, len(previous.digests)+1)}
	if previous.userID == userID {
		for key, value := range previous.digests {
			current.digests[key] = value
		}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("openai-oauth-affinity:v1:user:%d:%s", userID, strings.TrimSpace(seed))))
	current.digests[localHash] = fmt.Sprintf("%x", sum)
	c.Request = c.Request.WithContext(context.WithValue(ctx, openAIOAuthSharedAffinityContextKey{}, current))
}

func openAIOAuthSharedAffinityDigest(ctx context.Context, localHash string) string {
	if ctx == nil {
		return ""
	}
	affinity, _ := ctx.Value(openAIOAuthSharedAffinityContextKey{}).(openAIOAuthSharedAffinity)
	if affinity.userID <= 0 || affinity.userID != openAIRequestUserID(ctx) {
		return ""
	}
	return affinity.digests[localHash]
}

// WithOpenAIOAuthSharedAffinity preserves an already-derived execution context
// (cancellation, inflight reservation, credential owner) when session parsing
// subsequently adds affinity to the Gin request, notably at WS ingress.
func WithOpenAIOAuthSharedAffinity(ctx context.Context, c *gin.Context) context.Context {
	if ctx == nil || c == nil || c.Request == nil {
		return ctx
	}
	if affinity, ok := c.Request.Context().Value(openAIOAuthSharedAffinityContextKey{}).(openAIOAuthSharedAffinity); ok {
		return context.WithValue(ctx, openAIOAuthSharedAffinityContextKey{}, affinity)
	}
	return ctx
}

func openAIOAuthSharedSessionScopeCacheKey(account *Account, userID int64, digest string) string {
	if account == nil || userID <= 0 || digest == "" {
		return ""
	}
	policy, configured, valid := account.OpenAIOAuthSessionPolicy()
	if !configured || !valid || !policy.Enabled || account.OpenAIOAuthSessionScopeAccountID() <= 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("account:%d:scope:%s:user:%d:session:%s", account.OpenAIOAuthSessionScopeAccountID(), policy.ScopeVersion, userID, digest)))
	return fmt.Sprintf("openai:oauth-share:session-scope:v3:%x", sum)
}
