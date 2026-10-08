# Model Pricing Data

This directory contains the Sub2API Plus bundled model-pricing fallback copy.

## Source
Remote refresh discovers the latest GitHub Release through a manifest:

- Manifest: `https://github.com/luckykuang/sub2api-plus/releases/latest/download/model-pricing-manifest.json`
- Immutable release asset: declared by the manifest
- Upstream data source for maintainers: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

The sole-maintainer GitHub Release publication boundary is the source of trust.
The application accepts Release pricing only after validating the manifest
version, immutable asset URL, dedicated HTTPS host policy, response limits,
manifest-bound SHA-256 digest, and pricing JSON. No deployment key is required.

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process

The pricing service will:

1. Load a validated local cache when one exists
2. Otherwise make the bundled repository pricing immediately available
3. Automatically check the latest Release manifest before downloading data
4. Keep the current cache when a manifest, hash, URL policy, JSON, or version check fails

Remote responses are bounded before parsing:

- Manifest: 64 KiB
- Pricing data: 32 MiB

## Runtime Cache

`model_pricing.verified-cache.json` is the authoritative runtime cache. It
retains its historical filename for upgrade compatibility. It contains the
exact manifest and pricing bytes in one atomically replaced bundle. The
application validates the immutable pricing URL, digest, and JSON data again
on every startup before using it.

The following files are compatibility mirrors for operators and upgrades from
older versions:

- `model_pricing.json`
- `model_pricing.manifest.json`

A new runtime writes the authoritative bundle first, then refreshes these
mirrors. A mirror write failure does not invalidate an already committed
bundle. When no bundle exists, a valid legacy data-and-manifest cache is
validated and migrated automatically. Schema-v1 bundles containing the former
signature field remain readable and are rewritten as schema v2; obsolete
standalone `.sig` files are ignored.

## Release Update

Pricing data changes must be reviewed and published as immutable Release
assets. Do not point runtime configuration at a mutable branch. The release
workflow publishes `model-pricing.json` and `model-pricing-manifest.json` after
GoReleaser completes.

To refresh this bundled fallback before a release, update it from the upstream
source, review the diff, and let the release workflow calculate its digest:

```bash
curl -fsS https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

## Admin Reference Prices

The admin channel editor resolves reference prices from this catalog, but with a
stricter match rule than request billing:

- A catalog row must belong to the requested platform's provider label before it
  can be used. An `anthropic` row never prices an `openai` channel.
- Models without an exact catalog entry, a registered same-SKU alias, or a
  same-model built-in fallback are reported as `manual_required`. A missing
  price is never rendered as zero.
- Remote mirrors can lag a model release. `gpt-6-sol`, `gpt-6-luna` and
  `claude-opus-5-5` carry same-model built-in fallbacks in
  `internal/service/` so an absent or stale catalog cannot silently bill them as
  a different model. If the JSON is only being byte-identical to upstream, those
  fallbacks stay as the sole net-new protection — see `docs/CHANNEL_PRICING.md`.

Any change to a bundled JSON must keep its `.sha256` in sync and let the release
workflow recompute the digest.

The StepFun chat entries are maintained from its official international USD
price table, checked 2026-10-08. Model-specific rates, cache accounting, the
unpriced Step Plan router, and regional/subscription distinctions are documented
in [StepFun](../../../docs/providers/STEPFUN.md#default-prices). The same models
have exact billing fallbacks for an unavailable catalog; no unknown StepFun
model inherits a family price.
