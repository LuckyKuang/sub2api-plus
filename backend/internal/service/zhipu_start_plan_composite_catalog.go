package service

import (
	"context"
	"strings"
)

// FilterCompositeStartPlanModels applies Start proof after route aliases and
// account catalogs have been merged. An explicit route can replace even an
// account-published name with a different platform/model. Its public spelling
// and a group allowlist cannot establish permission for that actual target.
// The boolean preserves an all-Start group's authoritative empty result.
func (s *GatewayService) FilterCompositeStartPlanModels(ctx context.Context, groupID *int64, models []string, endpoint string) ([]string, bool, error) {
	if s == nil || s.accountRepo == nil {
		return models, false, nil
	}
	var accounts []Account
	var err error
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil {
		return nil, true, err
	}
	hasStart, allStart := false, len(accounts) > 0
	for i := range accounts {
		if isZhipuStartPlan(&accounts[i]) {
			hasStart = true
		} else {
			allStart = false
		}
	}
	if !hasStart || groupID == nil || s.compositeResolver == nil || s.compositeResolver.repo == nil {
		return models, allStart, nil
	}
	routes, err := s.compositeResolver.repo.ListByGroup(ctx, *groupID, false)
	if err != nil {
		return nil, true, err
	}
	enabled := make([]CompositeModelRoute, 0, len(routes))
	for _, route := range routes {
		if route.Enabled {
			enabled = append(enabled, route)
		}
	}
	filtered := make([]string, 0, len(models))
	for _, model := range models {
		if compositeStartPlanModelVisible(ctx, accounts, enabled, model, endpoint) {
			filtered = append(filtered, model)
		}
	}
	return filtered, allStart, nil
}

func compositeStartPlanModelVisible(ctx context.Context, accounts []Account, routes []CompositeModelRoute, model, endpoint string) bool {
	if endpoint != "" {
		route, matched := matchCompositeRoute(routes, model, normalizeCompositeRouteEndpoint(endpoint))
		return !matched || compositeStartPlanRouteVisible(ctx, accounts, route, model)
	}
	// /models covers all endpoint families. Use each concrete endpoint's
	// winning route; a lower-priority route never proves a shadowed target.
	matched := false
	for _, candidate := range []string{CompositeRouteEndpointMessages, CompositeRouteEndpointCountTokens,
		CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions,
		CompositeRouteEndpointEmbeddings, CompositeRouteEndpointImages, CompositeRouteEndpointGemini} {
		route, ok := matchCompositeRoute(routes, model, candidate)
		if !ok {
			continue
		}
		matched = true
		if compositeStartPlanRouteVisible(ctx, accounts, route, model) {
			return true
		}
	}
	return !matched
}

func compositeStartPlanRouteVisible(ctx context.Context, accounts []Account, route CompositeModelRoute, model string) bool {
	if route.TargetPlatform != PlatformZhipu {
		return true
	}
	target := strings.TrimSpace(route.UpstreamModel)
	if target == "" {
		target = model
	}
	hasStart := false
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != PlatformZhipu {
			continue
		}
		if isZhipuStartPlan(account) {
			hasStart = true
			if startPlanCatalogObservation(ctx, account) == nil {
				continue
			}
		}
		if account.IsModelSupported(target) {
			return true
		}
	}
	// Other Zhipu account types retain their established catalog policy.
	return !hasStart
}
