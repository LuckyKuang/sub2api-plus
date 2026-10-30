package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetCodexCLI reports the version selected by this application release. It never
// checks upstream latest or changes account/global outbound identity settings.
func (h *SettingHandler) GetCodexCLI(c *gin.Context) {
	response.Success(c, codexcli.DefaultManager().Status(c.Request.Context()))
}

func (h *SettingHandler) InstallCodexCLI(c *gin.Context) {
	installCodexCLI(c, codexcli.DefaultManager().Install)
}

func installCodexCLI(c *gin.Context, install func(context.Context) (codexcli.Status, error)) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	status, err := install(ctx)
	if err != nil {
		// Transport errors may contain proxy credentials and filesystem errors
		// contain private paths. Keep the public failure stable and source-free.
		response.ErrorWithDetails(c, http.StatusBadGateway, "Could not install the reviewed Codex CLI; check runtime status before retrying", "CODEX_CLI_INSTALL_FAILED", nil)
		return
	}
	response.Success(c, status)
}
