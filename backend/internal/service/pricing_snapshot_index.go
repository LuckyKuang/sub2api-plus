package service

import (
	"reflect"
	"sort"
	"strings"
	"sync"
)

// pricingSnapshotIndex precomputes the "date snapshot / family base" scan that
// lookupIdentifiedModelPricingLocked runs for every model absent from the
// catalog by exact key. Without it each miss sorts the whole catalog and
// re-derives candidate spellings for every key (O(catalog) allocations), which
// made the catalog endpoint cost ~3ms per offer.
//
// The index maps each derived base spelling to the lexicographically first
// catalog key that produces it, so a lookup returns exactly what the linear
// scan would: the first sorted key owning any requested base.
type pricingSnapshotIndex struct {
	data       map[string]*LiteLLMModelPricing // retained so the identity below stays unique
	firstByKey map[string]int                  // base spelling -> position in sorted keys
	keys       []string
}

var pricingSnapshotIndexCache struct {
	sync.Mutex
	index *pricingSnapshotIndex
}

func mapIdentity(m map[string]*LiteLLMModelPricing) uintptr {
	return reflect.ValueOf(m).Pointer()
}

// pricingSnapshotIndexFor returns the (cached) index for an immutable catalog
// map. Catalog reloads publish a new map, which transparently rebuilds it.
func (s *PricingService) pricingSnapshotIndexFor(data map[string]*LiteLLMModelPricing) *pricingSnapshotIndex {
	id := mapIdentity(data)
	pricingSnapshotIndexCache.Lock()
	defer pricingSnapshotIndexCache.Unlock()
	if cur := pricingSnapshotIndexCache.index; cur != nil && mapIdentity(cur.data) == id && len(cur.data) == len(data) {
		return cur
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	idx := &pricingSnapshotIndex{data: data, keys: keys, firstByKey: make(map[string]int, len(keys)*2)}
	for i, key := range keys {
		if isUnregisteredGPT61TerraPricingModel(key) {
			continue
		}
		for _, base := range modelLookupCandidates(s.extractBaseName(strings.ToLower(key))) {
			if _, ok := idx.firstByKey[base]; !ok {
				idx.firstByKey[base] = i
			}
		}
	}
	pricingSnapshotIndexCache.index = idx
	return idx
}

// firstKeyForBases returns the pricing of the first sorted key whose derived
// bases intersect the requested set, or nil.
func (idx *pricingSnapshotIndex) firstMatch(bases map[string]struct{}) *LiteLLMModelPricing {
	best := -1
	for base := range bases {
		if i, ok := idx.firstByKey[base]; ok && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return idx.data[idx.keys[best]]
}
