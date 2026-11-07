package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Codex codex-api/src/error.rs recognizes these exact provider codes with
// HTTP 400, or without an HTTP error status on SSE/WS. They describe the turn's
// input/budget, never the health or entitlement of a credential owner.
func isOpenAIInputTerminalCode(code string) bool {
	switch code {
	case "budget_reached", "audio_too_large", "audio_too_short", "invalid_audio",
		"invalid_audio_data", "invalid_audio_format", "missing_audio_sample_rate":
		return true
	}
	return false
}

func openAIInputTerminalCode(status int, payload []byte) string {
	if (status != 0 && status != http.StatusBadRequest) || !gjson.ValidBytes(payload) {
		return ""
	}
	code := gjson.GetBytes(payload, "error.code")
	if !code.Exists() {
		code = gjson.GetBytes(payload, "response.error.code")
	}
	if code.Type == gjson.String && isOpenAIInputTerminalCode(code.String()) {
		return code.String()
	}
	return ""
}

func (s *OpenAIGatewayService) writeOpenAINonStreamingInputTerminal(resp *http.Response, c *gin.Context, account *Account, payload []byte, message string) error {
	code := openAIInputTerminalCode(0, payload)
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = code
	}
	setOpsUpstreamError(c, http.StatusBadRequest, code, "")
	SetOpenAIErrorMessageSource(c, payload)
	if openAICompactClientWantsStream(c) && StopOpenAICompactSSEKeepaliveCommitted(c) {
		writeOpenAICompactSSEFailureMessage(c, http.StatusBadRequest, code, message)
	} else {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		s.finalizeCodexClientQuotaHeaders(c.Writer.Header(), c, account)
		c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writeOpenAIUpstreamClientError(c, http.StatusBadRequest, payload, message)
	}
	return &openAIRequestTerminalError{cause: fmt.Errorf("upstream terminal input error: %s", code)}
}
