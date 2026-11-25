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
	// recorded marks a confirmed final failure observed at its real exit. It
	// is never set for a retry candidate or a local denial.
	recorded bool
}

const openAIErrorMessageLocalKey = "openai_error_message_local"

// MarkLocalOpenAIError protects a locally produced user/account error from the
// public upstream projection even after an OpenAI account was selected.
func MarkLocalOpenAIError(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(openAIErrorMessageLocalKey, true)
}

func isLocalOpenAIError(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAIErrorMessageLocalKey)
	marked, _ := value.(bool)
	return ok && marked
}

// SetOpenAIErrorMessageSource keeps only the trusted classification needed for
// display, never the private upstream body. Call only when committing a final
// error, not while classifying retry candidates: a recovered account must not
// supply the source for a later failure. status is the real final upstream
// status, not the downstream writer's not-yet-committed status.
func SetOpenAIErrorMessageSource(c *gin.Context, status int, payload []byte) {
	if c == nil {
		return
	}
	key, _ := ClassifyCodexPublicFailure(status, payload)
	source := openAIErrorMessageSource{status: status, recorded: true, key: key}
	if code := openAIErrorMessageCode(payload); code != "" {
		source.code = code
	}
	c.Set("openai_error_message_source", source)
}

// CodexPublicErrorFinalFields exposes the trusted final classification for a
// direct writer that does not sit under the output middleware.
func CodexPublicErrorFinalFields(c *gin.Context) (errorType, publicCode, message string, ok bool) {
	source := getOpenAIErrorMessageSource(c)
	if !source.recorded {
		return "", "", "", false
	}
	key := source.key
	if key == "" {
		key, _ = ClassifyCodexPublicFailure(source.status, nil)
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok {
		return "", "", "", false
	}
	return definition.ErrorType, definition.PublicCode, definition.DefaultMessage, true
}

// CodexPublicErrorFinalStatus returns the public HTTP status for a recorded
// final failure. It is used by fallback writers that do not re-classify.
func CodexPublicErrorFinalStatus(c *gin.Context) (int, bool) {
	source := getOpenAIErrorMessageSource(c)
	if !source.recorded {
		return 0, false
	}
	key := source.key
	if key == "" {
		key, _ = ClassifyCodexPublicFailure(source.status, nil)
	}
	if _, ok := codexPublicErrorByKey(key); !ok {
		return 0, false
	}
	return codexPublicErrorPublicStatus(key, source.status), true
}

// ResetOpenAIErrorMessageSource clears a previous turn's classification. WS
// connections reuse one gin context across turns, so a new turn must not
// inherit the last turn's final failure.
func ResetOpenAIErrorMessageSource(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set("openai_error_message_source", openAIErrorMessageSource{})
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
		source.recorded = true
	}
	c.Set("openai_error_message_source", source)
}

