//go:build unit

package service

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// linearSnapshotScan is the pre-index reference implementation of step 3 in
// lookupIdentifiedModelPricingLocked; the index must return the same card.
func linearSnapshotScan(s *PricingService, bases map[string]struct{}) *LiteLLMModelPricing {
	keys := make([]string, 0, len(s.pricingData))
	for key := range s.pricingData {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if isUnregisteredGPT61TerraPricingModel(key) {
			continue
		}
		for _, base := range modelLookupCandidates(s.extractBaseName(strings.ToLower(key))) {
			if _, ok := bases[base]; ok {
				return s.pricingData[key]
			}
		}
	}
	return nil
}

func snapshotTestService(n int) *PricingService {
	data := map[string]*LiteLLMModelPricing{
		"claude-sonnet-4-20250514":   {InputCostPerToken: 1},
		"claude-sonnet-4-20250101":   {InputCostPerToken: 2},
		"gpt-5.4-2026-03-05":         {InputCostPerToken: 3},
		"vendor/some-model-20240101": {InputCostPerToken: 4},
	}
	for i := 0; i < n; i++ {
		data[fmt.Sprintf("filler-model-%d-20250%d01", i, i%9+1)] = &LiteLLMModelPricing{InputCostPerToken: float64(10 + i)}
	}
	return &PricingService{pricingData: data}
}

func TestPricingSnapshotIndexMatchesLinearScan(t *testing.T) {
	s := snapshotTestService(200)
	for _, model := range []string{
		"claude-sonnet-4", "claude-sonnet-4-20250601", "gpt-5.4", "some-model", "filler-model-7",
		"filler-model-7-20991231", "unknown-model", "claude-sonnet-4-5",
	} {
		t.Run(model, func(t *testing.T) {
			bases := map[string]struct{}{}
			for _, c := range s.buildModelLookupCandidates(model) {
				for _, b := range modelLookupCandidates(s.extractBaseName(c)) {
					bases[b] = struct{}{}
				}
			}
			require.Same(t, linearSnapshotScan(s, bases), s.pricingSnapshotIndexFor(s.pricingData).firstMatch(bases))
		})
	}
	// Sorted-first semantics: two dated Claude snapshots resolve to the earlier key.
	got := s.GetIdentifiedModelPricing("claude-sonnet-4")
	require.NotNil(t, got)
	require.Equal(t, 2.0, got.InputCostPerToken)
}

func TestPricingSnapshotIndexRebuildsOnCatalogReplacement(t *testing.T) {
	s := snapshotTestService(5)
	require.NotNil(t, s.GetIdentifiedModelPricing("gpt-5.4"))
	require.Nil(t, s.GetIdentifiedModelPricing("brand-new-model"))
	// Catalog reloads publish a new map; a stale index must not be reused.
	next := map[string]*LiteLLMModelPricing{"brand-new-model-20260101": {InputCostPerToken: 99}}
	s.pricingData = next
	got := s.GetIdentifiedModelPricing("brand-new-model")
	require.NotNil(t, got)
	require.Equal(t, 99.0, got.InputCostPerToken)
	require.Nil(t, s.GetIdentifiedModelPricing("gpt-5.4"))
}

// A catalog miss used to sort and re-derive spellings for every catalog key.
// With the index, steady-state misses cost a handful of allocations regardless
// of catalog size.
func TestPricingMissDoesNotScanCatalog(t *testing.T) {
	s := snapshotTestService(3000)
	s.GetIdentifiedModelPricing("not-in-catalog") // build index
	allocs := testing.AllocsPerRun(20, func() { s.GetIdentifiedModelPricing("not-in-catalog") })
	require.Less(t, allocs, 150.0, "catalog miss should not scale with catalog size")
}
