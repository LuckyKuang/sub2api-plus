//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCommercialCatalogUsesPublicRoutesAndAllowlistWithoutChannel(t *testing.T) {
	g := Group{ID: 8, Platform: PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 7,
		ModelAllowlist:    GroupModelAllowlist{Enabled: true, Models: []string{"bundle/step"}},
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformStepFun: .5}},
		ModelPricing:      []ChannelModelPricing{{Platform: PlatformStepFun, Models: []string{"step-3.5-flash"}, InputPrice: testPtrFloat64(.000001), OutputPrice: testPtrFloat64(.000002)}},
	}
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 8, PublicModel: "bundle/step", MatchType: "exact", Endpoint: "chat_completions", TargetPlatform: PlatformStepFun, UpstreamModel: "step-3.5-flash", Enabled: true},
		{ID: 2, GroupID: 8, PublicModel: "hidden", MatchType: "exact", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-6-sol", Enabled: true},
		{ID: 3, GroupID: 8, PublicModel: "disabled", MatchType: "exact", TargetPlatform: PlatformStepFun, Enabled: false},
	}}, nil)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{g})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 1)
	o := out[0].Models[0]
	require.Equal(t, "bundle/step", o.Name)
	require.Equal(t, PlatformStepFun, o.Platform)
	require.Equal(t, []string{"chat_completions"}, o.Endpoints)
	require.Equal(t, "resolved", o.PriceStatus)
	require.InDelta(t, .000001, *o.Pricing.InputPrice, 1e-12)
	quote := ResolveBillingRateQuote(&g, o.Platform, o.Name, o.BillingUnit, nil, time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	require.Equal(t, 1.0, quote.EffectiveMultiplier)
	require.Equal(t, int64(7), quote.PolicyVersion)
}

func TestCommercialCatalogDoesNotPublishOtherSupplierDuplicateAlias(t *testing.T) {
	g := Group{ID: 8, Platform: PlatformComposite}
	ch := plazaPricedChannel(1, "Channel", []int64{8}, PlatformOpenAI, "shared")
	ch.ModelPricing = append(ch.ModelPricing, ChannelModelPricing{Platform: PlatformOpenCodeGo, Models: []string{"shared"}, InputPrice: testPtrFloat64(.000009)})
	svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
	svc.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 8, PublicModel: "shared", MatchType: "exact", Endpoint: "any", TargetPlatform: PlatformOpenCodeGo, UpstreamModel: "shared", Enabled: true}}}, nil)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{g})
	require.NoError(t, err)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, PlatformOpenCodeGo, out[0].Models[0].Platform)
	require.InDelta(t, .000009, *out[0].Models[0].Pricing.InputPrice, 1e-12)
}

type unavailableCommercialRoutes struct{ CompositeModelRouteRepository }

func (unavailableCommercialRoutes) ListByGroup(context.Context, int64, bool) ([]CompositeModelRoute, error) {
	return nil, errors.New("unavailable")
}

type reloadingCommercialRoutes struct {
	CompositeModelRouteRepository
	once   sync.Once
	reload func()
	routes []CompositeModelRoute
}

func (r *reloadingCommercialRoutes) ListByGroup(context.Context, int64, bool) ([]CompositeModelRoute, error) {
	r.once.Do(r.reload)
	return r.routes, nil
}

