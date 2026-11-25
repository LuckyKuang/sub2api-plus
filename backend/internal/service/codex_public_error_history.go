package service

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
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

// codexPublicErrorFromLoosePayload accepts the stored shapes used by history
// and async task records. Only exact catalog codes are promoted.
func codexPublicErrorFromLoosePayload(payload []byte) (string, bool) {
	if len(payload) == 0 || !json.Valid(payload) {
		return "", false
	}
	for _, path := range []string{"error.code", "response.error.code", "code"} {
		value := gjson.GetBytes(payload, path)
		if value.Type != gjson.String {
			continue
		}
		if key, ok := codexPublicErrorKeyFromCode(strings.TrimSpace(value.String())); ok {
			return key, true
		}
	}
	return "", false
}

// ProjectCodexUserErrorListItem replaces an OpenAI error message with the
// current public text. List rows have no retained upstream code, so only a
// status-level classification is safe.
func ProjectCodexUserErrorListItem(item *UserErrorRequest, resolve func(string) string) {
	if item == nil || !strings.EqualFold(strings.TrimSpace(item.Platform), PlatformOpenAI) {
		return
	}
	key, publicStatus := ClassifyCodexPublicFailure(item.StatusCode, nil)
	if definition, ok := codexPublicErrorByKey(key); ok {
		item.Message = resolveOr(definition.DefaultMessage, resolve, key)
	}
	item.StatusCode = publicStatus
}

// ProjectCodexUserErrorDetail rebuilds every public error field from the
// retained classification. Provider diagnostics never reach the user-facing
// detail; authorized administrator Ops views keep reading the raw record.
func ProjectCodexUserErrorDetail(detail *UserErrorRequestDetail, resolve func(string) string) {
	if detail == nil || !strings.EqualFold(strings.TrimSpace(detail.Platform), PlatformOpenAI) {
		return
	}
	status := detail.StatusCode
	if detail.UpstreamStatusCode != nil && *detail.UpstreamStatusCode > 0 {
		status = *detail.UpstreamStatusCode
	}
	key := ""
	payload := []byte(strings.TrimSpace(detail.ErrorBody))
	if resolved, ok := codexPublicErrorFromLoosePayload(payload); ok {
		key = resolved
	}
	if key == "" {
		key, status = ClassifyCodexPublicFailure(status, payload)
	} else {
		status = codexPublicErrorPublicStatus(key, status)
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok {
		return
	}
	detail.Message = resolveOr(definition.DefaultMessage, resolve, key)
	detail.StatusCode = status
	if body := codexPublicErrorDocument(key, resolve); body != "" {
		detail.ErrorBody = body
	}
	detail.UpstreamStatusCode = nil
}

// ProjectCodexImageTaskError rebuilds the public async task error using the
// same classification as live failures. The stored row itself is unchanged.
func ProjectCodexImageTaskError(status int, payload json.RawMessage, resolve func(string) string) json.RawMessage {
	if len(payload) == 0 && status <= 0 {
		return payload
	}
	key := ""
	if resolved, ok := codexPublicErrorFromLoosePayload(payload); ok {
		key = resolved
	}
	if key == "" {
		key, _ = ClassifyCodexPublicFailure(status, payload)
	}
	definition, ok := codexPublicErrorByKey(key)
	if !ok || definition.PublicCode == "" {
		return payload
	}
	message := resolveOr(definition.DefaultMessage, resolve, key)
	document := map[string]string{"type": definition.ErrorType, "code": definition.PublicCode, "message": message}
	body, err := json.Marshal(document)
	if err != nil {
		return payload
	}
	return body
}

func resolveOr(fallback string, resolve func(string) string, key string) string {
	if resolve != nil {
		if message := strings.TrimSpace(resolve(key)); message != "" {
			return message
		}
	}
	return fallback
}
