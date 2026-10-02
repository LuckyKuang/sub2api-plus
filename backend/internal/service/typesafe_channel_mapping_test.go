//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TypeSafe 原生端点只支持 Jev：渠道映射只影响用量记录的
// requested/channel-mapped/upstream 语义，不得为了让 mapping UI 生效而改写
// 请求体协议。ResolveChannelMappingAndRestrict 的第二个返回值是 bool（不是被
// 丢弃的 error）且当前恒为 false；真正的型号限制检查在调度阶段。
func TestTypeSafeChannelMappingSemanticsAndSchedulingRestriction(t *testing.T) {
	mapped := Channel{
		ID:                 31,
		Status:             StatusActive,
		GroupIDs:           []int64{9},
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformTypeSafe, Models: []string{"jev-latest"}},
		},
		ModelMapping: map[string]map[string]string{
			PlatformTypeSafe: {"jev-latest": "jev-latest-alias"},
		},
	}
	svc := newTestChannelService(makeStandardRepo(mapped, map[int64]string{9: PlatformTypeSafe}))
	gateway := &GatewayService{channelService: svc}
	groupID := int64(9)

	mapping, restricted := svc.ResolveChannelMappingAndRestrict(context.Background(), &groupID, "jev-latest")
	require.False(t, restricted, "the second value is a bool and stays false; the limit check is in scheduling")
	require.True(t, mapping.Mapped)
	require.Equal(t, "jev-latest-alias", mapping.MappedModel)
	require.Equal(t, int64(31), mapping.ChannelID)
	require.Equal(t, BillingModelSourceChannelMapped, mapping.BillingModelSource)

	// The native payload keeps its validated model; only the usage record
	// carries the mapping chain.
	fields := mapping.ToUsageFields("jev-latest", "")
	require.Equal(t, "jev-latest", fields.OriginalModel)
	require.Equal(t, "jev-latest-alias", fields.ChannelMappedModel)
	require.Equal(t, int64(31), fields.ChannelID)
	require.Equal(t, BillingModelSourceChannelMapped, fields.BillingModelSource)
	require.Equal(t, "jev-latest→jev-latest-alias", fields.ModelMappingChain)
	require.False(t, gateway.checkChannelPricingRestriction(context.Background(), &groupID, "jev-latest"),
		"an unrestricted channel is admitted by the scheduling-stage check")

	// Restricting the list still happens in scheduling, independently of the
	// always-false mapping return value.
	allowed := Channel{
		ID:             32,
		Status:         StatusActive,
		GroupIDs:       []int64{9},
		RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformTypeSafe, Models: []string{"jev-latest"}},
		},
	}
	allowedSvc := newTestChannelService(makeStandardRepo(allowed, map[int64]string{9: PlatformTypeSafe}))
	_, stillFalse := allowedSvc.ResolveChannelMappingAndRestrict(context.Background(), &groupID, "jev-latest")
	require.False(t, stillFalse)
	require.False(t, (&GatewayService{channelService: allowedSvc}).checkChannelPricingRestriction(context.Background(), &groupID, "jev-latest"))

	blocked := Channel{
		ID:             33,
		Status:         StatusActive,
		GroupIDs:       []int64{9},
		RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformTypeSafe, Models: []string{"some-other-model"}},
		},
	}
	blockedSvc := newTestChannelService(makeStandardRepo(blocked, map[int64]string{9: PlatformTypeSafe}))
	require.True(t, (&GatewayService{channelService: blockedSvc}).checkChannelPricingRestriction(context.Background(), &groupID, "jev-latest"),
		"the scheduling stage owns the restriction decision")
}
