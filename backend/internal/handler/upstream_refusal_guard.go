package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// checkUpstreamRefusal runs exclusively after the Messages security audit gate.
func (h *GatewayHandler) checkUpstreamRefusal(c *gin.Context, userID, apiKeyID int64, sessionHash, requestID string) bool {
	if h.refusalGuard == nil {
		return false
	}
	blocked, remaining := h.refusalGuard.Check(c.Request.Context(), userID, apiKeyID, sessionHash, requestID)
	if blocked {
		h.errorResponseWithCode(c, http.StatusForbidden, "permission_error", "session_blocked_by_upstream_refusal", "该会话已被上游策略拒绝并屏蔽，请开启新会话 / This session was refused by upstream policy and is blocked, please start a new session")
		return true
	}
	if remaining > 0 {
		seconds := int(math.Ceil(remaining.Seconds()))
		c.Header("Retry-After", strconv.Itoa(seconds))
		h.errorResponseWithCode(c, http.StatusTooManyRequests, "rate_limit_error", "user_cooldown_by_upstream_refusal", fmt.Sprintf("上游拒绝冷却中，请在 %d 秒后重试 / Upstream refusal cooldown: retry in %d seconds", seconds, seconds))
		return true
	}
	return false
}