// SetCodexPublicErrorFinal records a final trusted classification and its
// public status.
func SetCodexPublicErrorFinal(c *gin.Context, key string, status int) {
	if c == nil {
		return
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok {
		return
	}
	source := getOpenAIErrorMessageSource(c)
	source.key = definition.Key
	source.status = status
	source.recorded = true
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
	source.recorded = true
	c.Set("openai_error_message_source", source)
}

// SetOpenAIErrorMessageSource retains the final attempt's original display
// classification even if stream failover had to sanitize ResponseBody earlier.
func (e *UpstreamFailoverError) SetOpenAIErrorMessageSource(c *gin.Context) {
	if e == nil || c == nil {
		return
	}
	status := e.StatusCode
	key := ""
	if e.openAIErrorMessageCode != "" {
		if resolved, ok := codexPublicErrorKeyFromCode(e.openAIErrorMessageCode); ok && !codexPublicErrorCodeConflicts(resolved, status) {
			key = resolved
		}
	}
	if key == "" {
		key, _ = ClassifyCodexPublicFailure(status, e.ResponseBody)
	}
	source := openAIErrorMessageSource{status: status, recorded: true, key: key}
	if e.openAIErrorMessageCode != "" {
		source.code = e.openAIErrorMessageCode
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

// codexPublicErrorParamAllowed keeps only platform-level request field names.
// Nested provider paths and free text never become a public error side channel.
func codexPublicErrorParamAllowed(param string) bool {
	switch param {
	case "model", "input", "tools", "instructions", "previous_response_id", "service_tier", "stream", "messages", "system", "systemInstruction":
		return true
	}
	return false
}

func normalizeOpenAIErrorMessageWithResolver(payload []byte, source openAIErrorMessageSource, resolve func(string) string) []byte {
	recognized := false
	if json.Valid(payload) {
		_, errorObject := openAIProtocolError(gjson.ParseBytes(payload))
		recognized = errorObject.IsObject()
	}
	if !recognized && !source.recorded {
		// Ordinary content such as text deltas, tool results and unknown valid
		// events keep their original bytes.
		return payload
	}
	key := strings.TrimSpace(source.key)
	if _, ok := codexPublicErrorByKey(key); !ok {
		key = ""
	}
	if key == "" {
		if code := openAIErrorMessageCode(payload); code != "" {
			if resolved, ok := codexPublicErrorKeyFromCode(code); ok && !codexPublicErrorCodeConflicts(resolved, source.status) {
				key = resolved
			}
		}
	}
	if key == "" {
		key = codexPublicErrorFallbackKey(source.status)
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok || definition.PublicCode == "" {
		// Normal protocol terminals (D13-D16) and unrecognized non-failure
		// documents keep their original representation.
		return payload
	}
	message := ""
	if resolve != nil {
		message = strings.TrimSpace(resolve(definition.Key))
	}
	if message == "" {
		message = definition.DefaultMessage
	}
	param := ""
	if json.Valid(payload) {
		_, errorObject := openAIProtocolError(gjson.ParseBytes(payload))
		if candidate := strings.TrimSpace(errorObject.Get("param").String()); codexPublicErrorParamAllowed(candidate) {
			param = candidate
		}
	}
	return rebuildCodexPublicErrorEnvelope(payload, definition, message, param)
}

// rebuildCodexPublicErrorEnvelope constructs a fresh protocol envelope from
// approved fields. Provider extensions, nested diagnostics and unapproved
// headers never survive a confirmed failure.
func rebuildCodexPublicErrorEnvelope(payload []byte, definition codexPublicErrorDefinition, message, param string) []byte {
	errorObject := []byte("{}")
	errorObject, _ = sjson.SetBytes(errorObject, "message", message)
	if definition.PublicCode != "" {
		errorObject, _ = sjson.SetBytes(errorObject, "code", definition.PublicCode)
	}
	if definition.ErrorType != "" {
		errorObject, _ = sjson.SetBytes(errorObject, "type", definition.ErrorType)
	}
	if param != "" {
		errorObject, _ = sjson.SetBytes(errorObject, "param", param)
	}
	if !json.Valid(payload) {
		return []byte(`{"error":` + string(errorObject) + `}`)
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return []byte(`{"error":` + string(errorObject) + `}`)
	}
	switch root.Get("type").String() {
	case "response.failed":
		return rebuildCodexPublicErrorFailedEnvelope(root, errorObject)
	case "error":
		if root.Get("error").IsObject() {
			event := []byte(`{"type":"error"}`)
			event, _ = sjson.SetRawBytes(event, "error", errorObject)
			event = copyCodexPublicEventFields(root, event, "sequence_number", "status")
			return event
		}
		// Top-level error event carriers use message/code directly.
		event := []byte(`{"type":"error"}`)
		if definition.ErrorType != "" {
			event, _ = sjson.SetBytes(event, "type", "error")
		}
		event, _ = sjson.SetBytes(event, "message", message)
		if definition.PublicCode != "" {
			event, _ = sjson.SetBytes(event, "code", definition.PublicCode)
		}
		if param != "" {
			event, _ = sjson.SetBytes(event, "param", param)
		}
		event = copyCodexPublicEventFields(root, event, "sequence_number", "status")
		return event
	default:
		return []byte(`{"error":` + string(errorObject) + `}`)
	}
}

func rebuildCodexPublicErrorFailedEnvelope(root gjson.Result, errorObject []byte) []byte {
	out := []byte(`{"type":"response.failed"}`)
	if response := root.Get("response"); response.IsObject() {
		rebuilt := []byte("{}")
		for _, field := range []string{"id", "object", "created_at", "status", "model"} {
			if value := response.Get(field); value.Exists() && value.Type != gjson.Null {
				rebuilt, _ = sjson.SetRawBytes(rebuilt, field, []byte(value.Raw))
			}
		}
		rebuilt, _ = sjson.SetRawBytes(rebuilt, "error", errorObject)
		if usage := response.Get("usage"); usage.IsObject() {
			rebuilt, _ = sjson.SetRawBytes(rebuilt, "usage", []byte(usage.Raw))
		}
		out, _ = sjson.SetRawBytes(out, "response", rebuilt)
	} else {
		out, _ = sjson.SetRawBytes(out, "error", errorObject)
	}
	return copyCodexPublicEventFields(root, out, "sequence_number")
}

func copyCodexPublicEventFields(root gjson.Result, out []byte, fields ...string) []byte {
	for _, field := range fields {
		if value := root.Get(field); value.Exists() && value.Type != gjson.Null {
			out, _ = sjson.SetRawBytes(out, field, []byte(value.Raw))
		}
	}
	return out
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
			return c.GetString(string(ctxkey.Platform)) == PlatformOpenAI && !isLocalOpenAIError(c)
		}
		w := &codexSafetyBufferingHTTPWriter{
			ResponseWriter: c.Writer, ctx: ctx, c: c,
			projectionEnabled: enabled,
			bypassProjection: func() bool {
				if c.GetString(string(ctxkey.Platform)) != PlatformOpenAI || isLocalOpenAIError(c) {
					return true
				}
				// A confirmed final failure is projected regardless of the
				// upstream MIME type or body shape (C07).
				if getOpenAIErrorMessageSource(c).recorded {
					return false
				}
				contentType := strings.ToLower(headers.Get("Content-Type"))
				return !strings.HasPrefix(contentType, "application/json") && !isEventStreamResponse(headers)
			},
			projectPayload: func(payload []byte, _ string) []byte {
				if !enabled() {
					return payload
				}
				source := getOpenAIErrorMessageSource(c)
				if !source.recorded && !hasOpenAIProtocolError(payload) {
					// Ordinary content and unknown valid events keep their
					// original bytes unless a final failure was recorded.
					return payload
				}
				return normalizeOpenAIErrorMessageWithResolver(payload, source, func(key string) string {
					return settings.CodexPublicErrorMessage(ctx, key)
				})
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

func hasOpenAIProtocolError(payload []byte) bool {
	if !json.Valid(payload) {
		return false
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return false
	}
	switch root.Get("type").String() {
	case "error", "response.failed":
		return true
	}
	return root.Get("error").IsObject()
}

// CodexPublicErrorMessage exposes the effective catalog text to read-side
// projections such as user history and async task views.
func (s *OpenAIGatewayService) CodexPublicErrorMessage(ctx context.Context, key string) string {
	return s.codexPublicErrorMessage(ctx, key)
}

// codexPublicErrorMessage returns the effective catalog text for a key.
func (s *OpenAIGatewayService) codexPublicErrorMessage(ctx context.Context, key string) string {
	if s != nil && s.settingService != nil {
		if message := strings.TrimSpace(s.settingService.CodexPublicErrorMessage(ctx, key)); message != "" {
			return message
		}
	}
	if definition, ok := codexPublicErrorByKey(key); ok {
		return definition.DefaultMessage
	}
	return ""
}

func (s *OpenAIGatewayService) projectCodexPublicErrorPayload(ctx context.Context, payload []byte) []byte {
	if s == nil || s.settingService == nil {
		return payload
	}
	source := openAIErrorMessageSource{}
	if code := openAIErrorMessageCode(payload); code != "" {
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
	if account != nil && account.Platform == PlatformOpenAI && s.settingService != nil && !isLocalOpenAIError(c) {
		return normalizeOpenAIErrorMessageWithResolver(payload, getOpenAIErrorMessageSource(c), func(key string) string {
			return s.settingService.CodexPublicErrorMessage(ctx, key)
		})
	}
	return payload
}
