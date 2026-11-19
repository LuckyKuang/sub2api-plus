//go:build unit || !integration

package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexModelCatalogMatchesPinnedOfficialBytes(t *testing.T) {
	digest := sha256.Sum256(CodexModelCatalogBytes())
	require.Equal(t, CodexModelCatalogSHA256, hex.EncodeToString(digest[:]),
		"the embedded catalog must stay byte-identical to %s", CodexModelCatalogRelease)
}

func TestCodexModelCatalogCoversReviewedRelease(t *testing.T) {
	slugs := CodexModelCatalogSlugs()
	require.NotEmpty(t, slugs)
	// The reviewed release ships 14 descriptors; GPT-6.2 Sol/Astra arrival is the
	// change that made this catalog refresh necessary.
	require.Len(t, slugs, 14)
	for _, slug := range []string{
		"gpt-6.2-sol", "gpt-6.2-astra",
		"gpt-6.1-sol", "gpt-6.1-terra",
		"gpt-6-astra", "gpt-6-sol", "gpt-6-luna",
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5",
	} {
		raw, ok := CodexModelCatalogMetadata(slug)
		require.True(t, ok, slug)
		var meta struct {
			Slug   string `json:"slug"`
			Window int64  `json:"context_window"`
		}
		require.NoError(t, json.Unmarshal(raw, &meta))
		require.Equal(t, slug, meta.Slug)
		require.Positive(t, meta.Window, slug)
	}
}

func TestCodexModelCatalogResolvesAliasSpellings(t *testing.T) {
	// Real-world spellings only: configuration may carry a vendor prefix or odd
	// casing, but it never drops the separator the model ID is defined with.
	for _, model := range []string{"gpt-6.1-sol", "openai/GPT-6.1-Sol", "GPT-6.1-SOL", " gpt-6.1-sol "} {
		raw, ok := CodexModelCatalogMetadata(model)
		require.True(t, ok, model)
		var meta struct {
			Slug string `json:"slug"`
		}
		require.NoError(t, json.Unmarshal(raw, &meta))
		require.Equal(t, "gpt-6.1-sol", meta.Slug, model)
	}
}

func TestCodexModelCatalogRejectsUnknownModels(t *testing.T) {
	for _, model := range []string{"", "gpt-9.9-imaginary", "deepseek-v4", "claude-opus-5-5"} {
		_, ok := CodexModelCatalogMetadata(model)
		require.False(t, ok, model)
	}
}

func TestCodexModelCatalogCarriesOfficialInstructions(t *testing.T) {
	for _, slug := range []string{"gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-sol"} {
		raw, ok := CodexModelCatalogMetadata(slug)
		require.True(t, ok, slug)
		var meta struct {
			ModelMessages struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		}
		require.NoError(t, json.Unmarshal(raw, &meta))
		require.True(t, strings.Contains(meta.ModelMessages.InstructionsTemplate, "You are Codex"), slug)
	}
}
