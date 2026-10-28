package handler

import (
	"net/http"

	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// acquireGatewayUserRequest admits one external request to the user-wide pool.
// Callers complete basic validation and the ingress audit before calling it.
// Internal retries keep this lease; independent agent requests acquire their own.
func acquireGatewayUserRequest(c *gin.Context, helper *ConcurrencyHelper, writeError func(*gin.Context, int, string, string)) (func(), bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	if helper == nil || helper.concurrencyService == nil {
		writeError(c, http.StatusServiceUnavailable, "api_error", "Concurrency service is unavailable")
		return nil, false
	}
	release, err := helper.AcquireUserSlot(c, subject.UserID, subject.Concurrency)
	if err != nil {
		status, errType, code, message := concurrencyErrorResponse(err, "user")
		setRPMRetryAfter(c, err)
		writeError(c, status, errType, message)
		service.SetOpsLocalErrorType(c, errType, code)
		return nil, false
	}
	return wrapReleaseOnDone(c.Request.Context(), release), true
}

// admitControl audits content-free task/catalog operations without capturing
// today's billing policy for a previously accepted task.
func (h *OpenAIGatewayHandler) admitControl(c *gin.Context) (func(), bool) {
	if h == nil {
		imageTaskJSONError(c, http.StatusServiceUnavailable, "api_error", "Gateway admission is unavailable")
		return nil, false
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	decision := runSecurityAudit(c, requestLogger(c, "handler.gateway.control"), h.securityAuditCoordinator, h.contentModerationService, key, subject, service.ContentModerationProtocolOpenAIResponses, "", nil, "http")
	if decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return nil, false
	}
	return acquireGatewayUserRequest(c, h.concurrencyHelper, h.errorResponse)
}

func googleUserAdmissionError(c *gin.Context, status int, errType string, message string) {
	googleErrorWithType(c, status, errType, "", message)
}

func (h *GatewayHandler) admitControl(c *gin.Context, protocol string) (func(), bool) {
	writeError := h.errorResponse
	if protocol == service.ContentModerationProtocolGemini {
		writeError = googleUserAdmissionError
	}
	if h == nil {
		writeError(c, http.StatusServiceUnavailable, "api_error", "Gateway admission is unavailable")
		return nil, false
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		writeError(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	decision := runSecurityAudit(c, requestLogger(c, "handler.gateway.control"), h.securityAuditCoordinator, h.contentModerationService, key, subject, protocol, "", nil, "http")
	if decision != nil && !decision.AllowNextStage {
		switch protocol {
		case service.ContentModerationProtocolGemini:
			googleSecurityAuditError(c, decision)
		case service.ContentModerationProtocolAnthropicMessages:
			h.anthropicSecurityAuditError(c, decision)
		default:
			h.openAISecurityAuditError(c, decision)
		}
		return nil, false
	}
	return acquireGatewayUserRequest(c, h.concurrencyHelper, writeError)
}
