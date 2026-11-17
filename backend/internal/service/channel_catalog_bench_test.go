//go:build unit

package service

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// catalogBenchFixture mirrors the production shape behind /channels/available:
// hundreds of groups, each bound to its own channel with a single priced model.
func catalogBenchFixture(t testing.TB, n int) (*ModelPlazaService, []Group) {
	type pm struct{ platform, model string }
	shapes := []pm{
		{PlatformOpenAI, "gpt-5.4"}, {PlatformOpenAI, "gpt-5.6-luna"}, {PlatformAnthropic, "claude-sonnet-4-6"},
		{PlatformGemini, "gemini-2.5-pro"}, {PlatformGrok, "grok-4"}, {PlatformKimi, "kimi-k2.5"},
		{PlatformDeepseek, "deepseek-chat"}, {PlatformOpenAI, "gpt-5.4"},
	}
	channels := make([]Channel, 0, n)
	groups := make([]Group, 0, n)
	for i := 0; i < n; i++ {
		s := shapes[i%len(shapes)]
		id := int64(i + 1)
		channels = append(channels, plazaPricedChannel(id, fmt.Sprintf("ch-%d", i), []int64{id}, s.platform, s.model))
		groups = append(groups, Group{ID: id, Name: fmt.Sprintf("g-%d", i), Platform: s.platform, Status: StatusActive, RateMultiplier: 1, LongContextPricingEnabled: true})
	}
	return newPlazaServiceWithBilling(channels, groups, nil, benchPricing(t)), groups
}

func benchPricing(t testing.TB) *PricingService {
	body := []byte(openAILadderCatalogJSON)
	// Optional: point at a copy of the production model_pricing.json for a
	// realistic catalog size.
	if p := os.Getenv("SUB2API_BENCH_PRICING_JSON"); p != "" {
		var err error
		body, err = os.ReadFile(p)
		require.NoError(t, err)
	}
	s := &PricingService{}
	data, err := s.parsePricingData(body)
	require.NoError(t, err)
	s.pricingData = data
	return s
}

func BenchmarkListAvailableChannelCatalog(b *testing.B) {
	svc, groups := catalogBenchFixture(b, 372)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := svc.ListAvailableChannelCatalog(context.Background(), groups)
		require.NoError(b, err)
		require.Len(b, out, 372)
	}
}
