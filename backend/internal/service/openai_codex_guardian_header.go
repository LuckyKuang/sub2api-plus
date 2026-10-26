package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const codexGuardianHeader = "x-codex-guardian"

// Official Guardian-v2 scopes reviewer to its backend review model and emits
// classifier from the Guardian sampler. Neither role follows API-key failover.
func applyOpenAICodexGuardianHeader(c *gin.Context, account *Account, headers http.Header, model string) {
	for name := range headers {
		if strings.EqualFold(name, codexGuardianHeader) {
			delete(headers, name)
		}
	}
	if c == nil || c.Request == nil || account == nil || !account.IsOpenAIOAuthLike() || isOpenAIResponsesCompactPath(c) {
		return
	}
	values := c.Request.Header.Values(codexGuardianHeader)
	if len(values) != 1 {
		return
	}
	role := strings.TrimSpace(values[0])
	if (role == "reviewer" && model == codexAutoReviewModel) ||
		(role == "classifier" && model != "" && strings.TrimSpace(c.GetHeader(openAISubagentHeader)) == "guardian") {
		headers.Set(codexGuardianHeader, role)
	}
}
