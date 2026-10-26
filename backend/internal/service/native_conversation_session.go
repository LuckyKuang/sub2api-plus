package service

import (
	"context"
	"errors"
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
	if account == nil || account.Platform != PlatformDeepseek && account.Platform != PlatformMiniMax && account.Platform != PlatformZhipu {
		return
	}
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil {
		return
	}
	scope.conversationOnce.Do(func() {
		seed := explicitOpenAIRequestSessionID(c, body)
		if account.Platform == PlatformZhipu {
			seed = strings.TrimPrefix(strings.TrimPrefix(seed, "sess_"), "subagent_agent_")
			if c != nil {
				if query := strings.TrimPrefix(NormalizeClientSessionID(c.GetHeader("x-query-id")), "query_"); query != "" {
					scope.zcodeQuery = OpenAISessionRouteDigest(scopeOpenAISchedulingSeed(c, seed+"\x00"+query))
				}
			}
		}
		if seed != "" {
			scope.conversation = OpenAISessionRouteDigest(scopeOpenAISchedulingSeed(c, seed))
		}
	})
}

func nativeConversationOwnerSession(ctx context.Context, account *Account) (string, error) {
	ownerScope := domesticCredentialOwnerScope(ctx, account)
	if account != nil && ownerScope == "" {
		return "", errors.New("native conversation credential owner is unavailable")
	}
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil || scope.conversation == "" || account == nil || account.ID <= 0 {
		return renderNativeConversationSession(account, uuid.NewString()), nil
	}
	seed := "sub2api:native-conversation:v2\x00" + ownerScope + "\x00" + scope.conversation
	id, err := resolveOutboundMappedID(ctx, "native-conversation", seed, 4)
	if err != nil {
		return "", err
	}
	return renderNativeConversationSession(account, id), nil
}

func renderNativeConversationSession(account *Account, id string) string {
	if account == nil {
		return id
	}
	switch account.Platform {
	case PlatformDeepseek:
		return "session-" + id
	case PlatformMiniMax:
		return "mvs_" + strings.ReplaceAll(id, "-", "")
	}
	return id
}
