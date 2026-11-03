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