func TestCommercialCatalogPinsOnePricingGenerationAcrossReload(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5":   {InputCostPerToken: .000002, OutputCostPerToken: .000004, LiteLLMProvider: "openai", Mode: "chat"},
		"gpt-5.1": {InputCostPerToken: .000003, OutputCostPerToken: .000006, LiteLLMProvider: "openai", Mode: "chat"},
	}}
	svc := newPlazaServiceWithBilling(nil, nil, nil, pricing)
	svc.SetCommercialCatalogSources(&reloadingCommercialRoutes{
		reload: func() {
			pricing.mu.Lock()
			defer pricing.mu.Unlock()
			pricing.pricingData = map[string]*LiteLLMModelPricing{
				"gpt-5":   {InputCostPerToken: .00002, OutputCostPerToken: .00004, LiteLLMProvider: "openai", Mode: "chat"},
				"gpt-5.1": {InputCostPerToken: .00003, OutputCostPerToken: .00006, LiteLLMProvider: "openai", Mode: "chat"},
			}
		},
		routes: []CompositeModelRoute{
			{ID: 1, GroupID: 8, PublicModel: "public-a", MatchType: "exact", Endpoint: "responses", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Enabled: true},
			{ID: 2, GroupID: 8, PublicModel: "public-b", MatchType: "exact", Endpoint: "responses", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.1", Enabled: true},
		},
	}, nil)
	groups := []Group{{ID: 8, Platform: PlatformComposite}}
	for _, expected := range []map[string]float64{
		{"public-a": .000002, "public-b": .000003},
		{"public-a": .00002, "public-b": .00003},
	} {
		out, err := svc.ListAvailableCatalog(context.Background(), groups)
		require.NoError(t, err)
		require.Len(t, out[0].Models, 2)
		for _, offer := range out[0].Models {
			require.NotNil(t, offer.Pricing)
			require.InDelta(t, expected[offer.Name], *offer.Pricing.InputPrice, 1e-12)
			require.NotNil(t, offer.OfficialPricing)
			require.InDelta(t, expected[offer.Name], *offer.OfficialPricing.InputPrice, 1e-12)
		}
	}
}

func TestCommercialCatalogImageRequestCardKeepsIndependentBaseAndRequestPolicy(t *testing.T) {
	group := Group{ID: 8, Platform: PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 12,
		ImageRateIndependent: true, ImageRateMultiplier: 1.5,
		BillingRatePolicy: domain.BillingRatePolicy{ModelMultipliers: []domain.BillingModelRate{
			{Platform: PlatformOpenAI, Model: "public-image", BillingUnit: "request", Multiplier: 3},
			{Platform: PlatformOpenAI, Model: "public-image", BillingUnit: "image", Multiplier: 99},
		}},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"public-image"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(0.2)}},
	}
	routes := &compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 8, PublicModel: "public-image", MatchType: "exact", Endpoint: "images", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-image-2", Enabled: true}}}
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{8}, BillingModelSource: BillingModelSourceRequested}
	svc := newPlazaServiceWithBilling([]Channel{channel}, nil, nil, nil)
	svc.SetCommercialCatalogSources(routes, nil)
	catalog, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Len(t, catalog[0].Models, 1)
	offer := catalog[0].Models[0]
	require.Equal(t, "resolved", offer.PriceStatus)
	require.Equal(t, "request", offer.BillingUnit)
	require.InDelta(t, 0.2, *offer.Pricing.PerRequestPrice, 1e-12)
	personal := 0.4
	quote := offer.ResolveBillingRateQuote(&group, &personal, catalog[0].AsOf)
	require.Equal(t, "media", quote.BaseSource)
	require.InDelta(t, 1.5, quote.BaseMultiplier, 1e-12)
	require.InDelta(t, 3, quote.PolicyMultiplier, 1e-12, "request-metered pricing does not select the image-metered rule")
	require.InDelta(t, 4.5, quote.EffectiveMultiplier, 1e-12)
	require.InDelta(t, 0.9, *offer.Pricing.PerRequestPrice*quote.EffectiveMultiplier, 1e-12)
	group.ImageRateIndependent = false
	quote = offer.ResolveBillingRateQuote(&group, &personal, catalog[0].AsOf)
	require.Equal(t, "personal", quote.BaseSource)
	require.InDelta(t, 1.2, quote.EffectiveMultiplier, 1e-12)

	// The same public ID can be sent through either text or image ingress.
	// With an independent media base there is no one rate for that offer.
	group.ImageRateIndependent = true
	routes.routes = append(routes.routes, CompositeModelRoute{ID: 2, GroupID: 8, PublicModel: "public-image", MatchType: "exact", Endpoint: "chat_completions", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-image-2", Enabled: true})
	catalog, err = svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Len(t, catalog[0].Models, 1)
	require.Equal(t, "request_dependent", catalog[0].Models[0].PriceReason)
	require.Equal(t, "unknown", catalog[0].Models[0].BillingUnit)
	require.Nil(t, catalog[0].Models[0].Pricing)
}

func TestCommercialCatalogDependencyFailureIsNotEmptyValidCatalog(t *testing.T) {
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(unavailableCommercialRoutes{}, nil)
	_, err := svc.ListAvailableCatalog(context.Background(), []Group{{ID: 8, Platform: PlatformComposite}})
	require.ErrorContains(t, err, "commercial model routes")
}

func TestSaleCatalogOnlyReturnsRequestedActiveSubscriptionGroups(t *testing.T) {
	groups := []Group{
		{ID: 1, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, Platform: PlatformComposite},
		{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, Platform: PlatformComposite},
		{ID: 3, Status: StatusDisabled, SubscriptionType: SubscriptionTypeSubscription, Platform: PlatformComposite},
		{ID: 4, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, Platform: PlatformOpenAI},
	}
	svc := newPlazaServiceWithBilling(nil, groups, nil, nil)
	out, err := svc.ListSaleCatalog(context.Background(), []int64{1, 3, 4})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, int64(1), out[0].Group.ID)
	require.NotNil(t, out[0].Models)
	require.Empty(t, out[0].Models)
}

