package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// A terminal model rejection is observable as a failed request, while carrying
// no evidence that the credential owner or its connection is unhealthy.
type openAIModelNotFoundTerminalError struct{ cause error }

func (e *openAIModelNotFoundTerminalError) Error() string { return e.cause.Error() }
func (e *openAIModelNotFoundTerminalError) Unwrap() error { return e.cause }

func isOpenAIModelNotFoundTerminalError(err error) bool {
	var terminal *openAIModelNotFoundTerminalError
	return errors.As(err, &terminal)
}

// isOpenAIModelNotFoundError follows Codex's terminal-provider-error contract:
// only the exact code with no status, 400, 403, or 404 is a model rejection.
// Free-form messages and similar codes must not override credential/rate errors.
func isOpenAIModelNotFoundError(statusCode int, body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	if statusCode != 0 && statusCode != http.StatusBadRequest && statusCode != http.StatusForbidden && statusCode != http.StatusNotFound {
		return false
	}
	code := gjson.GetBytes(body, "error.code")
	if !code.Exists() {
		code = gjson.GetBytes(body, "response.error.code")
	}
	return code.Type == gjson.String && code.String() == "model_not_found"
}

// IsOpenAIModelNotFoundError exposes the exact terminal code/status contract to
// the handler after all eligible credential owners have been tried.
func IsOpenAIModelNotFoundError(statusCode int, body []byte) bool {
	return isOpenAIModelNotFoundError(statusCode, body)
}

func openAIModelNotFoundEventStatus(body []byte) (int, bool) {
	status := 0
	for _, path := range []string{"status", "status_code", "error.status", "error.status_code", "response.error.status", "response.error.status_code"} {
		if value := gjson.GetBytes(body, path); value.Type == gjson.Number {
			status = int(value.Int())
			break
		}
	}
	if !isOpenAIModelNotFoundError(status, body) {
		return 0, false
	}
	if status == 0 {
		status = http.StatusBadRequest
	}
	return status, true
}

var upstreamModelNotFoundKeywords = []string{"model not found", "unknown model", "not found"}

func isUpstreamModelNotFoundError(statusCode int, body []byte) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	normalized := normalizeModelNotFoundBody(body)
	if normalized == "" || !strings.Contains(normalized, "model") {
		return false
	}
	return containsModelNotFoundKeyword(normalized)
}

func isModelNotFoundError(statusCode int, body []byte) bool {
	return isUpstreamModelNotFoundError(statusCode, body) || statusCode == http.StatusNotFound
}

// openAICodexPlanGatedModelPhrase matches the deterministic Codex 400 returned
// when a ChatGPT OAuth account's plan cannot serve the requested model, e.g.
// {"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}
// The phrase is compared against the normalized body (lowercased, "_"/"-"
// folded to spaces), so it also matches the same message embedded in
// error.message-style payloads.
const openAICodexPlanGatedModelPhrase = "model is not supported when using codex"

// isOpenAICodexPlanGatedModelError reports whether the upstream response is the
// deterministic Codex rejection of a plan-gated model on a ChatGPT account.
// Unlike transient failures, retrying the same account cannot succeed until the
// account's plan changes, so callers should treat it like model-not-found and
// cool the (account, model) pair down instead of re-selecting the account.
func isOpenAICodexPlanGatedModelError(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	normalized := normalizeModelNotFoundBody(body)
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, openAICodexPlanGatedModelPhrase)
}

func containsModelNotFoundKeyword(normalizedBody string) bool {
	if normalizedBody == "" {
		return false
	}
	for _, keyword := range upstreamModelNotFoundKeywords {
		if strings.Contains(normalizedBody, keyword) {
			return true
		}
	}
	return false
}

func normalizeModelNotFoundBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	normalized := strings.ToLower(string(body))
	normalized = strings.NewReplacer("_", " ", "-", " ", "\n", " ", "\r", " ", "\t", " ").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}
