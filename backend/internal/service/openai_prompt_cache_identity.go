package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openAIAlignedPromptCacheIdentityKey = "openai_aligned_prompt_cache_identity"

type openAIAlignedPromptCacheIdentity struct {
	Identity string
	RawSeed  string
	Scope    string
}

// ensureOpenAIResponsesPromptCacheIdentity resolves a single opaque identity
// for the Responses body and upstream session headers. Explicit client values
// win; content fallback is limited to requests with a meaningful user anchor.
func (s *OpenAIGatewayService) ensureOpenAIResponsesPromptCacheIdentity(
	c *gin.Context,
	account *Account,
	body []byte,
	seed string,
	effectiveModel string,
) ([]byte, string, bool, error) {
	if len(body) == 0 {
		return body, "", false, nil
	}

	rawIdentity := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	if rawIdentity == "" {
		rawIdentity = strings.TrimSpace(seed)
	}
	if rawIdentity == "" {
		rawIdentity = explicitOpenAIHeaderSessionID(c)
	}
	if rawIdentity == "" && shouldAutoInjectPromptCacheKeyForCompat(effectiveModel) {
		rawIdentity = deriveOpenAIAnchoredContentSessionSeed(body)
	}
	if rawIdentity == "" {
		return body, "", false, nil
	}

	// Internal retries do not always carry the finalized body forward. The
	// Chat-Completions compatibility recovery path, for example, rebuilds the
	// Responses body from the original request while passing the finalized key
	// as seed. Treat the request-scoped marker as authoritative regardless of
	// whether the identity came from the body or the seed: keep it unchanged in
	// the same scope, or recover its original client/content seed before a
	// failover applies a different sharing scope.
	if aligned, ok := getOpenAIAlignedPromptCacheIdentity(c); ok && aligned.Identity == rawIdentity {
		if aligned.Scope == openAIPromptCacheIdentityScope(c, account) {
			if existing := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); existing == aligned.Identity {
				return body, aligned.Identity, false, nil
			}
			normalized, err := sjson.SetBytes(body, "prompt_cache_key", aligned.Identity)
			if err != nil {
				return body, "", false, fmt.Errorf("set aligned prompt_cache_key: %w", err)
			}
			return normalized, aligned.Identity, true, nil
		}
		if aligned.RawSeed != "" {
			rawIdentity = aligned.RawSeed
		}
	}

	identity, err := s.resolveOpenAIPromptCacheIdentity(c, account, rawIdentity)
	if err != nil {
		return body, "", false, err
	}
	if identity == "" {
		return body, "", false, nil
	}
	markOpenAIAlignedPromptCacheIdentity(c, account, rawIdentity, identity)

	if existing := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); existing == identity {
		return body, identity, false, nil
	}
	normalized, err := sjson.SetBytes(body, "prompt_cache_key", identity)
	if err != nil {
		return body, "", false, fmt.Errorf("set prompt_cache_key: %w", err)
	}
	return normalized, identity, true, nil
}

// resolveOpenAIPromptCacheIdentity applies the existing tenant/session-sharing
// namespace and formats it as a deterministic UUID. The result stays well
// below the public 64-character prompt_cache_key limit.
func (s *OpenAIGatewayService) resolveOpenAIPromptCacheIdentity(c *gin.Context, account *Account, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	var (
		namespaced string
		err        error
	)
	if account != nil && account.IsOpenAIOAuthSessionSharingEnabled() {
		namespaced, err = s.resolveOpenAIUpstreamSessionID(c, account, raw)
	} else {
		namespaced = isolateOpenAIUpstreamSessionID(
			getAPIKeyIDFromContext(c),
			codexAccountIdentitySource(c, account),
			raw,
		)
	}
	if err != nil || namespaced == "" {
		return "", err
	}
	return generateSessionUUID(namespaced), nil
}

// resolveOpenAIUpstreamPromptCacheHeaderIdentity avoids hashing a body key a
// second time after ensureOpenAIResponsesPromptCacheIdentity aligned it.
func (s *OpenAIGatewayService) resolveOpenAIUpstreamPromptCacheHeaderIdentity(c *gin.Context, account *Account, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if isOpenAIAlignedPromptCacheIdentityForAccount(c, account, raw) {
		return raw, nil
	}
	if aligned, ok := getOpenAIAlignedPromptCacheIdentity(c); ok && aligned.Identity == raw && aligned.RawSeed != "" {
		raw = aligned.RawSeed
	}
	return s.resolveOpenAIPromptCacheIdentity(c, account, raw)
}

func markOpenAIAlignedPromptCacheIdentity(c *gin.Context, account *Account, rawSeed, identity string) {
	if c == nil {
		return
	}
	identity = strings.TrimSpace(identity)
	if identity != "" {
		c.Set(openAIAlignedPromptCacheIdentityKey, openAIAlignedPromptCacheIdentity{
			Identity: identity,
			RawSeed:  strings.TrimSpace(rawSeed),
			Scope:    openAIPromptCacheIdentityScope(c, account),
		})
	}
}

