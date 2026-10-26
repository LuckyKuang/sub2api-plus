package service

import (
	"context"
	"crypto/sha256"
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

	explicitSeed := explicitGrokCacheSeed(c, body, explicitKey)
	seed := explicitSeed
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
	identity := generateSessionUUID(isolatedSeed)
	if c != nil {
		// Keep explicit conversation evidence associated with this exact cache
		// identity. Content-derived cache reuse cannot establish a conversation,
		// and a later WS turn must not inherit the first turn's unrelated seed.
		c.Set(grokConversationCacheSeedKey(identity), explicitSeed)
	}
	return identity
}

func grokConversationCacheSeedKey(identity string) string {
	return "grok_conversation_cache_seed:" + identity
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
	if billing, err := grokAccountBillingSnapshot(account); err == nil && billing != nil {
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
	if snapshot, err := grokAccountQuotaSnapshot(account); err == nil && snapshot != nil {
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
	RequestID      string
	AgentID        string
}

func resolveGrokRequestConversation(ctx context.Context, c *gin.Context, account *Account, body []byte, cacheKey string) (grokConversationSnapshot, error) {
	result := grokConversationSnapshot{}
	tenant := getAPIKeyIDFromContext(c)
	if tenant <= 0 || isOpenAIResponsesCompactPath(c) {
		return result, nil
	}
	isolate := func(seed string, preserveType bool) (string, error) {
		if strings.TrimSpace(seed) == "" {
			return "", nil
		}
		version, prefix := 7, ""
		// Official resumed sessions may be UUIDv4; auxiliary conversations can
		// be labels followed by a UUIDv4. Preserve their type without forwarding
		// their caller-controlled identifier or merging them into the parent.
		if parsed, err := uuid.Parse(seed); preserveType && err == nil && parsed.Version() == 4 {
			version = 4
		}
		if preserveType && len(seed) > 37 && len(seed) < 100 && seed[len(seed)-37] == '-' {
			if parsed, err := uuid.Parse(seed[len(seed)-36:]); err == nil && parsed.Version() == 4 {
				candidate := seed[:len(seed)-36]
				valid := true
				for _, char := range candidate {
					if char != '-' && (char < 'a' || char > 'z') {
						valid = false
						break
					}
				}
				if valid {
					prefix, version = candidate, 4
				}
			}
		}
		// The same raw value may arrive as a resumed v4 session or as an
		// explicit cache seed for a new v7 session. They cannot share a stored
		// UUID. A new namespace also avoids previously mixed-version records.
		id, err := resolveOutboundMappedID(ctx, "grok-conversation-v4", fmt.Sprintf("uuid:%d:tenant:%d:owner:%s:seed:%s", version, tenant, grokSessionOwnerScope(account), strings.TrimSpace(seed)), version)
		return prefix + id, err
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
	fromCache := session == ""
	if fromCache {
		if c != nil {
			if cacheKey == "" && ctx != nil {
				cacheKey, _ = ctx.Value(grokConversationCacheIdentityKey{}).(string)
			}
			session = c.GetString(grokConversationCacheSeedKey(cacheKey))
		}
	}
	if session == "" {
		id, err := grokEphemeralConversationSession(ctx, c, account, tenant)
		if err != nil {
			return result, err
		}
		result.SessionID = id
	} else {
		if scoped, err := isolate(session, !fromCache); err != nil {
			return result, err
		} else if scoped != "" {
			result.SessionID = scoped
		}
	}
	if scoped, err := isolate(conv, true); err != nil {
		return result, err
	} else if scoped != "" {
		result.ConversationID = scoped
	} else {
		result.ConversationID = result.SessionID
	}
	result.RootSessionID = result.SessionID
	if scoped, err := isolate(root, true); err != nil {
		return result, err
	} else if scoped != "" {
		result.RootSessionID = scoped
	}
	return result, nil
}

type grokEphemeralConversationKey struct {
	tenant int64
	owner  string
	turn   int
}

// Without a reliable conversation declaration, only this accepted logical
// request and its retries/auxiliary prompts may share execution state. Content
// remains useful for cache routing but never becomes a persistent session seed.
func grokEphemeralConversationSession(ctx context.Context, c *gin.Context, account *Account, tenant int64) (string, error) {
	ctx = WithOutboundIdentityScope(ctx, c)
	turn, _ := ctx.Value(grokLogicalTurnKey{}).(int)
	key := grokEphemeralConversationKey{tenant: tenant, owner: grokSessionOwnerScope(account), turn: turn}
	candidate, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create Grok request session: %w", err)
	}
	actual, _ := outboundIdentityScopeFromContext(ctx).grokRequests.LoadOrStore(key, candidate.String())
	id, ok := actual.(string)
	if !ok {
		return "", fmt.Errorf("grok conversation state is invalid")
	}
	return id, nil
}

func grokSessionOwnerScope(account *Account) string {
	if account == nil {
		return "probe"
	}
	owner := credentialOwnerKey(account)
	if account.Type == AccountTypeAPIKey {
		key := sha256.Sum256([]byte(account.GetCredential("api_key")))
		return fmt.Sprintf("%d:%s:%x", owner.id, account.Type, key)
	}
	return fmt.Sprintf("%d:%s:%s:%s", owner.id, account.Type, account.GetCredential("sub"), account.GetCredential("team_id"))
}

type grokLogicalTurnKey struct{}
type grokLogicalOperationKey struct{}
type grokConversationCacheIdentityKey struct{}

func withGrokConversationCacheIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, grokConversationCacheIdentityKey{}, identity)
}

