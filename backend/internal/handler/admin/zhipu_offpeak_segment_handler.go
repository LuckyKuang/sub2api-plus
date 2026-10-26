package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// ZhipuOffPeakSegmentManager exposes only gateway-owned segment handles. Remote
// ticket IDs, task IDs, credential snapshots and proxy secrets stay private.
type ZhipuOffPeakSegmentManager interface {
	CurrentSegment(context.Context, *service.Account) (*service.ZhipuOffPeakSegment, error)
	TerminateSegment(context.Context, *service.Account, string, string) error
}

func (h *ZhipuOAuthHandler) SetOffPeakTicketManager(manager ZhipuOffPeakSegmentManager) {
	h.offPeakTickets = manager
}

func (h *ZhipuOAuthHandler) offPeakSegmentAccount(c *gin.Context) (*service.Account, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", "off-peak segment requires a valid account ID")
	}
	if h == nil || h.adminService == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_STORE_UNAVAILABLE", "off-peak segment management is unavailable")
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != id || account.Platform != service.PlatformZhipu || account.GetCredential("plan_kind") != service.ZhipuPlanOffPeak ||
		(account.Type != service.AccountTypeOAuth && account.Type != service.AccountTypeAPIKey) {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", "off-peak segment requires a ZCode off-peak account")
	}
	if h.offPeakTickets == nil {
		return nil, infraerrors.New(http.StatusNotImplemented, "ZHIPU_OFFPEAK_DURABLE_REQUIRED", "off-peak segment management requires durable persistence")
	}
	return account, nil
}

// GetOffPeakSegment returns the current gateway record/generation and state.
// GET /api/v1/admin/zhipu/accounts/:id/off-peak/segment
func (h *ZhipuOAuthHandler) GetOffPeakSegment(c *gin.Context) {
	account, err := h.offPeakSegmentAccount(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	segment, err := h.offPeakTickets.CurrentSegment(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, segment)
}

type ZhipuTerminateOffPeakSegmentRequest struct {
	RecordID   string `json:"record_id" binding:"required,max=128"`
	Generation string `json:"generation" binding:"required,max=128"`
}

// TerminateOffPeakSegment retires exactly the inspected gateway segment. A
// successful response acknowledges the durable intent, not upstream settlement.
// POST /api/v1/admin/zhipu/accounts/:id/off-peak/segment/terminate
func (h *ZhipuOAuthHandler) TerminateOffPeakSegment(c *gin.Context) {
	var request ZhipuTerminateOffPeakSegmentRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.RecordID) == "" || strings.TrimSpace(request.Generation) == "" {
		response.BadRequest(c, "record_id and generation are required")
		return
	}
	account, err := h.offPeakSegmentAccount(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.offPeakTickets.TerminateSegment(c.Request.Context(), account, request.RecordID, request.Generation); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"accepted": true})
}
