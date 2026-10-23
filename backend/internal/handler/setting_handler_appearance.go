package handler

import (
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetConsoleBackground(c *gin.Context) {
	revision := c.Param("revision")
	data, err := h.settingService.GetConsoleBackground(c.Request.Context(), revision)
	if err != nil {
		c.Header("Cache-Control", "no-store")
		response.ErrorFrom(c, err)
		return
	}
	etag := `"` + revision + `"`
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "image/jpeg", data)
}
