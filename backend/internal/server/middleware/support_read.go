package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SupportReadTarget is set only after administrator auth and target validation.
// Authentication and audit continue to use the original AuthSubject.
type SupportReadTarget struct {
	Subject AuthSubject
	Role    string
}

const SupportReadTargetKey = "admin_support_read_target"

func GetSupportReadTarget(c *gin.Context) (SupportReadTarget, bool) {
	if c.Request == nil || c.Request.Method != http.MethodGet {
		return SupportReadTarget{}, false
	}
	actor, authenticated := GetAuthSubjectFromContext(c)
	role, _ := GetUserRoleFromContext(c)
	value, exists := c.Get(SupportReadTargetKey)
	target, valid := value.(SupportReadTarget)
	return target, authenticated && actor.UserID > 0 && role == "admin" && exists && valid && target.Subject.UserID > 0
}

// GetReadSubjectFromContext scopes ordinary read handlers to the support target.
// Mutation handlers must continue to use GetAuthSubjectFromContext.
func GetReadSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	if target, ok := GetSupportReadTarget(c); ok {
		return target.Subject, true
	}
	return GetAuthSubjectFromContext(c)
}

func GetReadUserRoleFromContext(c *gin.Context) (string, bool) {
	if target, ok := GetSupportReadTarget(c); ok {
		return target.Role, true
	}
	return GetUserRoleFromContext(c)
}
