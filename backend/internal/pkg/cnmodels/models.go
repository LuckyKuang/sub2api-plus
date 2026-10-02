// Package cnmodels owns the provider model catalog shared with the frontend.
// Official sources and account restrictions are documented in docs/CN_PROVIDER_MODELS.md.
package cnmodels

import (
	_ "embed"
	"encoding/json"
	"slices"
)

//go:embed models.json
var catalogJSON []byte

var catalog = func() map[string][]string {
	var models map[string][]string
	if err := json.Unmarshal(catalogJSON, &models); err != nil {
		panic("invalid embedded CN provider model catalog: " + err.Error())
	}
	return models
}()

// DefaultModelIDs returns an independent copy of a provider's curated IDs.
// Unknown platforms have no defaults and never fall back to another provider.
func DefaultModelIDs(platform string) []string {
	return slices.Clone(catalog[platform])
}
