package service

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokConversationIDHeader       = "X-Grok-Conv-Id"
	grokConversationGroupNamespace = "xai:grok-build:conversation-group:"
	claudeCodeSessionHeader        = "X-Claude-Code-Session-Id"
)

// grokAgentID is the process-level bucketing key used by the official shell.
// Keep it stable for the lifetime of this process, but do not share one fixed
// value across every deployment.
var grokAgentID = uuid.NewString()

// Claude Code metadata.user_id often ends with _session_<uuid>.
var claudeCodeSessionSuffixPattern = regexp.MustCompile(`_session_([a-f0-9-]+)$`)

// extractClaudeCodeSessionID resolves the Claude Code conversation id from
// headers or Anthropic/OpenAI-compatible payload metadata.
func extractClaudeCodeSessionID(c *gin.Context, body []byte) string {
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader(claudeCodeSessionHeader)); seed != "" {
			return seed
		}
	}
	return extractClaudeCodeSessionIDFromPayload(body)
}

func extractClaudeCodeSessionIDFromPayload(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if userID == "" {
		return ""
	}
	if matches := claudeCodeSessionSuffixPattern.FindStringSubmatch(userID); len(matches) >= 2 {
		return matches[1]
	}
	// Claude Code may embed JSON: {"session_id":"..."}
	if len(userID) > 0 && userID[0] == '{' {
		if sid := strings.TrimSpace(gjson.Get(userID, "session_id").String()); sid != "" {
			return sid
		}
	}
	return ""
}

// resolveGrokCacheIdentity derives one stable, tenant-isolated routing identity
// for xAI's server-side prompt cache. The returned value is safe to expose to
// the upstream: it never contains the client's raw session identifier.
//
// A valid downstream API key is required. This intentionally fails closed on
// internal probes and incomplete request contexts instead of creating a cache
// identity that could be shared by unrelated tenants.
func resolveGrokCacheIdentity(c *gin.Context, body []byte, explicitKey, upstreamModel string) string {
	apiKeyID := getAPIKeyIDFromContext(c)
	if apiKeyID <= 0 {
		return ""
	}
	// /responses/compact rejects tool_choice and does not represent a normal
	// conversation turn. Keep cache identity out of this path.
	if isOpenAIResponsesCompactPath(c) {
		return ""
	}

	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if model == "" {
		return ""
	}

	seed := explicitGrokCacheSeed(c, body, explicitKey)
	if seed == "" {
		seed = deriveOpenAIStablePrefixSessionSeed(body)
		if seed == "" {
			// A model alone is too broad for cache routing. Preserve the
			// existing first-user-derived identity when no reusable prefix is
			// available so unrelated prompts do not share one tenant-wide key.
			seed = deriveOpenAIAnchoredContentSessionSeed(body)
		}
	}
	if seed == "" {
		return ""
	}

	// generateSessionUUID hashes the whole seed before formatting it as a UUID.
	// Include a versioned namespace so this identity cannot collide with other
	// upstream session identifiers derived by sub2api.
	isolatedSeed := fmt.Sprintf("grok-prompt-cache:v1:%d:%s:%s", apiKeyID, model, seed)
	return generateSessionUUID(isolatedSeed)
}

func explicitGrokCacheSeed(c *gin.Context, body []byte, explicitKey string) string {
	// Official Responses: an explicit cache key outranks the conversation ID.
	if seed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); seed != "" {
		return seed
	}
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader(grokConversationIDHeader)); seed != "" {
			return seed
		}
	}
	if seed := strings.TrimSpace(explicitKey); seed != "" {
		return seed
	}
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader("X-Grok-Session-Id")); seed != "" {
			return seed
		}
	}
	if seed := extractClaudeCodeSessionID(c, body); seed != "" {
		return seed
	}
	if seed := explicitOpenAIHeaderSessionID(c); seed != "" {
		return seed
	}
	return grokPreviousResponseSessionSeed(body)
}

func isGrokRequestContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if c.Request != nil {
		if platform, ok := ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform == PlatformGrok
		}
	}
	v, exists := c.Get("api_key")
	if !exists {
		return false
	}
	apiKey, ok := v.(*APIKey)
	return ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == PlatformGrok
}

