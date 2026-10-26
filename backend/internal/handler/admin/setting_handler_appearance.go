package admin

import (
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetConsoleAppearance(c *gin.Context) {
	appearance, err := h.settingService.GetConsoleAppearance(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, appearance)
}

func (h *SettingHandler) UpdateConsoleAppearance(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	var update service.ConsoleAppearanceUpdate
	if err := c.ShouldBindJSON(&update); err != nil {
		response.BadRequest(c, "Invalid appearance settings payload")
		return
	}
	appearance, err := h.settingService.UpdateConsoleAppearance(c.Request.Context(), update)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, appearance)
}
