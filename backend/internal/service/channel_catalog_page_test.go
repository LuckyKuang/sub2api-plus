//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func pageIdentities(names ...string) map[string]catalogIdentity {
	out := make(map[string]catalogIdentity, len(names))
	for _, name := range names {
		key := catalogModelIdentity(&Group{ID: 1, Platform: PlatformOpenAI}, PlatformOpenAI, name)
		out[key] = catalogIdentity{key: key, name: name, platform: PlatformOpenAI, category: "group"}
	}
	return out
}

func TestCatalogPageSelectionSlicesNineCardsWithoutPricing(t *testing.T) {
	names := []string{"m01", "m02", "m03", "m04", "m05", "m06", "m07", "m08", "m09", "m10", "m11", "m12"}
	identities := pageIdentities(names...)
	meta := CatalogPageMeta{}

	first := catalogPageSelection(&CatalogPageQuery{Page: 1, PageSize: 9}, &meta, identities)
	require.Len(t, first, 9)
	require.Equal(t, 12, meta.Total)
	require.Equal(t, map[string]int{"group": 12}, meta.Categories)
	require.Equal(t, map[string]int{PlatformOpenAI: 12}, meta.Platforms)

	second := catalogPageSelection(&CatalogPageQuery{Page: 2, PageSize: 9}, &meta, identities)
	require.Len(t, second, 3)
	require.Equal(t, 12, meta.Total)

	key := catalogModelIdentity(&Group{ID: 1, Platform: PlatformOpenAI}, PlatformOpenAI, "m10")
	require.Contains(t, second, key)
	require.NotContains(t, first, key)
}

func TestCatalogPageSelectionFiltersAndFacets(t *testing.T) {
	identities := pageIdentities("keep-one", "keep-two", "drop")
	identities[catalogModelIdentity(&Group{ID: 2, Platform: PlatformAnthropic}, PlatformAnthropic, "keep-three")] =
		catalogIdentity{
			key:      catalogModelIdentity(&Group{ID: 2, Platform: PlatformAnthropic}, PlatformAnthropic, "keep-three"),
			name:     "keep-three",
			platform: PlatformAnthropic,
			category: "group",
		}
	meta := CatalogPageMeta{}

	selected := catalogPageSelection(&CatalogPageQuery{Search: "KEEP", Page: 1, PageSize: 9}, &meta, identities)
	require.Len(t, selected, 3)
	require.Equal(t, 3, meta.Total)
	// Facets follow the search text too: only the matching cards are counted.
	require.Equal(t, 3, meta.Platforms[PlatformOpenAI]+meta.Platforms[PlatformAnthropic])

	platformMeta := CatalogPageMeta{}
	catalogPageSelection(&CatalogPageQuery{Platform: PlatformAnthropic, Page: 1, PageSize: 9}, &platformMeta, identities)
	require.Equal(t, 1, platformMeta.Total)
	require.Equal(t, 4, platformMeta.Platforms[PlatformOpenAI]+platformMeta.Platforms[PlatformAnthropic])
	// The category facet applies the platform filter, so only the anthropic card counts.
	require.Equal(t, 1, platformMeta.Categories["group"])
}

func TestCatalogPageSelectionDetailIgnoresPaging(t *testing.T) {
	identities := pageIdentities("keep-one", "keep-two")
	compositeKey := catalogModelIdentity(&Group{ID: 2, Platform: PlatformComposite}, PlatformOpenAI, "keep-one")
	identities[compositeKey] = catalogIdentity{key: compositeKey, name: "keep-one", platform: PlatformOpenAI, category: "composite"}
	meta := CatalogPageMeta{}

	selected := catalogPageSelection(&CatalogPageQuery{Category: "composite", Platform: PlatformOpenAI, Model: "Keep-One", Page: 1, PageSize: 9}, &meta, identities)
	require.Len(t, selected, 1)
	require.Contains(t, selected, compositeKey)
	require.Equal(t, 1, meta.Total)
}

func TestCatalogPageSelectionWithoutQuerySelectsEverything(t *testing.T) {
	identities := pageIdentities("a", "b")
	require.Nil(t, catalogPageSelection(nil, &CatalogPageMeta{}, identities))
}
