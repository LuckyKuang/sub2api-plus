package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func nativeConversationRequestPlatform(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if c.Request != nil {
		if platform, ok := ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	if value, ok := c.Get("api_key"); ok {
		if key, ok := value.(*APIKey); ok && key != nil && key.Group != nil {
			return key.Group.Platform
		}
	}
	return ""
}

// Official Harness and MiniMax session declarations are routing signals only;
// they cannot select a trusted outbound client or cross a tenant namespace.
func nativeConversationHeaderSessionID(c *gin.Context, platform string) string {
	if c == nil {
		return ""
	}
	var header string
	switch platform {
	case PlatformDeepseek:
		header = "X-Deepseek-Harness-Session-Id"
	case PlatformMiniMax:
		header = "X-Mavis-Session-Id"
	default:
		return ""
	}
	return strings.TrimSpace(c.GetHeader(header))
}

// Capture the original logical conversation before provider transforms change
// model, history or cache fields. Handler-level retries reuse this frozen seed.
func captureNativeConversationScope(ctx context.Context, c *gin.Context, account *Account, body []byte) {
	if account == nil || account.Platform != PlatformDeepseek && account.Platform != PlatformMiniMax {
		return
	}
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil {
		return
	}
	scope.conversationOnce.Do(func() {
		seed := explicitOpenAIRequestSessionID(c, body)
		if seed == "" {
			seed = deriveOpenAIContentSessionSeed(body)
		}
		if seed != "" {
			scope.conversation = OpenAISessionRouteDigest(scopeOpenAISchedulingSeed(c, seed))
		}
	})
}

func nativeConversationOwnerSession(ctx context.Context, account *Account) string {
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil || scope.conversation == "" || account == nil || account.ID <= 0 {
		return uuid.NewString()
	}
	ownerID := account.ID
	if account.IsShadow() && account.ParentAccountID != nil {
		ownerID = *account.ParentAccountID
	}
	seed := "sub2api:native-conversation:v1\x00" + account.Platform + "\x00" + strconv.FormatInt(ownerID, 10) + "\x00" + scope.conversation
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed)).String()
}
