package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
)

type commercialModelCandidate struct {
	SupportedModel
	UpstreamModel    string
	Endpoints        []string
	RequestDependent bool
}

type commercialModelKey struct{ platform, name string }
type commercialCatalogBuilder struct {
	group         *Group
	candidates    map[commercialModelKey]commercialModelCandidate
	accountOwners map[string]map[string]bool
}

// commercialModelCandidates enumerates finite configured public IDs. Global
// price catalogs never add models; this read does not contact a provider.
func (s *ModelPlazaService) commercialModelCandidates(ctx context.Context, group *Group, channel *Channel) ([]commercialModelCandidate, error) {
	routes, accounts, err := s.commercialCatalogInputs(ctx, group)
	if err != nil {
		return nil, err
	}
	builder := commercialCatalogBuilder{group: group, candidates: make(map[commercialModelKey]commercialModelCandidate), accountOwners: make(map[string]map[string]bool)}
	builder.seedConfiguredModels(channel, accounts, routes)
	names := make(map[string]string)
	for key, candidate := range builder.candidates {
		names[key.name] = candidate.Name
	}
	for lower, name := range names {
		builder.resolvePublicModel(lower, name, routes)
	}
	out := make([]commercialModelCandidate, 0, len(builder.candidates))
	for _, candidate := range builder.candidates {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *ModelPlazaService) commercialCatalogInputs(ctx context.Context, group *Group) ([]CompositeModelRoute, []Account, error) {
	var routes []CompositeModelRoute
	if group.Platform == PlatformComposite && s.commercialRoutes != nil {
		rows, err := s.commercialRoutes.ListByGroup(ctx, group.ID, false)
		if err != nil {
			return nil, nil, fmt.Errorf("commercial model routes: %w", err)
		}
		for _, route := range rows {
			if route.Enabled {
				routes = append(routes, route)
			}
		}
	}
	var accounts []Account
	if s.commercialAccounts != nil {
		platforms := []string{group.Platform}
		if group.Platform == PlatformComposite {
			platforms = domain.ConcretePlatformIDs()
		}
		var err error
		accounts, err = s.commercialAccounts.ListModelAvailabilityCandidates(ctx, &group.ID, platforms, false)
		if err != nil {
			return nil, nil, fmt.Errorf("commercial account models: %w", err)
		}
	}
	return routes, accounts, nil
}

func (b *commercialCatalogBuilder) add(model SupportedModel, upstream string, endpoints []string) {
	model.Name = strings.TrimSpace(model.Name)
	if model.Name == "" || strings.Contains(model.Name, "*") || !domain.IsConcretePlatform(model.Platform) ||
		(b.group.Platform != PlatformComposite && model.Platform != b.group.Platform) || !b.group.ModelAllowlist.Allows(model.Name) {
		return
	}
	key := commercialModelKey{model.Platform, strings.ToLower(model.Name)}
	old, exists := b.candidates[key]
	if exists && upstream == "" {
		return
	}
	dependent := old.RequestDependent || (exists && old.UpstreamModel != "" && old.UpstreamModel != upstream)
	if exists {
		endpoints = append(append([]string{}, old.Endpoints...), endpoints...)
	}
	b.candidates[key] = commercialModelCandidate{SupportedModel: model, UpstreamModel: upstream, Endpoints: dedupeAndSortModelIDs(endpoints), RequestDependent: dependent}
}

func (b *commercialCatalogBuilder) seedConfiguredModels(channel *Channel, accounts []Account, routes []CompositeModelRoute) {
	if channel != nil {
		for _, model := range channel.SupportedModels() {
			b.add(model, "", nil)
		}
	}
	for _, pricing := range b.group.ModelPricing {
		platform := pricing.Platform
		if platform == "" && b.group.Platform != PlatformComposite {
			platform = b.group.Platform
		}
		for _, model := range pricing.Models {
			p := pricing.Clone()
			b.add(SupportedModel{Name: model, Platform: platform, Pricing: &p}, "", nil)
		}
	}
	for _, account := range accounts {
		for model, target := range stringMappingFromRaw(account.Credentials["model_mapping"]) {
			if strings.TrimSpace(target) == "" {
				continue
			}
			b.add(SupportedModel{Name: model, Platform: account.Platform}, "", nil)
			lower := strings.ToLower(strings.TrimSpace(model))
			if b.accountOwners[lower] == nil {
				b.accountOwners[lower] = make(map[string]bool)
			}
			b.accountOwners[lower][account.Platform] = true
		}
	}
	for _, route := range routes {
		if route.MatchType == CompositeRouteMatchExact {
			b.add(SupportedModel{Name: route.PublicModel, Platform: route.TargetPlatform}, "", nil)
		}
	}
}

func commercialRoutesForModel(routes []CompositeModelRoute, name string) []CompositeModelRoute {
	endpoints := []string{CompositeRouteEndpointAny, CompositeRouteEndpointMessages, CompositeRouteEndpointCountTokens, CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointEmbeddings, CompositeRouteEndpointImages, CompositeRouteEndpointGemini}
	selected := make(map[int64]CompositeModelRoute)
	for _, endpoint := range endpoints {
		if route, ok := matchCompositeRoute(routes, name, endpoint); ok {
			selected[route.ID] = route
		}
	}
	ordered := make([]CompositeModelRoute, 0, len(selected))
	for _, route := range selected {
		ordered = append(ordered, route)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return ordered
}

func (b *commercialCatalogBuilder) resolvePublicModel(lower, name string, routes []CompositeModelRoute) {
	selected := commercialRoutesForModel(routes, name)
	if len(selected) == 0 {
		if b.group.Platform == PlatformComposite {
			b.filterUnroutedSuppliers(lower, name)
		}
		return
	}
	for key := range b.candidates {
		if key.name == lower {
			delete(b.candidates, key)
		}
	}
	for _, route := range selected {
		endpoint := normalizeCompositeRouteEndpoint(route.Endpoint)
		if !commercialRouteEndpointSupported(route.TargetPlatform, endpoint) {
			continue
		}
		upstream := strings.TrimSpace(route.UpstreamModel)
		if upstream == "" {
			upstream = name
		}
		b.add(SupportedModel{Name: name, Platform: route.TargetPlatform}, upstream, []string{endpoint})
	}
}

func (b *commercialCatalogBuilder) filterUnroutedSuppliers(lower, name string) {
	owners := b.accountOwners[lower]
	target := ""
	if len(owners) == 1 {
		for platform := range owners {
			target = platform
		}
	} else if len(owners) == 0 {
		target, _ = DetectModelPlatform(name)
	}
	for key := range b.candidates {
		if key.name == lower && (target == "" || key.platform != target) {
			delete(b.candidates, key)
		}
	}
}

// These are existing ingress constraints, not new provider protocol promises.
func commercialRouteEndpointSupported(platform, endpoint string) bool {
	spec, ok := domain.LookupPlatform(platform)
	if !ok {
		return false
	}
	switch endpoint {
	case CompositeRouteEndpointAny:
		return true
	case CompositeRouteEndpointEmbeddings:
		return platform == PlatformOpenAI
	case CompositeRouteEndpointImages:
		return platform == PlatformOpenAI || platform == PlatformGrok
	case CompositeRouteEndpointGemini:
		return spec.Gateway == domain.PlatformGatewayGemini || spec.Gateway == domain.PlatformGatewayAntigravity
	default:
		return platform != PlatformTypeSafe
	}
}
