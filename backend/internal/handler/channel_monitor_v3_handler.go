package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type ChannelMonitorV3Handler struct {
	service *service.ChannelMonitorV3Service
}

func NewChannelMonitorV3Handler(svc *service.ChannelMonitorV3Service) *ChannelMonitorV3Handler {
	return &ChannelMonitorV3Handler{service: svc}
}

func (h *ChannelMonitorV3Handler) Snapshot(c *gin.Context) {
	window, ok := service.ChannelMonitorV3Window(c.Query("range"))
	if !ok {
		response.BadRequest(c, "invalid service status range")
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if len(platform) > 64 {
		response.BadRequest(c, "invalid platform")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "authentication required")
		return
	}
	result, err := h.service.Snapshot(c.Request.Context(), window, platform)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "service status temporarily unavailable")
		return
	}
	response.Success(c, result)
}
func (h *ChannelMonitorV3Handler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}
func (h *ChannelMonitorV3Handler) UpdateConfig(c *gin.Context) {
	var cfg service.ChannelMonitorV3Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.BadRequest(c, "invalid service status configuration")
		return
	}
	updated, err := h.service.UpdateConfig(c.Request.Context(), cfg)
	if errors.Is(err, service.ErrChannelMonitorV3Config) {
		response.BadRequest(c, "invalid service status configuration")
		return
	}
	if errors.Is(err, service.ErrChannelMonitorV3Conflict) {
		response.Error(c, http.StatusConflict, "service status configuration changed; reload before saving")
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}
