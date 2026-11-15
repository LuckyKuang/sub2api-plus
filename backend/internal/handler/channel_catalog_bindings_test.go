//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type catalogChannelRepository struct {
	service.ChannelRepository
	channels []service.Channel
}

func (r *catalogChannelRepository) ListAll(context.Context) ([]service.Channel, error) {
	return r.channels, nil
}

type catalogMappingRepository struct {
	groupIDs []int64
	byGroup  map[int64][]service.CommercialModelMapping
}

func (r *catalogMappingRepository) ListCommercialModelMappings(_ context.Context, ids []int64) (map[int64][]service.CommercialModelMapping, error) {
	r.groupIDs = append([]int64(nil), ids...)
	return r.byGroup, nil
}

type catalogRouteRepository struct {
	service.CompositeModelRouteRepository
}

func (*catalogRouteRepository) ListByGroup(_ context.Context, id int64, _ bool) ([]service.CompositeModelRoute, error) {
	return []service.CompositeModelRoute{{
		ID: id, GroupID: id, PublicModel: "route-only", MatchType: service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformAnthropic, Enabled: true,
	}}, nil
}

func TestAvailableChannelCatalogExcludesUnboundModels(t *testing.T) {
	// User requirement: Available Channels mirrors active channel price cards.
	// Account mappings, group prices and Composite routes of unbound groups must
	// not publish extra models. Key binding quotes keep their separate contract.
	groups := []service.Group{
		{ID: 1, Name: "A", Platform: service.PlatformOpenAI},
		{ID: 2, Name: "B", Platform: service.PlatformAnthropic},
		{ID: 3, Name: "C", Platform: service.PlatformComposite},
		{ID: 4, Name: "D", Platform: service.PlatformOpenAI,
			ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"group-only"}}}},
		{ID: 5, Name: "E", Platform: service.PlatformAnthropic},
		{ID: 6, Name: "F", Platform: service.PlatformOpenAI},
		{ID: 7, Name: "G", Platform: service.PlatformComposite},
	}
	channels := &catalogChannelRepository{channels: []service.Channel{
		{ID: 11, Name: "OpenAI", Status: service.StatusActive, GroupIDs: []int64{1}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformOpenAI, Models: []string{"gpt-configured"}},
		}},
		{ID: 12, Name: "Anthropic", Status: service.StatusActive, GroupIDs: []int64{2}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformAnthropic, Models: []string{"claude-configured"}},
		}},
		{ID: 15, Name: "Disabled", Status: service.StatusDisabled, GroupIDs: []int64{5}},
		{ID: 16, Name: "Empty", Status: service.StatusActive, GroupIDs: []int64{6}},
		{ID: 17, Name: "Composite", Status: service.StatusActive, GroupIDs: []int64{7}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformAnthropic, Models: []string{"claude-configured"}},
		}},
		{ID: 99, Name: "Unauthorized", Status: service.StatusActive, GroupIDs: []int64{99}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformOpenAI, Models: []string{"hidden"}},
		}},
	}}
	mappings := &catalogMappingRepository{byGroup: map[int64][]service.CommercialModelMapping{
		1: {{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"stale-openai": "private"}}},
		2: {{Platform: service.PlatformAnthropic, ModelMapping: map[string]any{"stale-anthropic": "private"}}},
		3: {{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"unbound-openai": "private"}},
			{Platform: service.PlatformAnthropic, ModelMapping: map[string]any{"unbound-anthropic": "private"}}},
		5: {{Platform: service.PlatformAnthropic, ModelMapping: map[string]any{"disabled-channel-model": "private"}}},
	}}
	plaza := service.NewModelPlazaService(channels, nil, nil, nil, nil)
	plaza.SetCommercialCatalogSources(&catalogRouteRepository{}, mappings)
	access := &catalogAccessStub{groups: groups}
	settings := service.NewSettingService(&settingHandlerPublicRepoStub{values: map[string]string{
		service.SettingKeyAvailableChannelsEnabled: "true",
	}}, &config.Config{})
	h := &AvailableChannelHandler{apiKeyService: access, plazaService: plaza, settingService: settings}
	read := func(view string) channelCatalog {
		t.Helper()
		c, w := catalogContext("?view="+view, true)
		h.List(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		var body struct {
			Data channelCatalog `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Data
	}
	assertChannelModels := func(want map[int64][]string) {
		t.Helper()
		catalog := read("catalog")
		got := make(map[int64][]string)
		for _, group := range catalog.Groups {
			got[group.ID] = []string{}
			for _, model := range group.Models {
				require.NotEmpty(t, model.Source.Name)
				got[group.ID] = append(got[group.ID], model.Name)
			}
		}
		require.Equal(t, want, got)
	}
	// Sales catalog membership is the intersection of configured price cards and
	// the models the group's accounts actually serve: a channel card alone cannot
	// advertise a model no account exposes.
	assertChannelModels(map[int64][]string{
		1: {}, 2: {}, 6: {}, 7: {"claude-configured"},
	})
	require.ElementsMatch(t, []int64{1, 2, 6, 7}, mappings.groupIDs)

	// Refresh observes channel deletion, disablement, unbinding and price-card
	// deletion immediately; configuration from other sources cannot restore them.
	channels.channels[0].ModelPricing = nil
	assertChannelModels(map[int64][]string{
		1: {}, 2: {}, 6: {}, 7: {"claude-configured"},
	})
	channels.channels[0].GroupIDs = nil
	channels.channels[1].Status = service.StatusDisabled
	channels.channels = channels.channels[:len(channels.channels)-2]
	assertChannelModels(map[int64][]string{6: {}})

	quotes := read("key_quotes")
	require.Len(t, quotes.Groups, len(groups))
	got := make(map[int64][]string)
	for _, group := range quotes.Groups {
		for _, model := range group.Models {
			got[group.ID] = append(got[group.ID], model.Name)
		}
	}
	require.ElementsMatch(t, []string{"route-only", "unbound-anthropic", "unbound-openai"}, got[3])
	require.Equal(t, []string{"group-only"}, got[4])
	require.Equal(t, []string{"disabled-channel-model"}, got[5])

	channels.channels = nil
	mappings.groupIDs = nil
	assertChannelModels(map[int64][]string{})
	require.Nil(t, mappings.groupIDs, "unbound groups must not trigger account configuration reads")
}
