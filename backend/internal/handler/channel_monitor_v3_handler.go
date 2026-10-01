package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type channelMonitorV3GroupAuthorizer interface {
	GetAvailableGroups(context.Context, int64) ([]service.Group, error)
}
type ChannelMonitorV3Handler struct {
	service *service.ChannelMonitorV3Service
	groups  channelMonitorV3GroupAuthorizer
}

func NewChannelMonitorV3Handler(svc *service.ChannelMonitorV3Service, keys *service.APIKeyService) *ChannelMonitorV3Handler {
	return &ChannelMonitorV3Handler{service: svc, groups: keys}
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
	if h.groups == nil {
		response.Error(c, http.StatusInternalServerError, "service status authorization unavailable")
		return
	}
	groups, err := h.groups.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	result, err := h.service.Snapshot(c.Request.Context(), ids, window, platform)
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