// applyGrokResponsesCacheIdentity changes only the Responses cache field.
// Official responses.rs maps prompt_cache_key independently of tool declarations.
func applyGrokResponsesCacheIdentity(body []byte, identity string) ([]byte, error) {
	if strings.TrimSpace(identity) == "" {
		return sjson.DeleteBytes(body, "prompt_cache_key")
	}
	return sjson.SetBytes(body, "prompt_cache_key", identity)
}

// isKnownGrokFreeAccount recognizes free-tier Grok accounts, used for
// media free_tier blocks (broader than soft-gate).
// Soft-gate uses isExplicitGrokFreeOAuthAccount (exact "free" only).
func isKnownGrokFreeAccount(account *Account) bool {
	if account == nil || !account.IsGrokOAuth() {
		return false
	}
	// Live access-token JWT wins over stale billing/credential snapshots
	// so a downgrade to free is visible as soon as the AT is refreshed.
	if jwtTier := xai.SubscriptionTierFromJWT(account.GetCredential("access_token")); jwtTier != "" {
		return isGrokFreeSubscriptionTier(jwtTier)
	}
	freeSignal := false
	paidSignal := false
	inferredFreeSignal := false
	if billing, err := grokBillingSnapshotFromExtra(account.Extra); err == nil && billing != nil {
		if tier := strings.TrimSpace(billing.Plan); tier != "" {
			if isGrokFreeSubscriptionTier(tier) {
				freeSignal = true
			} else if !isGrokUnknownSubscriptionTier(tier) {
				paidSignal = true
			}
		}
		// Usage % or a monthly dollar cap is evidence of a paid plan.
		if billing.UsagePercent != nil || billing.UsedPercent != nil ||
			(billing.MonthlyLimitCents != nil && *billing.MonthlyLimitCents > 0) {
			paidSignal = true
		}
		// Empty plan + successful monthly observation → inferred free (no paid plan/limit).
		if strings.TrimSpace(billing.MonthlyUpdatedAt) != "" ||
			(billing.StatusCode >= http.StatusOK && billing.StatusCode < http.StatusMultipleChoices &&
				!billing.Partial && len(billing.FailedWindows) == 0) {
			inferredFreeSignal = true
		}
	}
	if snapshot, err := grokQuotaSnapshotFromExtra(account.Extra); err == nil && snapshot != nil {
		if tier := strings.TrimSpace(snapshot.SubscriptionTier); tier != "" {
			if isGrokFreeSubscriptionTier(tier) {
				freeSignal = true
			} else if !isGrokUnknownSubscriptionTier(tier) {
				paidSignal = true
			}
		}
		if snapshot.Tokens != nil && snapshot.Tokens.Limit != nil &&
			xai.IsGrokFreeRolling24hTokenLimit(*snapshot.Tokens.Limit) {
			inferredFreeSignal = true
		}
	}
	// Only credentials subscription_tier is authoritative here (not plan_type / extra keys).
	if tier := strings.TrimSpace(account.GetCredential("subscription_tier")); tier != "" {
		if isGrokFreeSubscriptionTier(tier) {
			freeSignal = true
		} else if !isGrokUnknownSubscriptionTier(tier) {
			paidSignal = true
		}
	}
	// Explicit paid evidence always wins over an inferred Free signal.
	return !paidSignal && (freeSignal || inferredFreeSignal)
}

func isGrokFreeSubscriptionTier(tier string) bool {
	switch xai.NormalizeSubscriptionTier(tier) {
	case "free", "x_basic":
		return true
	default:
		return false
	}
}

func isGrokUnknownSubscriptionTier(tier string) bool {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "unknown", "n/a", "none":
		return true
	default:
		return false
	}
}

// grokConversationSnapshot separates official request association from cache routing.
// side_call.rs permits a fresh conversation ID and the parent session/cache key.
type grokConversationSnapshot struct {
	ConversationID string
	SessionID      string
	RootSessionID  string
}

