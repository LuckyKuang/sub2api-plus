//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The installation API must never return transport credentials or private
// filesystem paths. Runtime status is the separate, sanitized diagnostic view.
func TestCodexCLIInstallFailureDoesNotExposePrivateDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, diagnostic := range []string{
		"proxy http://private-user:private-password@proxy.invalid:8080: invalid proxy",
		"open /private/account-data/cli/official.tar.gz: permission denied",
		"CLI subprocess emitted private-access-token",
	} {
		t.Run(diagnostic, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/codex-cli/install", nil)
			calls := 0
			installCodexCLI(c, func(ctx context.Context) (codexcli.Status, error) {
				calls++
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.Positive(t, time.Until(deadline))
				require.LessOrEqual(t, time.Until(deadline), 5*time.Minute)
				return codexcli.Status{}, errors.New(diagnostic)
			})
			require.Equal(t, 1, calls)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.JSONEq(t, `{"code":502,"message":"Could not install the reviewed Codex CLI; check runtime status before retrying","reason":"CODEX_CLI_INSTALL_FAILED"}`, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), diagnostic)
			require.NotContains(t, recorder.Body.String(), "private-")
		})
	}
}

func TestCodexCLIInstallSuccessReturnsRuntimeStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/codex-cli/install", nil)
	installCodexCLI(c, func(context.Context) (codexcli.Status, error) {
		return codexcli.Status{Installed: true, InstalledVersion: "0.165.0", TargetVersion: "0.165.0", ExecutionAvailable: true}, nil
	})
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"execution_available":true`)
	require.Contains(t, recorder.Body.String(), `"installed_version":"0.165.0"`)
}
