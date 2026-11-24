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
	key           string
	status        int
}

// SetOpenAIErrorMessageSource keeps only the code needed for display, never the
// private upstream body. Call only when committing a final error, not while
// classifying retry candidates:
// a recovered account must not supply the source for a later failure.
func SetOpenAIErrorMessageSource(c *gin.Context, payload []byte) {
	if c == nil {
		return
	}
	status := c.Writer.Status()
	if status < 400 {
		status = 0
	}
	source := openAIErrorMessageSource{status: status}
	if code := openAIErrorMessageCode(payload); code != "" {
		source.code = code
		if key, ok := codexPublicErrorKeyFromCode(code); ok {
			source.key = key
		}
	}
	c.Set("openai_error_message_source", source)
}

// SetCodexPublicErrorKey records the trusted failure classification before any
// downstream text is rewritten. It takes precedence over a wire code that may
// be ambiguous or shared by several catalog entries.
func SetCodexPublicErrorKey(c *gin.Context, key string) {
	if c == nil {
		return
	}
	source := getOpenAIErrorMessageSource(c)
	if definition, ok := codexPublicErrorByKey(key); ok {
		source.key = definition.Key
	}
	c.Set("openai_error_message_source", source)
}

func openAIErrorMessageCode(payload []byte) string {
	if json.Valid(payload) {
		_, object := openAIProtocolError(gjson.ParseBytes(payload))
		candidate := object.Get("code")
		if candidate.Type == gjson.String {
			return strings.TrimSpace(candidate.String())
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
	source := getOpenAIErrorMessageSource(c)
	if e.StatusCode >= 400 {
		source.status = e.StatusCode
	}
	if e.openAIErrorMessageCode != "" {
		source.code = e.openAIErrorMessageCode
		if key, ok := codexPublicErrorKeyFromCode(e.openAIErrorMessageCode); ok {
			source.key = key
		}
	}
	c.Set("openai_error_message_source", source)
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
	return normalizeOpenAIErrorMessageWithResolver(payload, source, func(key string) string {
		definition, ok := codexPublicErrorByKey(key)
		if !ok {
			return ""
		}
		return definition.DefaultMessage
	})
}

func normalizeOpenAIErrorMessageWithResolver(payload []byte, source openAIErrorMessageSource, resolve func(string) string) []byte {
	if !json.Valid(payload) {
		return payload
	}
	path, errorObject := openAIProtocolError(gjson.ParseBytes(payload))
	if !errorObject.IsObject() {
		return payload
	}
	key := strings.TrimSpace(source.key)
	if _, ok := codexPublicErrorByKey(key); !ok {
		key = ""
	}
	if key == "" {
		if code := strings.TrimSpace(errorObject.Get("code").String()); code != "" {
			if resolved, ok := codexPublicErrorKeyFromCode(code); ok {
				key = resolved
			}
		}
	}
	if key == "" {
		key = codexPublicErrorFallbackKey(source.status)
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok || definition.PublicCode == "" {
		return payload
	}
	message := ""
	if resolve != nil {
		message = strings.TrimSpace(resolve(key))
	}
	if message == "" {
		message = definition.DefaultMessage
	}
	replacement := []byte("{}")
	var err error
	if replacement, err = sjson.SetBytes(replacement, "message", message); err != nil {
		return payload
	}
	if definition.PublicCode != "" {
		if replacement, err = sjson.SetBytes(replacement, "code", definition.PublicCode); err != nil {
			return payload
		}
	}
	if definition.ErrorType != "" {
		if replacement, err = sjson.SetBytes(replacement, "type", definition.ErrorType); err != nil {
			return payload
		}
	}
	if path == "" {
		root := gjson.ParseBytes(payload)
		if strings.EqualFold(root.Get("type").String(), "error") {
			// Error events keep their protocol envelope. Only the message and
			// public code are replaced; sequence/status stay for the client's
			// correlation and terminal handling.
			event := []byte("{}")
			event, _ = sjson.SetBytes(event, "type", "error")
			event, _ = sjson.SetBytes(event, "message", message)
			if definition.PublicCode != "" {
				event, _ = sjson.SetBytes(event, "code", definition.PublicCode)
			}
			for _, field := range []string{"sequence_number", "status"} {
				if value := root.Get(field); value.Exists() && value.Type != gjson.Null {
					event, _ = sjson.SetRawBytes(event, field, []byte(value.Raw))
				}
			}
			return event
		}
		return replacement
	}
	result, err := sjson.SetRawBytes(payload, path, replacement)
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
		enabled := func() bool {
			return c.GetString(string(ctxkey.Platform)) == PlatformOpenAI
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
					return normalizeOpenAIErrorMessageWithResolver(payload, getOpenAIErrorMessageSource(c), func(key string) string {
						return settings.CodexPublicErrorMessage(ctx, key)
					})
				}
				return payload
			},
			stripDiagnosticHeaders: true,
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

func (s *OpenAIGatewayService) projectCodexPublicErrorPayload(ctx context.Context, payload []byte) []byte {
	if s == nil || s.settingService == nil {
		return payload
	}
	source := openAIErrorMessageSource{}
	if code := openAIErrorMessageCode(payload); code != "" {
		source.code = code
		if key, ok := codexPublicErrorKeyFromCode(code); ok {
			source.key = key
		}
	}
	return normalizeOpenAIErrorMessageWithResolver(payload, source, func(key string) string {
		return s.settingService.CodexPublicErrorMessage(ctx, key)
	})
}

func (s *OpenAIGatewayService) projectOpenAIWebSocketOutput(ctx context.Context, c *gin.Context, account *Account, payload []byte) []byte {
	payload = s.projectCodexSafetyBufferingOutput(ctx, c, account, payload, "websocket")
	if account != nil && account.Platform == PlatformOpenAI && s.settingService != nil {
		return normalizeOpenAIErrorMessageWithResolver(payload, getOpenAIErrorMessageSource(c), func(key string) string {
			return s.settingService.CodexPublicErrorMessage(ctx, key)
		})
	}
	return payload
}
