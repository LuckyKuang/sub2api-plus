//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
			}, func(context.Context) (bool, error) { return false, nil })
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
	}, func(context.Context) (bool, error) { return false, nil })
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"execution_available":true`)
	require.Contains(t, recorder.Body.String(), `"installed_version":"0.165.0"`)
	require.Contains(t, recorder.Body.String(), `"bridge_enabled":false`)
}

// Requirement: a new OAuth account must display the server's documented
// default config.toml before saving. Every successful runtime response provides
// the same template, including when the global bridge is disabled.
func TestCodexCLIRuntimeReturnsAccountConfigDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const expected = "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\ncli_auth_credentials_store = \"file\"\n"
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		for _, enabled := range []bool{false, true} {
			name := method + "/disabled"
			body := `{"bridge_enabled":false}`
			if enabled {
				name = method + "/enabled"
				body = `{"bridge_enabled":true}`
			}
			t.Run(name, func(t *testing.T) {
				r := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(r)
				c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/codex-cli", strings.NewReader(body))
				status := codexcli.Status{Installed: true, InstalledVersion: "0.165.0", TargetVersion: "0.165.0", SourceVerified: true, VersionMatch: true, ExecutionAvailable: true}
				read := func(context.Context) (bool, error) { return enabled, nil }
				switch method {
				case http.MethodGet:
					respondCodexCLIRuntime(c, status, read)
				case http.MethodPost:
					installCodexCLI(c, func(context.Context) (codexcli.Status, error) { return status, nil }, read)
				case http.MethodPut:
					updateCodexCLIBridge(c, func(context.Context) codexcli.Status { return status }, func(context.Context, bool) error { return nil })
				}
				require.Equal(t, http.StatusOK, r.Code)
				var result struct {
					Data struct {
						BridgeEnabled     bool   `json:"bridge_enabled"`
						DefaultConfigTOML string `json:"default_config_toml"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
				require.Equal(t, enabled, result.Data.BridgeEnabled)
				require.Equal(t, expected, result.Data.DefaultConfigTOML)
			})
		}
	}
}

// Requirement: only an explicit administrator enable with the exact verified
// ready runtime may persist true. Invalid requests and failed readiness have no
// writes; disabling remains available after installation becomes unavailable.
func TestCodexCLIBridgeEnablementRequiresVerifiedRuntimeBeforeWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ready := codexcli.Status{Installed: true, InstalledVersion: "0.165.0", TargetVersion: "0.165.0", SourceVerified: true, VersionMatch: true, ExecutionAvailable: true}
	for _, tc := range []struct {
		name   string
		mutate func(*codexcli.Status)
	}{
		{"missing", func(s *codexcli.Status) { s.Installed = false }},
		{"source_unverified", func(s *codexcli.Status) { s.SourceVerified = false }},
		{"wrong_version", func(s *codexcli.Status) { s.VersionMatch = false }},
		{"inconsistent_version", func(s *codexcli.Status) { s.InstalledVersion = "0.164.0" }},
		{"missing_target", func(s *codexcli.Status) { s.TargetVersion = "" }},
		{"unavailable", func(s *codexcli.Status) { s.ExecutionAvailable = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := ready
			tc.mutate(&status)
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/codex-cli", strings.NewReader(`{"bridge_enabled":true}`))
			writes := 0
			updateCodexCLIBridge(c, func(context.Context) codexcli.Status { return status }, func(context.Context, bool) error { writes++; return nil })
			require.Equal(t, http.StatusUnprocessableEntity, r.Code)
			require.Contains(t, r.Body.String(), "CODEX_CLI_RUNTIME_UNAVAILABLE")
			require.Zero(t, writes)
		})
	}
	for _, enabled := range []bool{true, false} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		body := `{"bridge_enabled":false}`
		status := codexcli.Status{}
		if enabled {
			body = `{"bridge_enabled":true}`
			status = ready
		}
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/codex-cli", strings.NewReader(body))
		writes := 0
		updateCodexCLIBridge(c, func(context.Context) codexcli.Status { return status }, func(_ context.Context, got bool) error { writes++; require.Equal(t, enabled, got); return nil })
		require.Equal(t, http.StatusOK, r.Code)
		require.Equal(t, 1, writes)
		require.Contains(t, r.Body.String(), body[1:len(body)-1])
	}
}

func TestCodexCLIBridgeRejectsMalformedInputBeforeRuntimeOrWrites(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"bridge_enabled":null}`, `{"bridge_enabled":"true"}`, `{"bridge_enabled":1}`, `{"bridge_enabled":true,"version":"latest"}`, `{"bridge_enabled":true} {}`, `{"bridge_enabled":`} {
		t.Run(body, func(t *testing.T) {
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/codex-cli", strings.NewReader(body))
			updateCodexCLIBridge(c, func(context.Context) codexcli.Status {
				t.Fatal("runtime checked before basic validation")
				return codexcli.Status{}
			}, func(context.Context, bool) error { t.Fatal("invalid setting written"); return nil })
			require.Equal(t, http.StatusBadRequest, r.Code)
		})
	}
}

func TestCodexCLIBridgeStorageFailuresDoNotExposeDiagnostics(t *testing.T) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/codex-cli", strings.NewReader(`{"bridge_enabled":false}`))
	updateCodexCLIBridge(c, func(context.Context) codexcli.Status { return codexcli.Status{} }, func(context.Context, bool) error { return errors.New("private-password /private/path") })
	require.Equal(t, http.StatusServiceUnavailable, r.Code)
	require.NotContains(t, r.Body.String(), "private")
	r = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/codex-cli", nil)
	respondCodexCLIRuntime(c, codexcli.Status{}, func(context.Context) (bool, error) { return true, errors.New("private-password") })
	require.Equal(t, http.StatusServiceUnavailable, r.Code)
	require.NotContains(t, r.Body.String(), "private")
}
