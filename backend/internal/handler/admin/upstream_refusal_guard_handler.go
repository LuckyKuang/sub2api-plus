package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamRefusalGuardHandler struct {
	guard *service.UpstreamRefusalGuardService
}

func NewUpstreamRefusalGuardHandler(guard *service.UpstreamRefusalGuardService) *UpstreamRefusalGuardHandler {
	return &UpstreamRefusalGuardHandler{guard}
}
func (h *UpstreamRefusalGuardHandler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.guard.Config(c.Request.Context())})
}
func (h *UpstreamRefusalGuardHandler) UpdateConfig(c *gin.Context) {
	var cfg service.UpstreamRefusalGuardConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.guard.UpdateConfig(c.Request.Context(), cfg); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": cfg})
}
func (h *UpstreamRefusalGuardHandler) ListUsers(c *gin.Context) {
	items, err := h.guard.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "Unable to list refusal guard users"})
		return
	}
	c.JSON(200, gin.H{"data": items})
}
func (h *UpstreamRefusalGuardHandler) ClearUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "Invalid user ID"})
		return
	}
	n, err := h.guard.ClearUser(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"error": "Unable to clear user"})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"deleted": n}})
}
func (h *UpstreamRefusalGuardHandler) ListSessions(c *gin.Context) {
	items, err := h.guard.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "Unable to list refusal guard sessions"})
		return
	}
	id, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	filtered := make([]service.RefusalSession, 0, len(items))
	for _, item := range items {
		if id == 0 || item.UserID == id {
			filtered = append(filtered, item)
		}
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	start := (page - 1) * size
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	c.JSON(200, gin.H{"data": filtered[start:end], "total": len(filtered), "page": page, "page_size": size})
}
func (h *UpstreamRefusalGuardHandler) ClearSession(c *gin.Context) {
	var req struct {
		BlockKey string `json:"block_key"`
	}
	if c.ShouldBindJSON(&req) != nil || len(strings.TrimSpace(req.BlockKey)) != 64 {
		c.JSON(400, gin.H{"error": "Invalid block key"})
		return
	}
	n, err := h.guard.ClearSession(c.Request.Context(), req.BlockKey)
	if err != nil {
		c.JSON(400, gin.H{"error": "Unable to clear session"})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"deleted": n}})
}
func (h *UpstreamRefusalGuardHandler) ClearAll(c *gin.Context) {
	n, err := h.guard.ClearAll(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "Unable to clear refusal guard"})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"deleted": n}})
}
