package handler

import (
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func startClientDisconnectRiskLifecycle(
	c *gin.Context,
	risk *service.ClientDisconnectRiskService,
	userID, apiKeyID int64,
	protocol string,
) *service.ClientDisconnectLifecycle {
	if c == nil || c.Request == nil || risk == nil {
		return nil
	}
	role, ok := middleware2.GetUserRoleFromContext(c)
	if !ok {
		// Role is required to enforce the administrator exemption at the counter
		// boundary. The database predicate remains a second protection layer.
		return nil
	}
	requestID := trustedClientRequestID(c)
	lifecycle := risk.NewLifecycle(userID, apiKeyID, role, requestID, protocol)
	if lifecycle != nil {
		c.Request = c.Request.WithContext(service.WithClientDisconnectLifecycle(c.Request.Context(), lifecycle))
	}
	return lifecycle
}

func trustedClientRequestID(c *gin.Context) string {
	if c != nil && c.Request != nil {
		requestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		if requestID = strings.TrimSpace(requestID); requestID != "" {
			return requestID
		}
	}
	return uuid.NewString()
}
