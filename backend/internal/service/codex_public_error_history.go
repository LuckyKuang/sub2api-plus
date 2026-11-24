package service

import (
	"encoding/json"
	"strings"
)

func codexPublicErrorDocument(key string, resolve func(string) string) string {
	definition, ok := codexPublicErrorByKey(key)
	if !ok {
		definition, ok = codexPublicErrorByKey("server_error")
		if !ok {
			return ""
		}
	}
	message := ""
	if resolve != nil {
		message = strings.TrimSpace(resolve(definition.Key))
	}
	if message == "" {
		message = definition.DefaultMessage
	}
	errorObject := map[string]string{"message": message}
	if definition.ErrorType != "" {
		errorObject["type"] = definition.ErrorType
	}
	if definition.PublicCode != "" {
		errorObject["code"] = definition.PublicCode
	}
	body, err := json.Marshal(map[string]any{"error": errorObject})
	if err != nil {
		return ""
	}
	return string(body)
}

// ProjectCodexUserErrorListItem replaces an OpenAI error message with the
// current public fallback. List rows have no retained upstream code, so only a
// status-level classification is safe.
func ProjectCodexUserErrorListItem(item *UserErrorRequest, resolve func(string) string) {
	if item == nil || !strings.EqualFold(strings.TrimSpace(item.Platform), PlatformOpenAI) {
		return
	}
	key := codexPublicErrorFallbackKey(item.StatusCode)
	if definition, ok := codexPublicErrorByKey(key); ok {
		item.Message = resolveOr(definition.DefaultMessage, resolve, key)
	}
}

// ProjectCodexUserErrorDetail rebuilds the public error body from the retained
// code or status. Upstream diagnostics never reach the user-facing detail.
func ProjectCodexUserErrorDetail(detail *UserErrorRequestDetail, resolve func(string) string) {
	if detail == nil || !strings.EqualFold(strings.TrimSpace(detail.Platform), PlatformOpenAI) {
		return
	}
	key := ""
	if code := openAIErrorMessageCode([]byte(detail.ErrorBody)); code != "" {
		if resolved, ok := codexPublicErrorKeyFromCode(code); ok {
			key = resolved
		}
	}
	if key == "" {
		status := detail.StatusCode
		if detail.UpstreamStatusCode != nil && *detail.UpstreamStatusCode > 0 {
			status = *detail.UpstreamStatusCode
		}
		key = codexPublicErrorFallbackKey(status)
	}
	if body := codexPublicErrorDocument(key, resolve); body != "" {
		detail.ErrorBody = body
	}
	detail.UpstreamStatusCode = nil
}

func resolveOr(fallback string, resolve func(string) string, key string) string {
	if resolve != nil {
		if message := strings.TrimSpace(resolve(key)); message != "" {
			return message
		}
	}
	return fallback
}
