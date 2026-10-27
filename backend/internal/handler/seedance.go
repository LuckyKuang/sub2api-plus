package handler

import (
	"context"
	"mime"
	"net/http"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SeedanceTasks exposes Ark's native asynchronous video task protocol.
func (h *OpenAIGatewayHandler) SeedanceTasks(c *gin.Context) {
	if c.Request.Method == http.MethodPost && c.GetHeader("Content-Type") != "" {
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			h.errorResponse(c, http.StatusUnsupportedMediaType, "invalid_request_error", "Seedance requires application/json")
			return
		}
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key.Group == nil || (key.Group.Platform != service.PlatformOpenAI && key.Group.Platform != service.PlatformComposite) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Seedance requires an OpenAI or composite group")
		return
	}
	endpoint := service.SeedanceEndpointCreate
	setActualUpstreamEndpoint(c, EndpointSeedanceTasks)
	taskID := ""
	if c.Request.Method != http.MethodPost {
		taskID = service.SeedanceTaskKey(c.Param("task_id"))
		endpoint = service.SeedanceEndpointStatus
		if c.Request.Method == http.MethodDelete {
			endpoint = service.SeedanceEndpointDelete
		}
	}
	h.handleGrokMedia(c, endpoint, taskID)
}

// Ark reports actual completion tokens. Never infer tokens from duration or use
// Grok's per-second video tariff. Repeated polls share the durable task dedup key.
func prepareSeedanceCompletionBilling(ctx context.Context, h *OpenAIGatewayHandler, key *service.APIKey, subject middleware.AuthSubject, taskID string, result *service.OpenAIForwardResult) (context.Context, *service.OpenAIForwardResult) {
	if result == nil || result.Usage.OutputTokens <= 0 {
		return ctx, nil
	}
	pending, err := h.gatewayService.LoadGrokVideoPendingBilling(ctx, taskID, subject.UserID, key.ID)
	if err != nil || pending == nil {
		logger.L().Error("seedance.billing_skipped_missing_pending", zap.String("request_id", taskID), zap.String("reason", "create_time_pricing_record_missing"), zap.Error(err))
		return ctx, nil
	}
	restored, err := service.RestoreBillingPricingSnapshot(ctx, pending.PricingSnapshot)
	if err == nil {
		err = service.ValidateBillingPricingSnapshot(restored)
	}
	if err != nil {
		logger.L().Error("seedance.pricing_snapshot_unavailable", zap.String("request_id", taskID), zap.Error(err))
		return ctx, nil
	}
	ctx = restored
	merged := *result
	merged.Model = pending.Model
	merged.BillingModel = firstNonEmptyString(pending.BillingModel, pending.Model)
	merged.UpstreamModel = firstNonEmptyString(pending.UpstreamModel, result.UpstreamModel)
	if len(pending.PricingSnapshot) > 0 {
		var priceErr error
		priced := false
		for _, model := range []string{merged.BillingModel, merged.UpstreamModel, merged.Model} {
			if model == "" {
				continue
			}
			priceErr = service.ValidateBillingPricingMeter(ctx, model, "token")
			if priceErr == nil {
				priced = true
				break
			}
		}
		if !priced {
			logger.L().Error("seedance.price_evidence_unavailable", zap.String("request_id", taskID), zap.Error(priceErr))
			return ctx, nil
		}
	}
	claim, err := h.gatewayService.ClaimGrokVideoBilling(ctx, taskID, subject.UserID, key.ID)
	if err != nil || claim == nil {
		return ctx, nil
	}
	merged.VideoBillingClaim = claim
	merged.RequestID = service.StableGrokVideoBillingRequestID(taskID)
	merged.ResponseID = taskID
	merged.Duration = service.GrokVideoE2EDuration(pending.CreatedAt, time.Now())
	return ctx, &merged
}
