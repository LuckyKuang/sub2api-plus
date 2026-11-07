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

type openAIErrorMessageSource struct {
	code          string
	generatedCode string
}

func openAIErrorMessage(code string) string {
	switch code {
	case "upstream_response_interrupted", "upstream_unavailable":
		return "响应已中断，请输入「继续」重新提交。"
	case "session_account_lost":
		return "当前会话可能无法恢复，请新开会话继续。"
	default:
		return ""
	}
}

// SetOpenAIErrorMessageSource keeps only the code needed for display, never the
// private upstream body. Call only when committing a final error, not while
// classifying retry candidates:
// a recovered account must not supply the source for a later failure.
func SetOpenAIErrorMessageSource(c *gin.Context, payload []byte) {
	if c == nil {
		return
	}
	c.Set("openai_error_message_source", openAIErrorMessageSource{code: openAIErrorMessageCode(payload)})
}

func openAIErrorMessageCode(payload []byte) string {
	if json.Valid(payload) {
		_, object := openAIProtocolError(gjson.ParseBytes(payload))
		candidate := object.Get("code")
		if candidate.Type == gjson.String && object.Get("message").Type == gjson.String && openAIErrorMessage(candidate.String()) != "" {
			return candidate.String()
		}
	}
	return ""
}

func getOpenAIErrorMessageSource(c *gin.Context) openAIErrorMessageSource {
	if c == nil {
		return openAIErrorMessageSource{}
	}
	value, _ := c.Get("openai_error_message_source")
	source, _ := value.(openAIErrorMessageSource)
	return source
}

// SetOpenAIErrorMessageGeneratedCode identifies a locally synthesized terminal
// code. It permits message projection without changing that wire code, while
// explicit upstream codes retain precedence.
func SetOpenAIErrorMessageGeneratedCode(c *gin.Context, code string) {
	if c == nil {
		return
	}
	source := getOpenAIErrorMessageSource(c)
	source.generatedCode = code
	c.Set("openai_error_message_source", source)
}

// SetOpenAIErrorMessageSource retains the final attempt's original display
// classification even if stream failover had to sanitize ResponseBody earlier.
func (e *UpstreamFailoverError) SetOpenAIErrorMessageSource(c *gin.Context) {
	if e == nil || c == nil {
		return
	}
	SetOpenAIErrorMessageSource(c, e.ResponseBody)
	if e.openAIErrorMessageCode != "" {
		c.Set("openai_error_message_source", openAIErrorMessageSource{code: e.openAIErrorMessageCode})
	}
}

func openAIProtocolError(root gjson.Result) (string, gjson.Result) {
	if !root.IsObject() {
		return "", gjson.Result{}
	}
	path := ""
	switch root.Get("type").String() {
	case "error":
		if root.Get("error").IsObject() {
			path = "error"
		}
	case "response.failed":
		path = "response.error"
		if !root.Get(path).IsObject() {
			path = "error"
		}
	case "":
		path = "error"
	default:
		return "", gjson.Result{}
	}
	if path != "" {
		return path, root.Get(path)
	}
	return path, root
}

func normalizeOpenAIErrorMessage(payload []byte, source openAIErrorMessageSource) []byte {
	if !json.Valid(payload) {
		return payload
	}
	path, errorObject := openAIProtocolError(gjson.ParseBytes(payload))
	if !errorObject.IsObject() || errorObject.Get("message").Type != gjson.String {
		return payload
	}
	code := errorObject.Get("code")
	sourceCode := source.code
	if code.Exists() {
		// Only an explicitly marked locally generated code may use the source;
		// other explicit unknown/non-string downstream codes take precedence.
		if code.Type != gjson.String {
			return payload
		}
		if sourceCode == "" || source.generatedCode == "" || code.String() != source.generatedCode {
			sourceCode = code.String()
		}
	}
	message := openAIErrorMessage(sourceCode)
	if message == "" {
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
					return normalizeOpenAIErrorMessage(payload, getOpenAIErrorMessageSource(c))
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
		return normalizeOpenAIErrorMessage(payload, getOpenAIErrorMessageSource(c))
	}
	return payload
}
