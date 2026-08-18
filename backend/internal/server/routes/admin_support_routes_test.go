package routes

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminSupportNamespaceRegistersOnlyReadRoutes(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	require.NoError(t, err)
	function := regexp.MustCompile(`(?s)func registerAdminSupportRoutes\(.*?\n}\n`).FindString(string(source))
	require.NotEmpty(t, function)
	require.Contains(t, function, `support.Use(h.Admin.User.RequireSupportTarget)`)

	for _, path := range []string{
		`support.GET("",`,
		`support.GET("/profile",`,
		`support.GET("/api-keys",`,
		`support.GET("/usage", h.Usage.AdminSupportStats)`,
		`support.GET("/async-images",`,
		`support.GET("/async-images/:task_id",`,
		`support.GET("/channels",`,
		`support.GET("/channel-status",`,
		`support.GET("/channel-status/:id",`,
		`support.GET("/subscriptions",`,
		`support.GET("/orders",`,
		`support.GET("/orders/:order_id",`,
	} {
		require.Contains(t, function, path)
	}

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		require.NotContains(t, function, "support."+method+"(")
	}
	require.False(t, strings.Contains(function, "ContextKeyUser"))
}

func TestAdminSupportSensitiveReadsAreAudited(t *testing.T) {
	source, err := os.ReadFile("../middleware/audit_log.go")
	require.NoError(t, err)
	for _, path := range []string{
		"/api/v1/admin/support/users/:user_id",
		"/api/v1/admin/support/users/:user_id/api-keys",
		"/api/v1/admin/support/users/:user_id/async-images",
		"/api/v1/admin/support/users/:user_id/channels",
		"/api/v1/admin/support/users/:user_id/channel-status",
		"/api/v1/admin/support/users/:user_id/subscriptions",
		"/api/v1/admin/support/users/:user_id/orders",
	} {
		require.Contains(t, string(source), path)
	}
}
