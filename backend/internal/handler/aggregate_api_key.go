package handler

import (
	"sort"
	"strconv"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *APIKeyHandler) GetAggregateNativeAccess(c *gin.Context) {
	subject, ok := middleware2.GetReadSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid key ID")
		return
	}
	key, err := h.apiKeyService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}
	if !key.IsAggregate() {
		response.BadRequest(c, "Aggregate key is required")
		return
	}
	key, err = h.apiKeyService.AggregateScope(c.Request.Context(), key)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	models, err := h.apiKeyService.AggregateNativeModels(c.Request.Context(), key)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	seen := map[string]bool{}
	for _, model := range models {
		for _, endpoint := range model.Endpoints {
			seen[endpoint] = true
		}
	}
	endpoints := []string{}
	for endpoint := range seen {
		endpoints = append(endpoints, endpoint)
	}
	sort.Strings(endpoints)
	response.Success(c, gin.H{"models": models, "endpoints": endpoints, "conversion_endpoints": []string{}})
}
