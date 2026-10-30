package handler

import (
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// CodexCLIResponses shares the existing authentication, original-body audit,
// billing and concurrency pipeline. Only this route selects the CLI runner.
func (h *OpenAIGatewayHandler) CodexCLIResponses(c *gin.Context) {
	// Every exec task is independent. A supplied session header must not read,
	// create or migrate an existing Responses/WebSocket conversation owner.
	ctx := service.WithoutOpenAISessionRoute(c.Request.Context())
	c.Request = c.Request.WithContext(service.WithCodexCLITask(ctx))
	h.Responses(c)
}