type commercialAccountsStub struct {
	byGroup  map[int64][]CommercialModelMapping
	groupIDs []int64
	err      error
	calls    int
}

func (r *commercialAccountsStub) ListCommercialModelMappings(_ context.Context, groupIDs []int64) (map[int64][]CommercialModelMapping, error) {
	r.calls++
	r.groupIDs = append([]int64(nil), groupIDs...)
	return r.byGroup, r.err
}

func TestCommercialCatalogIncludesEveryRegisteredSupplierWithoutProviderRequests(t *testing.T) {
	mappings := make([]CommercialModelMapping, 0)
	for _, spec := range domain.Platforms() {
		mappings = append(mappings, CommercialModelMapping{Platform: spec.ID, ModelMapping: map[string]any{spec.ID + "-public": "private-upstream"}})
	}
	repo := &commercialAccountsStub{byGroup: map[int64][]CommercialModelMapping{1: mappings}}
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(nil, repo)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{{ID: 1, Platform: PlatformComposite}})
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Len(t, out[0].Models, len(domain.Platforms()))
	for _, offer := range out[0].Models {
		require.NotEqual(t, "private-upstream", offer.Name)
	}
}

func TestCommercialCatalogReadsAuthorizedMappingsOnceAndPreservesModelPrices(t *testing.T) {
	// docs/COMPOSITE_GROUPS.md: finite public models retain their owning
	// supplier, obey the group's allowlist, and never expose private targets.
	// One directory read must not hydrate account/proxy/group entities per group.
	repo := &commercialAccountsStub{byGroup: map[int64][]CommercialModelMapping{
		1: {
			{Platform: PlatformOpenAI, ModelMapping: map[string]any{"public-a": "private-a", "bad": 42, "empty": " ", "wild*": "target"}},
			{Platform: PlatformAnthropic, ModelMapping: map[string]any{"wrong-platform": "target"}},
		},
		2: {
			{Platform: PlatformOpenCodeGo, ModelMapping: map[string]any{"public-b": "private-b", "not-allowed": "target"}},
			{Platform: "unknown-platform", ModelMapping: map[string]any{"unknown": "target"}},
		},
		999: {{Platform: PlatformOpenAI, ModelMapping: map[string]any{"unauthorized": "private-secret"}}},
	}}
	groups := []Group{
		{ID: 1, Name: "A", Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{Models: []string{"public-a"}, InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(.000002)}}},
		{ID: 2, Name: "B", Platform: PlatformComposite, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-b"}}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenCodeGo, Models: []string{"public-b"}, InputPrice: testPtrFloat64(.000003), OutputPrice: testPtrFloat64(.000004)}}},
		{ID: 3, Name: "Empty", Platform: PlatformOpenAI},
	}
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(nil, repo)
	out, err := svc.ListAvailableCatalog(context.Background(), groups)
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls, "catalog reads one projection for the complete authorized group set")
	require.Equal(t, []int64{1, 2, 3}, repo.groupIDs)
	require.Len(t, out, 3)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "public-a", out[0].Models[0].Name)
	require.Equal(t, PlatformOpenAI, out[0].Models[0].Platform)
	require.Equal(t, "resolved", out[0].Models[0].PriceStatus)
	require.Zero(t, *out[0].Models[0].Pricing.InputPrice, "explicit free configured input remains free")
	require.InDelta(t, .000002, *out[0].Models[0].Pricing.OutputPrice, 1e-12)
	require.Len(t, out[1].Models, 1)
	require.Equal(t, "public-b", out[1].Models[0].Name)
	require.Equal(t, PlatformOpenCodeGo, out[1].Models[0].Platform)
	require.InDelta(t, .000003, *out[1].Models[0].Pricing.InputPrice, 1e-12)
	require.InDelta(t, .000004, *out[1].Models[0].Pricing.OutputPrice, 1e-12)
	require.Empty(t, out[2].Models)

	// A second request must observe configuration changes, not reuse a process
	// cache or a previous request's mappings.
	repo.byGroup = map[int64][]CommercialModelMapping{3: {{Platform: PlatformOpenAI, ModelMapping: map[string]any{"new-public": "new-private"}}}}
	out, err = svc.ListAvailableCatalog(context.Background(), groups[2:])
	require.NoError(t, err)
	require.Equal(t, 2, repo.calls)
	require.Equal(t, []int64{3}, repo.groupIDs)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "new-public", out[0].Models[0].Name)
}

