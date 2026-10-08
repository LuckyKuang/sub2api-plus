package admin

import (
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type CNOAuthHandler struct{ service *service.CNOAuthService }

func NewCNOAuthHandler(s *service.CNOAuthService) *CNOAuthHandler { return &CNOAuthHandler{service: s} }

// Handle exposes only opaque, administrator-bound session handles. Provider
// grants, device codes and PKCE verifiers never appear in admin API responses.
func (h *CNOAuthHandler) Handle(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "administrator required")
		return
	}
	platform := c.Param("platform")
	if !cnoauth.Supported(platform) {
		response.BadRequest(c, "unsupported OAuth platform")
		return
	}
	var input struct {
		SessionID string `json:"session_id"`
		Region    string `json:"region"`
		ProxyID   *int64 `json:"proxy_id"`
		AccountID int64  `json:"account_id"`
		Callback  string `json:"callback"`
		service.CNOAuthCompleteInput
	}
	if c.ShouldBindJSON(&input) != nil || len(input.Callback) > 8192 || len(input.Name) > 200 || input.AccountID < 0 || input.Concurrency < 0 {
		response.BadRequest(c, "invalid authorization request")
		return
	}
	var result *service.CNOAuthView
	var err error
	switch c.Param("action") {
	case "start":
		result, err = h.service.Start(c.Request.Context(), subject.UserID, platform, input.Region, input.ProxyID, input.AccountID)
	case "poll", "cancel", "exchange":
		result, err = h.service.Advance(c.Request.Context(), subject.UserID, platform, input.SessionID, input.Callback, c.Param("action") == "cancel")
	case "complete":
		result, err = h.service.Complete(c.Request.Context(), subject.UserID, platform, input.SessionID, input.CNOAuthCompleteInput)
	case "models":
		models, modelErr := h.service.PreviewModels(c.Request.Context(), subject.UserID, platform, input.SessionID)
		if modelErr != nil {
			response.ErrorFrom(c, modelErr)
			return
		}
		response.Success(c, gin.H{"models": models})
		return
	default:
		response.BadRequest(c, "unsupported authorization action")
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