func isOpenAIAlignedPromptCacheIdentity(c *gin.Context, identity string) bool {
	aligned, ok := getOpenAIAlignedPromptCacheIdentity(c)
	return ok && aligned.Identity == strings.TrimSpace(identity)
}

func isOpenAIAlignedPromptCacheIdentityForAccount(c *gin.Context, account *Account, identity string) bool {
	aligned, ok := getOpenAIAlignedPromptCacheIdentity(c)
	return ok &&
		aligned.Identity == strings.TrimSpace(identity) &&
		aligned.Scope == openAIPromptCacheIdentityScope(c, account)
}

func getOpenAIAlignedPromptCacheIdentity(c *gin.Context) (openAIAlignedPromptCacheIdentity, bool) {
	if c == nil {
		return openAIAlignedPromptCacheIdentity{}, false
	}
	value, ok := c.Get(openAIAlignedPromptCacheIdentityKey)
	if !ok {
		return openAIAlignedPromptCacheIdentity{}, false
	}
	aligned, ok := value.(openAIAlignedPromptCacheIdentity)
	if !ok || strings.TrimSpace(aligned.Identity) == "" {
		return openAIAlignedPromptCacheIdentity{}, false
	}
	aligned.Identity = strings.TrimSpace(aligned.Identity)
	aligned.RawSeed = strings.TrimSpace(aligned.RawSeed)
	return aligned, true
}

func openAIPromptCacheIdentityScope(c *gin.Context, account *Account) string {
	if account != nil && account.IsOpenAIOAuthSessionSharingEnabled() {
		policy, _, _ := account.OpenAIOAuthSessionPolicy()
		return fmt.Sprintf(
			"oauth-account:%d:scope:%s:user:%d",
			account.OpenAIOAuthSessionScopeAccountID(),
			policy.ScopeVersion,
			getAPIKeyUserIDFromContext(c),
		)
	}
	return fmt.Sprintf("api-key:%d", getAPIKeyIDFromContext(c))
}

func setOpenAIUpstreamSessionIdentity(headers http.Header, identity string) {
	if headers == nil {
		return
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return
	}
	// session-id is the current Codex spelling. Keep the underscore alias for
	// older ChatGPT-compatible relays while both carry the same value.
	headers.Set(codexSessionIDHeader, identity)
	headers.Set("session_id", identity)
}

// alignOpenAIUpstreamSessionIdentityFromBody makes the finalized Responses
// prompt_cache_key authoritative after fingerprinting and account-level header
// overrides have run. This preserves Codex's body/header cache identity even
// when an earlier stage supplied a different session identity.
func (s *OpenAIGatewayService) alignOpenAIUpstreamSessionIdentityFromBody( //nolint:unused // compact/session identity alignment helper
	c *gin.Context,
	account *Account,
	headers http.Header,
	body []byte,
) error {
	promptCacheKey := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	if promptCacheKey == "" {
		return nil
	}
	if isOpenAIResponsesCompactPath(c) {
		// The compact endpoint owns its session namespace and the Plus contract
		// keeps the client key opaque. Do not hash or rewrite it through the
		// ordinary Responses cache identity layer.
		setOpenAIUpstreamSessionIdentity(headers, promptCacheKey)
		return nil
	}
	identity, err := s.resolveOpenAIUpstreamPromptCacheHeaderIdentity(c, account, promptCacheKey)
	if err != nil {
		return fmt.Errorf("resolve prompt cache header identity: %w", err)
	}
	setOpenAIUpstreamSessionIdentity(headers, identity)
	return nil
}

func alignOpenAICodexThreadHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	if threadID := strings.TrimSpace(headers.Get("thread-id")); threadID != "" {
		headers.Set("x-client-request-id", threadID)
	}
}

func shouldPreserveOpenAIPromptCacheOptions(account *Account, effectiveModel string) bool {
	return account != nil &&
		account.Platform == PlatformOpenAI &&
		account.Type == AccountTypeAPIKey &&
		isOpenAIGPT56Model(effectiveModel)
}

func normalizeOpenAIPromptCacheControlsForAccount(body []byte, account *Account, effectiveModel string) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}

	normalized := body
	changed := false
	if gjson.GetBytes(normalized, "prompt_cache_retention").Exists() {
		var err error
		normalized, err = sjson.DeleteBytes(normalized, "prompt_cache_retention")
		if err != nil {
			return body, false, fmt.Errorf("delete deprecated prompt_cache_retention: %w", err)
		}
		changed = true
	}
	if gjson.GetBytes(normalized, "prompt_cache_options").Exists() &&
		!shouldPreserveOpenAIPromptCacheOptions(account, effectiveModel) {
		var err error
		normalized, err = sjson.DeleteBytes(normalized, "prompt_cache_options")
		if err != nil {
			return body, false, fmt.Errorf("delete unsupported prompt_cache_options: %w", err)
		}
		changed = true
	}
	return normalized, changed, nil
}
