//go:build unit || !integration

package handler

import (
	"testing"
	"time"

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
