//go:build unit || !integration

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Fixtures follow deepseek-harness 22ddee3 serialize.ts and MiniMax
// da22f11 messages-file-api-patcher.ts. File identifiers and tool references
// are opaque; only the current direct-user text is an audit policy input.
func TestDomesticProviderMediaAndToolChangesRetainCurrentUserAudit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		images []string
	}{
		{
			name: "DeepSeek file image after deferred tool changes",
			body: `{"model":"deepseek-v4.1","max_tokens":64,"tools":[{"name":"lookup","description":"historical tool definition","input_schema":{"type":"object"},"defer_loading":true}],"messages":[{"role":"user","content":"historical request"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"lookup"}},{"type":"tool_removal","tool":{"type":"tool_reference","name":"old_tool"}}]},{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file-test"}},{"type":"text","text":"inspect current attachment"}]}]}`,
		},
		{
			name:   "DeepSeek inline image fallback",
			body:   `{"model":"deepseek-v4.1","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"text","text":"inspect current attachment"}]}]}`,
			images: []string{"data:image/png;base64,AAAA"},
		},
		{
			name:   "MiniMax inline image and native video",
			body:   `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"video","source":{"type":"base64","media_type":"video/mp4","data":"BBBB"}},{"type":"text","text":"inspect current attachment"}]}]}`,
			images: []string{"data:image/png;base64,AAAA"},
		},
		{
			name: "MiniMax video MIME fallback in image block",
			body: `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"video/mp4","data":"BBBB"}},{"type":"text","text":"inspect current attachment"}]}]}`,
		},
		{
			name: "MiniMax uploaded image and video references",
			body: `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"mm_file://image-test"}},{"type":"video","source":{"type":"url","url":"mm_file://video-test"}},{"type":"text","text":"inspect current attachment"}]}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolAnthropicMessages, []byte(tc.body))
			require.Equal(t, "inspect current attachment", moderation.Text)
			if len(tc.images) == 0 {
				require.Empty(t, moderation.Images)
			} else {
				require.Equal(t, tc.images, moderation.Images)
			}
			prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: service.ContentModerationProtocolAnthropicMessages, Body: []byte(tc.body),
			})
			require.NoError(t, err)
			require.Equal(t, "inspect current attachment", prompt.ScanText)
		})
	}
}

func TestDomesticProviderOpaquePayloadsPassBlockingAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, platform, body string }{
		{"DeepSeek file image", service.PlatformDeepseek, `{"model":"deepseek-v4.1","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file-test"}}]}]}`},
		{"DeepSeek final system tool changes", service.PlatformDeepseek, `{"model":"deepseek-v4.1","max_tokens":64,"messages":[{"role":"user","content":"historical request"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"lookup"}},{"type":"tool_removal","tool":{"type":"tool_reference","name":"old_tool"}}]}]}`},
		{"MiniMax image and video references", service.PlatformMiniMax, `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"mm_file://image-test"}},{"type":"video","source":{"type":"url","url":"mm_file://video-test"}}]}]}`},
		{"MiniMax inline video", service.PlatformMiniMax, `{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":[{"type":"video","source":{"type":"base64","media_type":"video/mp4","data":"BBBB"}}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolAnthropicMessages, []byte(tc.body))
			require.Empty(t, moderation.Text)
			require.Empty(t, moderation.Images, "provider file identifiers and video bytes are not fetchable audit images")
			_, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolAnthropicMessages, Body: []byte(tc.body)})
			require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
			// These are shared audit-hook stages, not new native provider WS routes.
			for _, stage := range []string{"http", "first_turn", "subsequent_turn"} {
				t.Run(stage, func(t *testing.T) {
					metrics := securityaudit.NewAtomicMetrics()
					prompt := securityaudit.NewPromptService(blockingCompatibilityConfigStore{cfg: securityaudit.ActiveConfig{
						RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true,
						Scanners: []string{"pii"}, Endpoints: []securityaudit.ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}},
					}}, nil, nil, nil, metrics)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
					groupID := int64(3)
					apiKey := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: tc.platform}}
					decision := runSecurityAudit(c, nil, securityaudit.NewCoordinator(nil, prompt), nil, apiKey,
						middleware.AuthSubject{UserID: 7, Concurrency: 1}, service.ContentModerationProtocolAnthropicMessages,
						"native-model", []byte(tc.body), stage)
					require.NotNil(t, decision)
					require.Equal(t, securityaudit.DecisionAllow, decision.Kind)
					require.True(t, decision.AllowNextStage)
					require.False(t, c.Writer.Written(), "opaque content must not commit an audit error or close response")
					require.Equal(t, int64(1), metrics.AuditSnapshot().ExtractionAttempted)
				})
			}
		})
	}
}
