package admin

import (
	"strconv"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetWeComUsageAlertService(svc *service.WeComUsageAlertService) {
	if h == nil {
		return
	}
	h.wecomUsageAlert = svc
}

func (h *AccountHandler) requireWeComUsageAlert(c *gin.Context) bool {
	if h != nil && h.wecomUsageAlert != nil {
		return true
	}
	response.ErrorFrom(c, service.ErrWeComUsageAlertUnavailable)
	return false
}

func weComUsageAlertAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}

// GetWeComUsageAlert GET /admin/accounts/:id/wecom-usage-alert
func (h *AccountHandler) GetWeComUsageAlert(c *gin.Context) {
	if !h.requireWeComUsageAlert(c) {
		return
	}
	accountID, ok := weComUsageAlertAccountID(c)
	if !ok {
		return
	}
	cfg, err := h.wecomUsageAlert.GetConfig(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// UpdateWeComUsageAlert PUT /admin/accounts/:id/wecom-usage-alert
func (h *AccountHandler) UpdateWeComUsageAlert(c *gin.Context) {
	if !h.requireWeComUsageAlert(c) {
		return
	}
	accountID, ok := weComUsageAlertAccountID(c)
	if !ok {
		return
	}
	var req service.WeComUsageAlertConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.wecomUsageAlert.UpdateConfig(c.Request.Context(), accountID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// TestWeComUsageAlert POST /admin/accounts/:id/wecom-usage-alert/test
func (h *AccountHandler) TestWeComUsageAlert(c *gin.Context) {
	if !h.requireWeComUsageAlert(c) {
		return
	}
	accountID, ok := weComUsageAlertAccountID(c)
	if !ok {
		return
	}
	cfg, err := h.wecomUsageAlert.TestSend(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}
