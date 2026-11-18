package openai

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// codexModelCatalogJSON is the official model catalog bundled with the reviewed
// Codex release, byte for byte. It is the single source of truth for every
// descriptor the gateway serves to an official client: context windows, reasoning
// levels, service tiers, priorities and model messages all come from here, and
// Plus-specific policy is layered on top instead of being written back into it.
//
// Source: openai/codex rust-v0.170.0 codex-rs/models-manager/models.json
// (last catalog change 176cbbe92c, "Add GPT-6.2 Sol and Astra to model catalogs").
//
//go:embed codex_models_catalog.json
var codexModelCatalogJSON []byte

const (
	// CodexModelCatalogRelease is the reviewed Codex release that produced the
	// embedded catalog. Every identity/CLI upgrade must refresh it or document why
	// the catalog is unchanged.
	CodexModelCatalogRelease = "rust-v0.170.0"
	// CodexModelCatalogSHA256 pins the embedded bytes so an accidental edit of the
	// official file fails the catalog regression test.
	CodexModelCatalogSHA256 = "d50b6749506d97e5f334dbabcb666eb1689f423cf3e88cfabdcf3c536b873068"
)

var (
	codexModelCatalogOnce sync.Once
	codexModelCatalog     map[string]json.RawMessage
	codexModelCatalogErr  error
)

func loadCodexModelCatalog() (map[string]json.RawMessage, error) {
	codexModelCatalogOnce.Do(func() {
		var payload struct {
			Models []json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(codexModelCatalogJSON, &payload); err != nil {
			codexModelCatalogErr = err
			return
		}
		index := make(map[string]json.RawMessage, len(payload.Models))
		for _, raw := range payload.Models {
			var meta struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(raw, &meta); err != nil {
				codexModelCatalogErr = err
				return
			}
			slug := strings.TrimSpace(meta.Slug)
			if slug == "" {
				continue
			}
			index[slug] = raw
		}
		codexModelCatalog = index
	})
	return codexModelCatalog, codexModelCatalogErr
}

// CodexModelCatalogMetadata returns the exact official descriptor for a model
// slug. Callers pass the upstream model ID; aliases and vendor prefixes are
// canonicalized first so `openai/gpt-6.1-sol` and `gpt6.1sol` resolve to the same
// descriptor. A model outside the official catalog returns false and keeps the
// synthesized descriptor.
func CodexModelCatalogMetadata(model string) ([]byte, bool) {
	catalog, err := loadCodexModelCatalog()
	if err != nil || len(catalog) == 0 {
		return nil, false
	}
	slug := CanonicalizeOpenAIModelAliasSpelling(model)
	if slug == "" {
		slug = strings.ToLower(strings.TrimSpace(model))
	}
	raw, ok := catalog[slug]
	if !ok || len(raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

// CodexModelCatalogSlugs lists the embedded catalog in file order. Tests use it
// to prove every official model resolves and to detect silent catalog drift.
func CodexModelCatalogSlugs() []string {
	var payload struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(codexModelCatalogJSON, &payload); err != nil {
		return nil
	}
	slugs := make([]string, 0, len(payload.Models))
	for _, model := range payload.Models {
		if slug := strings.TrimSpace(model.Slug); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

// CodexModelCatalogBytes exposes the embedded catalog for the digest regression
// test only.
func CodexModelCatalogBytes() []byte {
	return codexModelCatalogJSON
}
