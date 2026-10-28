package handler

import (
	"context"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) releaseUnfinishedTask(reservation *service.GatewayTaskReservation, reason string) {
	if reservation == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.unfinishedTasks.Release(ctx, reservation, reason); err != nil {
		logger.L().Error("gateway.unfinished_task_release_failed", zap.String("reservation_id", reservation.ID), zap.Error(err))
	}
}
