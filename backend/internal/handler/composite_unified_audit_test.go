//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompositeUnifiedResponsesAuditsOriginalContentBeforeConversion(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformGemini} {
		engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, captureSnapshot: true, decisions: []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionBlock, AllowNextStage: false}}}
		slots := &domesticAuditConcurrencyCache{}
		h := &GatewayHandler{cfg: &config.Config{}, gatewayService: &service.GatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatClaude, 0)}
		body := `{"model":"public-model","input":[{"type":"future_state","opaque":true},{"type":"message","role":"user","content":[{"type":"input_text","text":"blocked original Composite request"}]}]}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		groupID := int64(3)
		key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, ConversionEnabled: true, DefaultClient: "codex", DefaultEndpoint: "/v1/responses", Group: &service.Group{ID: groupID, Platform: service.PlatformComposite}, User: &service.User{ID: 7, Concurrency: 1}}
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
		require.True(t, service.BindCompositeAccessPolicy(c, key))
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), platform))
		moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, []byte(body))
		require.Equal(t, "blocked original Composite request", moderation.Text)
		h.Responses(c)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Contains(t, w.Body.String(), securityaudit.ErrorCodeBlocked)
		require.Equal(t, moderation.Text, engine.capturedScanText())
		require.Equal(t, int64(1), engine.evaluates.Load())
		require.Zero(t, slots.userAcquisitions)
		_, selected := c.Get(opsAccountIDKey)
		require.False(t, selected)
	}
}

// Requirement: every admitted unified Composite endpoint audits the original
// client payload before account selection, concurrency acquisition or any
// upstream write. Responses was already covered; these two endpoints convert
// their payloads, so a regression here would silently audit converted content.
func TestCompositeUnifiedChatAndMessagesAuditOriginalContentBeforeConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, endpoint, path, body string
	}{
		{
			name: "messages", endpoint: "/v1/messages", path: "/v1/messages",
			body: `{"model":"public-model","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"blocked original Composite request"}]}]}`,
		},
		{
			name: "chat completions", endpoint: "/v1/chat/completions", path: "/v1/chat/completions",
			body: `{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"blocked original Composite request"}]}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, captureSnapshot: true,
				decisions: []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionBlock, AllowNextStage: false}}}
			cache := &domesticAuditConcurrencyCache{}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			groupID := int64(3)
			key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, ConversionEnabled: true,
				DefaultClient: "custom", DefaultEndpoint: tc.endpoint,
				Group:         &service.Group{ID: groupID, Platform: service.PlatformComposite},
				User:          &service.User{ID: 7, Concurrency: 1}}
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
			require.True(t, service.BindCompositeAccessPolicy(c, key))
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformAnthropic))

			// Missing downstream repositories deliberately make any reordered
			// selection, billing or upstream write fail instead of satisfying the
			// audit result.
			// An Anthropic target is served by the Anthropic gateway handler, which
			// is the one that converts the Messages/Chat payload for that supplier.
			(&GatewayHandler{
				cfg: &config.Config{}, gatewayService: &service.GatewayService{},
				concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0),
				securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine),
			}).Messages(c)
			if tc.name == "chat completions" {
				(&GatewayHandler{
					cfg: &config.Config{}, gatewayService: &service.GatewayService{},
					concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0),
					securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine),
				}).ChatCompletions(c)
			}

			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
			require.Equal(t, "blocked original Composite request", engine.capturedScanText())
			require.Equal(t, int64(1), engine.evaluates.Load())
			require.Zero(t, cache.userAcquisitions)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected, "the gate must precede account selection")
		})
	}
}
