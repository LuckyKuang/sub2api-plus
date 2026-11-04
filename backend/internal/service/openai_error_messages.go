package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Only known protocol error carriers are projected. Assistant text, tool
// arguments, opaque sibling objects and unknown event types remain untouched.
func normalizeOpenAIErrorMessage(payload []byte) []byte {
	if !json.Valid(payload) {
		return payload
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return payload
	}
	path := ""
	switch root.Get("type").String() {
	case "error":
		path = "error"
		if !root.Get(path).IsObject() {
			path = ""
		}
	case "response.failed":
		path = "response.error"
		if !root.Get(path).IsObject() {
			path = "error"
		}
	case "":
		path = "error" // HTTP JSON error envelope
	default:
		return payload
	}
	errorObject := root
	if path != "" {
		errorObject = root.Get(path)
	}
	if !errorObject.IsObject() || errorObject.Get("code").Type != gjson.String || errorObject.Get("message").Type != gjson.String {
		return payload
	}
	message := ""
	switch errorObject.Get("code").String() {
	case "upstream_response_interrupted", "upstream_unavailable":
		message = "响应已中断，请输入「继续」重新提交。"
	case "session_account_lost":
		message = "当前会话可能无法恢复，请新开会话继续。"
	default:
		return payload
	}
	if path != "" {
		path += "."
	}
	result, err := sjson.SetBytes(payload, path+"message", message)
	if err != nil {
		return payload
	}
	return result
}

// Install beneath the ops capture writer: diagnostics see the original error,
// and handler errors emitted after Forward returns still reach this boundary.
// Account selection remains the handler's responsibility; read its trusted
// current platform only when releasing bytes, including after failover.
func OpenAIErrorMessageOutput(settings *SettingService, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settings == nil {
			next(c)
			return
		}
		ctx := c.Request.Context()
		headers := c.Writer.Header()
		if platform, ok := ctx.Value(ctxkey.Platform).(string); ok {
			c.Set(string(ctxkey.Platform), platform)
		}
		settings.OpenAIErrorMessagesNormalized(ctx)
		enabled := func() bool {
			return c.GetString(string(ctxkey.Platform)) == PlatformOpenAI && settings.OpenAIErrorMessagesNormalized(ctx)
		}
		w := &codexSafetyBufferingHTTPWriter{
			ResponseWriter: c.Writer, ctx: ctx, c: c,
			projectionEnabled: enabled,
			bypassProjection: func() bool {
				contentType := strings.ToLower(headers.Get("Content-Type"))
				return c.GetString(string(ctxkey.Platform)) != PlatformOpenAI || (!strings.HasPrefix(contentType, "application/json") && !isEventStreamResponse(headers))
			},
			projectPayload: func(payload []byte, _ string) []byte {
				if enabled() {
					return normalizeOpenAIErrorMessage(payload)
				}
				return payload
			},
		}
		c.Writer = w
		c.Set("openai_error_message_output_writer", w)
		defer func() {
			if err := w.finish(); err != nil {
				slog.WarnContext(ctx, "OpenAI error message output write failed", "request_id", ctx.Value(ctxkey.RequestID), "endpoint", c.FullPath(), "protocol", "http", "stage", "downstream_output", "code", "openai_error_message_output_write_failed")
			}
			c.Writer = w.ResponseWriter
			c.Set("openai_error_message_output_writer", nil)
		}()
		next(c)
	}
}

func (s *OpenAIGatewayService) projectOpenAIWebSocketOutput(ctx context.Context, c *gin.Context, account *Account, payload []byte) []byte {
	payload = s.projectCodexSafetyBufferingOutput(ctx, c, account, payload, "websocket")
	if account != nil && account.Platform == PlatformOpenAI && s.settingService.OpenAIErrorMessagesNormalized(ctx) {
		return normalizeOpenAIErrorMessage(payload)
	}
	return payload
}
