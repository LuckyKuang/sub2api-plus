//go:build unit

package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeConversationTestContext(platform, header, session string, keyID int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set(header, session)
	c.Set("api_key", &APIKey{ID: keyID, UserID: keyID + 100, Group: &Group{Platform: platform}})
	return c
}

func TestNativeConversationSessionRoutingAndOwnerIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct{ platform, header string }{
		{PlatformDeepseek, "X-Deepseek-Harness-Session-Id"},
		{PlatformMiniMax, "X-Mavis-Session-Id"},
	} {
		t.Run(tt.platform, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			body := []byte(`{"model":"native","messages":[{"role":"user","content":"first"}]}`)
			nextBody := []byte(`{"model":"native","messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"second"}]}`)
			c := nativeConversationTestContext(tt.platform, tt.header, "official-conversation", 11)
			next := nativeConversationTestContext(tt.platform, tt.header, "official-conversation", 11)
			other := nativeConversationTestContext(tt.platform, tt.header, "other-conversation", 11)
			otherTenant := nativeConversationTestContext(tt.platform, tt.header, "official-conversation", 12)
			require.Equal(t, svc.GenerateSessionHash(c, body), svc.GenerateSessionHash(next, nextBody))
			require.NotEqual(t, svc.GenerateSessionHash(c, body), svc.GenerateSessionHash(other, body))
			require.NotEqual(t, svc.GenerateSessionHash(c, body), svc.GenerateSessionHash(otherTenant, body))
			for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
				a := &Account{ID: 41, Platform: tt.platform, Type: kind}
				b := &Account{ID: 42, Platform: tt.platform, Type: kind}
				project := func(c *gin.Context, account *Account, body []byte) string {
					ctx := WithOutboundIdentityScope(context.Background(), c)
					captureNativeConversationScope(ctx, c, account, body)
					id, err := nativeConversationOwnerSession(ctx, account)
					require.NoError(t, err)
					return id
				}
				first := project(c, a, body)
				require.NotEqual(t, "official-conversation", first)
				idText := strings.TrimPrefix(strings.TrimPrefix(first, "session-"), "mvs_")
				parsed, err := uuid.Parse(idText)
				require.NoError(t, err)
				require.Equal(t, uuid.Version(4), parsed.Version())
				require.Equal(t, first, project(next, a, nextBody))
				require.NotEqual(t, first, project(other, a, body))
				require.NotEqual(t, first, project(otherTenant, a, body))
				second := project(c, b, body)
				require.NotEqual(t, first, second)
				require.Equal(t, second, project(next, b, nextBody))
			}
			// Provider-specific routing signals cannot influence a foreign group.
			foreign := nativeConversationTestContext(PlatformOpenAI, tt.header, "one", 11)
			foreignNext := nativeConversationTestContext(PlatformOpenAI, tt.header, "two", 11)
			require.Equal(t, svc.GenerateSessionHash(foreign, body), svc.GenerateSessionHash(foreignNext, body))
			owner := &Account{ID: 41, Platform: tt.platform, Type: AccountTypeAPIKey}
			projectForeign := func(c *gin.Context) string {
				ctx := WithOutboundIdentityScope(context.Background(), c)
				captureNativeConversationScope(ctx, c, owner, body)
				id, err := nativeConversationOwnerSession(ctx, owner)
				require.NoError(t, err)
				return id
			}
			require.NotEqual(t, projectForeign(foreign), projectForeign(foreignNext), "without a native conversation scope separate requests mint independent sessions")
		})
	}
}

func TestNativeConversationSessionSurvivesRealForwardingTurnsAndFailover(t *testing.T) {
	for _, tt := range []struct{ platform, header string }{
		{PlatformDeepseek, "X-Deepseek-Harness-Session-Id"},
		{PlatformMiniMax, "X-Mavis-Session-Id"},
	} {
		t.Run(tt.platform, func(t *testing.T) {
			forward := func(owner int64, turn string) string {
				body := []byte(`{"model":"native","max_tokens":32,"messages":[{"role":"user","content":"` + turn + `"}]}`)
				c := nativeConversationTestContext(tt.platform, tt.header, "stable-client-session", 11)
				c.Request.Body = http.NoBody
				upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := nativeAnthropicTestAccount()
				account.ID, account.Platform = owner, tt.platform
				_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				return upstream.lastReq.Header.Get(tt.header)
			}
			first := forward(41, "hello")
			require.NotEmpty(t, first)
			require.Equal(t, first, forward(41, "next"))
			second := forward(42, "hello")
			require.NotEqual(t, first, second)
			require.Equal(t, second, forward(42, "next"))
		})
	}
}

func TestNativeConversationScopeCapturesOriginalBodyOnce(t *testing.T) {
	c := nativeConversationTestContext(PlatformDeepseek, "Content-Type", "application/json", 11)
	a := &Account{ID: 41, Platform: PlatformDeepseek}
	ctx := WithOutboundIdentityScope(context.Background(), c)
	body := []byte(`{"model":"native","messages":[{"role":"user","content":"hello"}]}`)
	captureNativeConversationScope(ctx, c, a, body)
	first, err := nativeConversationOwnerSession(ctx, a)
	require.NoError(t, err)
	captureNativeConversationScope(ctx, c, a, bytes.ReplaceAll(body, []byte("hello"), []byte("rewritten")))
	second, err := nativeConversationOwnerSession(ctx, a)
	require.NoError(t, err)
	require.NotEqual(t, first, second, "no explicit conversation does not persist an arbitrary content-derived session")
}
