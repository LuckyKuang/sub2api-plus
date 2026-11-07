package service

import (
	"errors"
	"net/http"

	"github.com/tidwall/gjson"
)

// A deterministic request rejection carries no evidence that the credential
// owner or its connection is unhealthy. Preserve this across transport adapters
// so scheduler observations cannot turn it into an account penalty.
type openAIRequestTerminalError struct{ cause error }

func (e *openAIRequestTerminalError) Error() string { return e.cause.Error() }
func (e *openAIRequestTerminalError) Unwrap() error { return e.cause }

func isOpenAIRequestTerminalError(err error) bool {
	var terminal *openAIRequestTerminalError
	return errors.As(err, &terminal)
}

// Wrapped WS errors carry a real HTTP status, including the official
// status_code alias. A response's string status ("failed") is not an HTTP code.
func openAIErrorEventStatus(payload []byte) int {
	for _, path := range []string{"status", "status_code", "error.status", "error.status_code", "response.error.status", "response.error.status_code"} {
		if value := gjson.GetBytes(payload, path); value.Type == gjson.Number {
			return int(value.Int())
		}
	}
	return 0
}

func openAIRequestTerminalEventStatus(payload []byte) (int, bool) {
	if openAIInputTerminalCode(0, payload) != "" {
		return http.StatusBadRequest, true
	}
	return openAIModelNotFoundEventStatus(payload)
}
