package service

import (
	"errors"
	"net/http"
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

func openAIRequestTerminalEventStatus(payload []byte) (int, bool) {
	if openAIInputTerminalCode(0, payload) != "" {
		return http.StatusBadRequest, true
	}
	return openAIModelNotFoundEventStatus(payload)
}
