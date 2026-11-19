//go:build unit || !integration

package service

import "github.com/LuckyKuang/sub2api-plus/internal/config"

func newTestBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func newChannelServiceWithPricings(groupID int64, pricings []ChannelModelPricing) *ChannelService {
	ch := Channel{
		ID:           1,
		Name:         "codex-channel",
		Status:       StatusActive,
		ModelPricing: pricings,
		GroupIDs:     []int64{groupID},
	}
	cs := &ChannelService{}
	cs.cache.Store(populateChannelCache([]Channel{ch}, map[int64]string{groupID: PlatformOpenAI}))
	return cs
}
