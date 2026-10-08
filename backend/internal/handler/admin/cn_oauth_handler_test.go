//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCNOAuthHandlerRejectsInvalidAndUnauthenticatedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, platform, action, body string
		owner                        int64
		status                       int
	}{
		{"unauthenticated", "kimi", "start", `{}`, 0, 401},
		{"invalid platform", "other", "start", `{}`, 7, 400},
		{"malformed JSON", "kimi", "start", `{`, 7, 400},
		{"invalid region", "kimi", "start", `{"region":"other"}`, 7, 400},
		{"missing session", "minimax", "complete", `{}`, 7, 400},
		{"missing model session", "stepfun", "models", `{}`, 7, 400},
		{"model mapping must contain strings", "stepfun", "complete", `{"model_mapping":{"step-3.7-flash":42}}`, 7, 400},
		{"missing action", "deepseek", "invalid", `{}`, 7, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "platform", Value: tc.platform}, {Key: "action", Value: tc.action}}
			if tc.owner > 0 {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.owner})
			}
			NewCNOAuthHandler(service.NewCNOAuthService(nil, nil, nil)).Handle(c)
			require.Equal(t, tc.status, rec.Code)
			require.NotContains(t, rec.Body.String(), "access_token")
			require.NotContains(t, rec.Body.String(), "code_verifier")
		})
	}
}
