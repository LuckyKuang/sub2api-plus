package service

import (
	"context"
	"time"
)

// TokenCostRequest 通用网关 token 计费请求。
type TokenCostRequest struct {
	Ctx             context.Context
	Model           string
	Group           *Group
	Tokens          UsageTokens
	RateMultiplier  float64
	PricingAt       time.Time
	ServiceTier     string
	ReasoningEffort string
	Resolver        *ModelPricingResolver
	// Resolved 为调用方预先解析的定价（Resolver.Resolve 的结果），nil 表示未解析。
	Resolved *ResolvedPricing
}

// CalculateTokenCostForRequest uses the same price chain for grouped and
// ungrouped gateway requests, including the resolved concrete platform fallback.
// 模型广场的阶梯表查询与网关使用同一入口，保证展示与扣费同源。
func (s *BillingService) CalculateTokenCostForRequest(req TokenCostRequest) (*CostBreakdown, error) {
	if frozen := billingServiceForContext(req.Ctx, s); frozen != s {
		return frozen.CalculateTokenCostForRequest(req)
	}
	if req.Resolver == nil {
		req.Resolver = NewModelPricingResolver(nil, s)
	}
	return s.CalculateCostUnified(s.tokenCostInput(req, req.Resolved))
}

func (s *BillingService) tokenCostInput(req TokenCostRequest, resolved *ResolvedPricing) CostInput {
	input := CostInput{
		Ctx:             req.Ctx,
		Model:           req.Model,
		Group:           req.Group,
		Tokens:          req.Tokens,
		RequestCount:    1,
		RateMultiplier:  req.RateMultiplier,
		PricingAt:       req.PricingAt,
		ServiceTier:     req.ServiceTier,
		ReasoningEffort: req.ReasoningEffort,
		Resolver:        req.Resolver,
		Resolved:        resolved,
	}
	if req.Group != nil {
		gid := req.Group.ID
		input.GroupID = &gid
	}
	return input
}
