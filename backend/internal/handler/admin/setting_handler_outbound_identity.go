package admin

import (
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetOutboundIdentity(c *gin.Context) {
	response.Success(c, h.settingService.GetOutboundIdentityView(c.Request.Context()))
}

func (h *SettingHandler) PreviewOutboundIdentity(c *gin.Context) {
	var input struct {
		Platform  string                             `json:"platform"`
		Type      string                             `json:"type"`
		UserAgent string                             `json:"user_agent"`
		Selection *service.OutboundIdentitySelection `json:"selection"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid identity preview")
		return
	}
	identity, err := h.settingService.PreviewOutboundIdentity(c.Request.Context(), &service.Account{Platform: input.Platform, Type: input.Type, Credentials: map[string]any{"user_agent": input.UserAgent}}, input.Selection)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, identity)
}

func (h *SettingHandler) UpdateOutboundIdentity(c *gin.Context) {
	var settings service.OutboundIdentitySettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid outbound identity settings")
		return
	}
	if err := h.settingService.SetOutboundIdentitySettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.settingService.GetOutboundIdentityView(c.Request.Context()))
}