func resolveGrokRequestConversation(c *gin.Context, body []byte, cacheKey string) grokConversationSnapshot {
	result := grokConversationSnapshot{ConversationID: cacheKey, SessionID: cacheKey, RootSessionID: cacheKey}
	tenant := getAPIKeyIDFromContext(c)
	if tenant <= 0 || isOpenAIResponsesCompactPath(c) {
		return result
	}
	isolate := func(seed string) string {
		if strings.TrimSpace(seed) == "" {
			return ""
		}
		return generateSessionUUID(fmt.Sprintf("grok-conversation:v2:%d:%s", tenant, strings.TrimSpace(seed)))
	}
	session := ""
	conv := ""
	root := ""
	if c != nil {
		session = strings.TrimSpace(c.GetHeader("X-Grok-Session-Id"))
		conv = strings.TrimSpace(c.GetHeader(grokConversationIDHeader))
		// An official group declaration identifies a root shared by descendants.
		root = strings.TrimSpace(c.GetHeader("X-Grok-Conv-Group-Id"))
	}
	if session == "" {
		session = extractClaudeCodeSessionID(c, body)
	}
	if session == "" {
		session = explicitOpenAIHeaderSessionID(c)
	}
	if session == "" {
		session = conv
	}
	if scoped := isolate(session); scoped != "" {
		result.SessionID = scoped
	}
	if scoped := isolate(conv); scoped != "" {
		result.ConversationID = scoped
	} else {
		result.ConversationID = result.SessionID
	}
	result.RootSessionID = result.SessionID
	if scoped := isolate(root); scoped != "" {
		result.RootSessionID = scoped
	}
	return result
}

func applyGrokConversationHeaders(headers http.Header, conversation grokConversationSnapshot) {
	for name, value := range map[string]string{
		grokConversationIDHeader: conversation.ConversationID,
		"X-Grok-Session-Id":      conversation.SessionID,
	} {
		if value == "" {
			headers.Del(name)
		} else {
			headers.Set(name, value)
		}
	}
	if conversation.RootSessionID == "" {
		headers.Del("X-Grok-Conv-Group-Id")
	} else {
		headers.Set("X-Grok-Conv-Group-Id", uuid.NewSHA1(uuid.NameSpaceOID, []byte(grokConversationGroupNamespace+conversation.RootSessionID)).String())
	}
}

// applyGrokRequestMetadata renders only authoritative sampler declarations.
func applyGrokRequestMetadata(headers http.Header, body []byte, conversation grokConversationSnapshot, userID string) {
	if headers == nil {
		return
	}
	headers.Set("x-grok-req-id", uuid.NewString())
	headers.Set("x-grok-agent-id", grokAgentID)
	if model := strings.TrimSpace(gjson.GetBytes(body, "model").String()); model != "" {
		headers.Set("x-grok-model-override", model)
	} else {
		headers.Del("x-grok-model-override")
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		headers.Set("x-grok-user-id", userID)
	} else {
		headers.Del("x-grok-user-id")
	}
	applyGrokConversationHeaders(headers, conversation)
}

// Imagine start/poll clients carry session association, not sampler metadata.
func applyGrokMediaSessionHeader(headers http.Header, c *gin.Context) {
	conversation := resolveGrokRequestConversation(c, nil, "")
	if conversation.SessionID != "" {
		headers.Set("X-Grok-Session-Id", conversation.SessionID)
	}
}

// stripGrokChatPromptCacheKey removes the Responses-only body field after it
// has been used as an identity seed. Chat Completions routes cache by header.
func stripGrokChatPromptCacheKey(body []byte) ([]byte, error) {
	if !gjson.GetBytes(body, "prompt_cache_key").Exists() {
		return body, nil
	}
	return sjson.DeleteBytes(body, "prompt_cache_key")
}

// hasGrokResponsesToolIntent tracks explicit tools through WS tool lowering.
func hasGrokResponsesToolIntent(body []byte) bool {
	if gjson.GetBytes(body, "tools").Exists() || gjson.GetBytes(body, "tool_choice").Exists() {
		return true
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "additional_tools" {
			tools := item.Get("tools")
			if !tools.IsArray() || len(tools.Array()) > 0 {
				return true
			}
		}
	}
	return false
}
