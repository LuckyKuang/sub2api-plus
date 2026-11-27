package service

import (
	"context"
	"fmt"
	"strings"
)

// QualityCompositeResolver 把复合分组里的公开模型解析成具体平台与上游模型。
//
// 端点语义：matchCompositeRoute 只匹配“声明端点 == 请求端点”或“声明 any”的路由，
// 因此请求 endpoint=any 不会命中声明为 messages/responses 的路由。这里按文本生成
// 端点顺序逐个尝试，取第一个命中，避免只有配置了具体端点的复合路由全部解析失败。
type QualityCompositeResolver struct {
	resolver *CompositeRouteResolver
}

func NewQualityCompositeResolver(resolver *CompositeRouteResolver) *QualityCompositeResolver {
	return &QualityCompositeResolver{resolver: resolver}
}

var qualityCompositeEndpointOrder = []string{
	CompositeRouteEndpointMessages,
	CompositeRouteEndpointResponses,
	CompositeRouteEndpointChatCompletions,
	CompositeRouteEndpointGemini,
}

// Resolve 返回可用的解析决策；无法解析时返回 ErrQualityCompositeUnresolved 包装的错误。
func (r *QualityCompositeResolver) Resolve(ctx context.Context, groupID int64, model, endpoint string) (CompositeRouteDecision, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return CompositeRouteDecision{}, fmt.Errorf("%w: 模型不能为空", ErrQualityCompositeUnresolved)
	}
	if r == nil || r.resolver == nil {
		// 没有复合路由仓储时按模型名探测器兜底，与网关行为一致。
		if platform, ok := DetectModelPlatform(model); ok {
			return CompositeRouteDecision{Matched: true, PublicModel: model, TargetPlatform: platform,
				UpstreamModel: model, Endpoint: normalizeCompositeRouteEndpoint(endpoint),
				Source: CompositeRouteSourceDetector}, nil
		}
		return CompositeRouteDecision{}, fmt.Errorf("%w: 复合路由不可用", ErrQualityCompositeUnresolved)
	}

	endpoint = normalizeCompositeRouteEndpoint(endpoint)
	if endpoint != CompositeRouteEndpointAny {
		decision, err := r.resolver.Resolve(ctx, groupID, model, endpoint)
		return r.finalize(decision, err)
	}

	for _, candidate := range qualityCompositeEndpointOrder {
		decision, err := r.resolver.Resolve(ctx, groupID, model, candidate)
		if err != nil {
			return decision, err
		}
		if decision.Matched {
			return r.finalize(decision, nil)
		}
	}
	// 最后回退到 any：覆盖显式声明 any 的路由与账号归属/模型名探测器。
	decision, err := r.resolver.Resolve(ctx, groupID, model, CompositeRouteEndpointAny)
	return r.finalize(decision, err)
}

func (r *QualityCompositeResolver) finalize(decision CompositeRouteDecision, err error) (CompositeRouteDecision, error) {
	if err != nil {
		return decision, err
	}
	if !decision.Matched {
		reason := decision.Reason
		if reason == "" {
			reason = "没有匹配的复合路由"
		}
		return decision, fmt.Errorf("%w: %s", ErrQualityCompositeUnresolved, reason)
	}
	if !isConcreteRequestPlatform(decision.TargetPlatform) {
		return decision, fmt.Errorf("%w: 目标平台 %q 不是具体平台", ErrQualityCompositeUnresolved, decision.TargetPlatform)
	}
	if strings.TrimSpace(decision.UpstreamModel) == "" {
		decision.UpstreamModel = decision.PublicModel
	}
	return decision, nil
}
