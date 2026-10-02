//go:build unit || !integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSupportReadScopePreservesActorAndIgnoresUntrustedTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(method, "/api/v1/admin/support/users/42/keys", nil)
			c.Set(string(ContextKeyUser), AuthSubject{UserID: 1})
			c.Set(string(ContextKeyUserRole), "admin")
			c.Set(SupportReadTargetKey, SupportReadTarget{Subject: AuthSubject{UserID: 42}, Role: "user"})
			actor, _ := GetAuthSubjectFromContext(c)
			read, _ := GetReadSubjectFromContext(c)
			role, _ := GetReadUserRoleFromContext(c)
			require.Equal(t, int64(1), actor.UserID)
			if method == http.MethodGet {
				require.Equal(t, int64(42), read.UserID)
				require.Equal(t, "user", role)
			} else {
				require.Equal(t, int64(1), read.UserID)
				require.Equal(t, "admin", role)
			}
			c.Set(string(ContextKeyUserRole), "user")
			read, _ = GetReadSubjectFromContext(c)
			require.Equal(t, int64(1), read.UserID)
		})
	}
}