func TestCommercialCatalogMappingReadFailureReturnsNoPartialCatalog(t *testing.T) {
	dependencyError := errors.New("mapping read failed")
	repo := &commercialAccountsStub{err: dependencyError}
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(nil, repo)
	empty, err := svc.ListAvailableCatalog(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Zero(t, repo.calls)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{{ID: 1, Platform: PlatformOpenAI}, {ID: 2, Platform: PlatformComposite}})
	require.Nil(t, out)
	require.ErrorIs(t, err, dependencyError)
	require.ErrorContains(t, err, "commercial account models")
	require.Equal(t, 1, repo.calls)
}

func TestCommercialQuoteTimeFactorUsesBillingPeriodAndPriceSource(t *testing.T) {
	channel := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceChannel, channelPricing: &ChannelModelPricing{TimePricing: &ChannelTimePricing{Timezone: "UTC", WeekdaysOnly: true, Periods: []ChannelTimePricingPeriod{{StartTime: "10:00:01", EndTime: "12:00:02", Multiplier: .5}}}}}
	require.Equal(t, .5, catalogPriceTimeMultiplier(channel, "model", time.Date(2026, 1, 5, 10, 0, 1, 0, time.UTC)))
	require.Equal(t, 1.0, catalogPriceTimeMultiplier(channel, "model", time.Date(2026, 1, 5, 12, 0, 2, 0, time.UTC)))
	require.Equal(t, 1.0, catalogPriceTimeMultiplier(channel, "model", time.Date(2026, 1, 10, 10, 0, 1, 0, time.UTC)))
	group := *channel
	group.Source = PricingSourceGroup
	require.Equal(t, 1.0, catalogPriceTimeMultiplier(&group, "model", time.Date(2026, 1, 5, 10, 0, 1, 0, time.UTC)))
	require.Equal(t, 2.0, catalogPriceTimeMultiplier(&ResolvedPricing{Source: PricingSourceLiteLLM}, "deepseek-v4-flash", time.Date(2026, 1, 5, 2, 0, 0, 0, time.UTC)))
	require.Equal(t, 1.0, catalogPriceTimeMultiplier(&ResolvedPricing{Source: PricingSourceLiteLLM}, "deepseek-v4-flash", time.Date(2026, 1, 5, 4, 0, 0, 0, time.UTC)))
}

func TestCommercialCatalogSuppressesUnsupportedEndpointAndAmbiguousAliases(t *testing.T) {
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 1, PublicModel: "not-an-embedding", MatchType: "exact", Endpoint: "embeddings", TargetPlatform: PlatformStepFun, Enabled: true}}},
		&commercialAccountsStub{byGroup: map[int64][]CommercialModelMapping{1: {
			{Platform: PlatformOpenAI, ModelMapping: map[string]any{"shared": "target"}},
			{Platform: PlatformOpenCodeGo, ModelMapping: map[string]any{"shared": "target"}},
		}}})
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{{ID: 1, Platform: PlatformComposite}})
	require.NoError(t, err)
	require.Empty(t, out[0].Models)
}
