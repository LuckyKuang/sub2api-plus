//go:build unit || !integration

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSTurnPricingForTurnOr(t *testing.T) {
	fallback := time.Date(2024, time.January, 2, 2, 0, 0, 0, time.UTC)

	t.Run("frozen time takes precedence", func(t *testing.T) {
		frozen := fallback.Add(time.Minute)
		var p openAIWSTurnPricing
		p.freeze(1, frozen)
		require.Equal(t, frozen, p.forTurnOr(1, fallback))
	})

	t.Run("zero value falls back to turn start", func(t *testing.T) {
		var p openAIWSTurnPricing
		require.Equal(t, fallback, p.forTurnOr(1, fallback))
	})
}

func TestOpenAIWSTurnSnapshotsDoNotOverwriteDeferredSettlement(t *testing.T) {
	var pricing openAIWSTurnPricing
	gateway := &service.OpenAIGatewayService{}
	key := &service.APIKey{Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, RateMultiplier: 1}}
	firstCtx := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Now().Add(-time.Minute), "first-model")
	first := service.BillingPricingSnapshotFromContext(firstCtx)
	pricing.freezeSnapshot(1, first)
	key.Group.RateMultiplier = 2
	secondCtx := gateway.WithBillingPricingSnapshot(service.WithoutBillingPricingSnapshot(firstCtx), key, time.Now(), "second-model")
	second := service.BillingPricingSnapshotFromContext(secondCtx)
	require.NotSame(t, first, second)
	pricing.freezeSnapshot(2, second)
	require.Same(t, first, pricing.snapshotForTurn(1), "the next turn must not change pending usage from the previous turn")
	require.Same(t, second, pricing.snapshotForTurn(2), "later turns use their own generation")
	pricing.freezeSnapshot(3, second)
	require.Nil(t, pricing.snapshotForTurn(1))
	require.Same(t, second, pricing.snapshotForTurn(2))
	require.Len(t, pricing.snapshots, 2)
}

// 长连接跨峰谷时，每轮独立定价；下一轮开始不能覆盖上一轮待结算的时刻。
func TestOpenAIWSTurnPricingFreezePerTurn(t *testing.T) {
	var p openAIWSTurnPricing
	turn1 := time.Now().Add(-time.Hour)
	turn2 := time.Now()

	p.freeze(1, turn1)
	require.Equal(t, turn1, p.forTurnOr(1, time.Time{}))

	p.freeze(2, turn2)
	require.Equal(t, turn2, p.forTurnOr(2, time.Time{}), "后续 turn 必须使用自己的定价时刻")
	require.Equal(t, turn1, p.forTurnOr(1, time.Time{}), "下一轮开始后，上一轮仍按原时刻结算")
	fallback := turn1.Add(-time.Minute)
	p.freeze(3, turn2.Add(time.Minute))
	require.Equal(t, fallback, p.forTurnOr(1, fallback))
	require.Len(t, p.times, 2)
}
