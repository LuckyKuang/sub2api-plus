package admin

import (
	"strconv"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetUsageAlertService(svc *service.UsageAlertService) {
	if h == nil {
		return
	}
	h.usageAlert = svc
}

// SetWeComUsageAlertService is a legacy alias.
func (h *AccountHandler) SetWeComUsageAlertService(svc *service.WeComUsageAlertService) {
	h.SetUsageAlertService(svc)
}

func (h *AccountHandler) requireUsageAlert(c *gin.Context) bool {
	if h != nil && h.usageAlert != nil {
		return true
	}
	response.ErrorFrom(c, service.ErrUsageAlertUnavailable)
	return false
}

func usageAlertAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}

// GetUsageAlert GET /admin/accounts/:id/usage-alert
func (h *AccountHandler) GetUsageAlert(c *gin.Context) {
	if !h.requireUsageAlert(c) {
		return
	}
	accountID, ok := usageAlertAccountID(c)
	if !ok {
		return
	}
	cfg, err := h.usageAlert.GetConfig(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// UpdateUsageAlert PUT /admin/accounts/:id/usage-alert
func (h *AccountHandler) UpdateUsageAlert(c *gin.Context) {
	if !h.requireUsageAlert(c) {
		return
	}
	accountID, ok := usageAlertAccountID(c)
	if !ok {
		return
	}
	var req service.UsageAlertConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.usageAlert.UpdateConfig(c.Request.Context(), accountID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// TestUsageAlert POST /admin/accounts/:id/usage-alert/test
func (h *AccountHandler) TestUsageAlert(c *gin.Context) {
	if !h.requireUsageAlert(c) {
		return
	}
	accountID, ok := usageAlertAccountID(c)
	if !ok {
		return
	}
	var req service.UsageAlertTestRequest
	_ = c.ShouldBindJSON(&req) // body optional
	cfg, err := h.usageAlert.TestSend(c.Request.Context(), accountID, req.RuleID, req.Rule)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// Legacy WeCom route aliases.
func (h *AccountHandler) GetWeComUsageAlert(c *gin.Context)    { h.GetUsageAlert(c) }
func (h *AccountHandler) UpdateWeComUsageAlert(c *gin.Context) { h.UpdateUsageAlert(c) }
func (h *AccountHandler) TestWeComUsageAlert(c *gin.Context)   { h.TestUsageAlert(c) }
