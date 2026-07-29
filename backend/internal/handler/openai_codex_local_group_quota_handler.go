package handler

import (
	"net/http"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// CodexLocalGroupQuotaUsage serves the local 7-day and rolling 5-hour
// subscription counters expected by Codex at /backend-api/wham/usage. This
// endpoint never calls or proxies the upstream ChatGPT API.
func (h *OpenAIGatewayHandler) CodexLocalGroupQuotaUsage(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformOpenAI {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex local quota is not available for this group")
		return
	}
	if h == nil || h.gatewayService == nil || !h.gatewayService.IsCodexLocalGroupQuotaEnabledForGroup(c.Request.Context(), apiKey.Group) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex local quota is disabled")
		return
	}
	subscription, ok := middleware2.GetSubscriptionFromContext(c)
	if !ok || subscription == nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "No active local subscription quota is available")
		return
	}
	quota := service.BuildCodexLocalGroupQuotaUsage(apiKey.Group, subscription, time.Now())
	if quota == nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "No active local subscription quota is available")
		return
	}
	c.Header("Cache-Control", "no-store, private")
	c.Header("Vary", "Authorization")
	c.JSON(http.StatusOK, quota)
}
