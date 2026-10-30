package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// GetCodexCLI reports the version selected by this application release. It never
// checks upstream latest or changes account/global outbound identity settings.
func (h *SettingHandler) GetCodexCLI(c *gin.Context) {
	respondCodexCLIRuntime(c, codexcli.DefaultManager().Status(c.Request.Context()), h.settingService.GetCodexCLIBridgeEnabled)
}

func (h *SettingHandler) InstallCodexCLI(c *gin.Context) {
	installCodexCLI(c, codexcli.DefaultManager().Install, h.settingService.GetCodexCLIBridgeEnabled)
}

type codexCLIRuntimeView struct {
	codexcli.Status
	BridgeEnabled     bool   `json:"bridge_enabled"`
	DefaultConfigTOML string `json:"default_config_toml"`
}

func respondCodexCLIRuntime(c *gin.Context, status codexcli.Status, read func(context.Context) (bool, error)) {
	enabled, err := read(c.Request.Context())
	if err != nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Could not read the Codex CLI bridge setting", "CODEX_CLI_BRIDGE_SETTING_UNAVAILABLE", nil)
		return
	}
	response.Success(c, codexCLIRuntimeView{Status: status, BridgeEnabled: enabled, DefaultConfigTOML: service.DefaultCodexCLIConfigTOML})
}

func installCodexCLI(c *gin.Context, install func(context.Context) (codexcli.Status, error), read func(context.Context) (bool, error)) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	status, err := install(ctx)
	if err != nil {
		// Transport errors may contain proxy credentials and filesystem errors
		// contain private paths. Keep the public failure stable and source-free.
		response.ErrorWithDetails(c, http.StatusBadGateway, "Could not install the reviewed Codex CLI; check runtime status before retrying", "CODEX_CLI_INSTALL_FAILED", nil)
		return
	}
	respondCodexCLIRuntime(c, status, read)
}

func (h *SettingHandler) UpdateCodexCLIBridge(c *gin.Context) {
	updateCodexCLIBridge(c, codexcli.DefaultManager().Status, h.settingService.SetCodexCLIBridgeEnabled)
}

func updateCodexCLIBridge(c *gin.Context, runtime func(context.Context) codexcli.Status, save func(context.Context, bool) error) {
	var input struct {
		BridgeEnabled *bool `json:"bridge_enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.BridgeEnabled == nil || decoder.Decode(new(any)) != io.EOF {
		response.ErrorWithDetails(c, http.StatusBadRequest, "An explicit bridge_enabled boolean is required", "CODEX_CLI_BRIDGE_CONFIG_INVALID", nil)
		return
	}
	status := runtime(c.Request.Context())
	if *input.BridgeEnabled && (!status.ExecutionAvailable || !status.Installed || !status.SourceVerified || !status.VersionMatch || status.TargetVersion == "" || status.InstalledVersion != status.TargetVersion) {
		response.ErrorWithDetails(c, http.StatusUnprocessableEntity, "A verified release-pinned Codex CLI runtime is required before enabling the bridge", "CODEX_CLI_RUNTIME_UNAVAILABLE", nil)
		return
	}
	if err := save(c.Request.Context(), *input.BridgeEnabled); err != nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Could not save the Codex CLI bridge setting", "CODEX_CLI_BRIDGE_SETTING_UNAVAILABLE", nil)
		return
	}
	response.Success(c, codexCLIRuntimeView{Status: status, BridgeEnabled: *input.BridgeEnabled, DefaultConfigTOML: service.DefaultCodexCLIConfigTOML})
}
