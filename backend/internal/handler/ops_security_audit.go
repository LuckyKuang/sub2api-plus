package handler

import (
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

const opsSecurityAuditDenialKey = "ops_security_audit_denial"

// Only the local audit decision may establish this attribution. A provider
// response carrying the same error code is not a local security refusal.
func markOpsSecurityAuditDecision(c *gin.Context, d *securityaudit.Decision) {
	if c == nil || d == nil || d.Kind != securityaudit.DecisionBlock || securityAuditStatus(d) != 403 {
		return
	}
	code := securityAuditErrorCode(d)
	switch code {
	case "content_policy_violation", "session_blocked_by_content_policy", "prompt_guard_blocked":
		if service.GetOpenAIClientTransport(c) == service.OpenAIClientTransportWS {
			service.MarkOpsStreamErrorValue(c, service.OpsStreamError{
				ErrType: code, Code: code, SecurityAuditCode: code,
				Message: securityAuditMessage(d), IntendedStatus: 403, RequestScoped: true,
			})
			return
		}
		c.Set(opsSecurityAuditDenialKey, code)
	}
}

func opsSecurityAuditDenialCode(c *gin.Context) string {
	if c == nil {
		return ""
	}
	code, _ := c.Get(opsSecurityAuditDenialKey)
	value, _ := code.(string)
	return value
}

func normalizeOpsErrorTypeForContext(c *gin.Context, errType, code string) string {
	if localCode := opsSecurityAuditDenialCode(c); localCode != "" {
		return localCode
	}
	return normalizeOpsErrorType(errType, code)
}
