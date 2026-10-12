package service

import (
	"context"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/firstbyte"
)

func beginFirstByteTiming(ctx context.Context, start time.Time) (context.Context, *firstbyte.Timing) {
	return firstbyte.WithStart(ctx, start)
}