func withGrokLogicalTurn(ctx context.Context, turn int) context.Context {
	return context.WithValue(ctx, grokLogicalTurnKey{}, turn)
}

// Auxiliary sampler calls share the parent conversation but create independent
// prompts. Keep their operation stable across retries of the same logical turn.
func withGrokLogicalOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, grokLogicalOperationKey{}, operation)
}

func applyGrokRequestMetadataForRequest(req *http.Request, c *gin.Context, account *Account, body []byte, cacheKey string) error {
	ctx := WithOutboundIdentityScope(req.Context(), c)
	owner := grokResponseOwner(ctx, account)
	if owner == nil {
		return fmt.Errorf("grok request credential owner unavailable")
	}
	conversation, err := resolveGrokRequestConversation(ctx, c, owner, body, cacheKey)
	if err != nil {
		return err
	}
	// A gateway installation is the virtual device. Its persisted random seed
	// replaces host machine data while retaining the official UUIDv5 lifetime.
	device, err := resolveOutboundMappedID(ctx, "grok-device", "installation", 4)
	if err != nil {
		return err
	}
	conversation.AgentID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(device)).String()
	scope := outboundIdentityScopeFromContext(ctx)
	turn, _ := ctx.Value(grokLogicalTurnKey{}).(int)
	operation, _ := ctx.Value(grokLogicalOperationKey{}).(string)
	key := fmt.Sprintf("%s:%d:%s", grokSessionOwnerScope(owner), turn, operation)
	id, _ := scope.grokRequests.LoadOrStore(key, uuid.NewString())
	requestID, ok := id.(string)
	if !ok {
		return fmt.Errorf("grok request state is invalid")
	}
	conversation.RequestID = requestID
	*req = *req.WithContext(ctx)
	applyGrokRequestMetadata(req.Header, body, conversation, owner.GetCredential("sub"))
	return nil
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
	headers.Set("x-grok-req-id", conversation.RequestID)
	headers.Set("x-grok-agent-id", conversation.AgentID)
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
func applyGrokMediaSessionHeader(req *http.Request, c *gin.Context, account *Account) error {
	owner := grokResponseOwner(req.Context(), account)
	if owner == nil {
		return fmt.Errorf("grok media credential owner unavailable")
	}
	conversation, err := resolveGrokRequestConversation(req.Context(), c, owner, nil, "")
	if err != nil {
		return err
	}
	if conversation.SessionID != "" {
		req.Header.Set("X-Grok-Session-Id", conversation.SessionID)
	}
	return nil
}

// stripGrokChatPromptCacheKey removes the Responses-only body field after it
// has been used as an identity seed. Chat Completions routes cache by header.
func stripGrokChatPromptCacheKey(body []byte) ([]byte, error) {
	if !gjson.GetBytes(body, "prompt_cache_key").Exists() {
		return body, nil
	}
	return sjson.DeleteBytes(body, "prompt_cache_key")
}
