package pricingmanifest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func rawManifest(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	return raw
}

func TestParse(t *testing.T) {
	want := Manifest{
		Version:    "v0.1.169+custom.001",
		PricingURL: "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.169%2Bcustom.001/model-pricing.json",
		SHA256:     "6ed8c77d4f106ca99a1db4dc8f8bb5cae0ee708a1a13c07f82efd435dc1d6395",
	}
	raw := rawManifest(t, want)

	got, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestParseRejectsMalformedAndInvalidFields(t *testing.T) {
	manifest := Manifest{
		Version:    "v0.1.169+custom.001",
		PricingURL: "https://example.com/releases/download/v0.1.169+custom.001/model-pricing.json",
		SHA256:     "6ed8c77d4f106ca99a1db4dc8f8bb5cae0ee708a1a13c07f82efd435dc1d6395",
	}
	raw := rawManifest(t, manifest)
	_, err := Parse(append(raw, []byte(`{"extra":true}`)...))
	require.ErrorContains(t, err, "exactly one JSON value")

	invalid := manifest
	invalid.SHA256 = "not-a-sha"
	_, err = Parse(rawManifest(t, invalid))
	require.ErrorContains(t, err, "SHA-256")

	unknown := append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
	_, err = Parse(unknown)
	require.ErrorContains(t, err, "unknown field")

	_, err = Parse([]byte(`{"version":`))
	require.ErrorContains(t, err, "decode pricing manifest")
}

func TestManifestValidateRequiresImmutableVersionedReleaseAsset(t *testing.T) {
	manifest := Manifest{
		Version: "v0.1.169+custom.001",
		SHA256:  "6ed8c77d4f106ca99a1db4dc8f8bb5cae0ee708a1a13c07f82efd435dc1d6395",
	}

	for name, pricingURL := range map[string]string{
		"mutable latest": "https://github.com/luckykuang/sub2api-plus/releases/latest/download/model-pricing.json",
		"wrong version":  "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.170+custom.001/model-pricing.json",
		"wrong asset":    "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.169+custom.001/other.json",
		"query":          "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.169+custom.001/model-pricing.json?mutable=true",
		"fragment":       "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.169+custom.001/model-pricing.json#mutable",
		"user info":      "https://user@github.com/luckykuang/sub2api-plus/releases/download/v0.1.169+custom.001/model-pricing.json",
	} {
		t.Run(name, func(t *testing.T) {
			manifest.PricingURL = pricingURL
			require.Error(t, manifest.Validate())
		})
	}

	manifest.PricingURL = "https://github.com/luckykuang/sub2api-plus/releases/download/v0.1.169%2Bcustom.001/model-pricing.json"
	require.NoError(t, manifest.Validate())
}

func TestCompareVersion(t *testing.T) {
	comparison, err := CompareVersion("v0.1.170+custom.001", "v0.1.169+custom.999")
	require.NoError(t, err)
	require.Equal(t, 1, comparison)

	comparison, err = CompareVersion("v0.1.169+custom.001", "v0.1.169+custom.001")
	require.NoError(t, err)
	require.Zero(t, comparison)

	comparison, err = CompareVersion("v0.1.169+custom.001", "v0.1.169+custom.002")
	require.NoError(t, err)
	require.Equal(t, -1, comparison)

	_, err = CompareVersion("pricing-20260802", "v0.1.169+custom.001")
	require.Error(t, err)

	_, err = CompareVersion("v0.1.169+custom.001 ", "v0.1.169+custom.001")
	require.Error(t, err)
}
