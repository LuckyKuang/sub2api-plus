//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type domesticAuditConcurrencyCache struct {
	fakeConcurrencyCache
	userAcquisitions int
}

func (c *domesticAuditConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	c.userAcquisitions++
	return true, nil
}

func TestDomesticProviderMessagesAuditBlocksBeforeSelectionAndConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ platform, body string }{
		{service.PlatformDeepseek, `{"model":"deepseek-v4.1","max_tokens":64,"tools":[{"name":"lookup","input_schema":{"type":"object"},"defer_loading":true}],"messages":[{"role":"user","content":"historical request"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"lookup"}}]},{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file-test"}},{"type":"text","text":"blocked current attachment request"}]}]}`},
		{service.PlatformMiniMax, `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"video","source":{"type":"url","url":"mm_file://video-test"}},{"type":"text","text":"blocked current attachment request"}]}]}`},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, captureSnapshot: true,
				decisions: []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionBlock, AllowNextStage: false}}}
			cache := &domesticAuditConcurrencyCache{}
			// Missing downstream repositories deliberately make any reordered
			// selection/billing/write fail instead of satisfying the audit result.
			h := &OpenAIGatewayHandler{
				gatewayService: &service.OpenAIGatewayService{}, billingCacheService: &service.BillingCacheService{},
				apiKeyService: &service.APIKeyService{}, cfg: &config.Config{},
				concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0),
				securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine),
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			groupID := int64(3)
			apiKey := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID,
				Group: &service.Group{ID: groupID, Platform: tc.platform}, User: &service.User{ID: 7, Concurrency: 1}}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
			h.Messages(c)
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
			require.Equal(t, "blocked current attachment request", engine.capturedScanText())
			require.Equal(t, int64(1), engine.evaluates.Load())
			require.Zero(t, cache.userAcquisitions)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected, "the gate must precede both API-key and OAuth account selection")
		})
	}
}
