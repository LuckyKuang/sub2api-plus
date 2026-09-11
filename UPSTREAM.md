# Upstream Mapping

This file maps Sub2API Plus releases to their official Sub2API baseline. Release
procedures are documented in [`docs/RELEASING.md`](docs/RELEASING.md).

## Integrated Baseline

The current integration tree incorporates official `v0.2.4`, commit
`5de5e2bed035d43591a2e10e51f420ef6a84eb98`, on top of Plus
`6344b5db3d5bc819acb222a66c6884bccdb37369`. The release mapping below is the
authoritative record of this baseline's publication status.
The official tag's source VERSION contains `0.2.3`; the tag commit is the
integration reference. Plus version/tag/image promotion remains a separate step.

Plus retains credential-owner identity precedence, ingress content audit,
session and quota accounting, asynchronous images, administrator export
controls, IP access controls, and distribution/toolchain choices. Retired
upstream billing probes remain removed. Grok cross-client rewriting stays
opt-in, and inconclusive OAuth billing does not grant media eligibility.

See [v0.2.4 integration and upgrade behavior](docs/UPSTREAM_V0_2_4_INTEGRATION.md)
for public API changes, migrations, defaults, and validation boundaries.

## Release Mapping

| Custom Release | Official Release | Official Commit | Status |
| --- | --- | --- | --- |
| `v0.1.164+custom.001` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.003` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.004` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.005` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.165+custom.001` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.002` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.003` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.004` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.166+custom.001` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.002` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.003` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.004` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.005` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.006` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.007` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | invalid |
| `v0.1.166+custom.008` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.009` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.010` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.168+custom.001` | `v0.1.168` | `99c8e4bf7564823bafbab369acab6539e734c1bb` | published |
| `v0.1.169+custom.001` | `v0.1.169` | `26d894ef4f50645a4bf1030e378ac892f17d0223` | published |
| `v0.1.169+custom.002` | `v0.1.169` | `26d894ef4f50645a4bf1030e378ac892f17d0223` | published |
| `v0.1.170+custom.001` | `v0.1.170` | `c043c24774228ba891ddf90d783aa6dc7d0855b5` | published |
| `v0.1.170+custom.002` | `v0.1.170` | `c043c24774228ba891ddf90d783aa6dc7d0855b5` | published |
| `v0.1.171+custom.001` | `v0.1.171` | `f0e7a9c7a23a7d02fb159b62fa809621eb0475a6` | published |
| `v0.1.172+custom.001` | `v0.1.172` | `155c494964c3ea6ecc31f52679525c1034bf0f16` | published |
| `v0.1.173+custom.002` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.173+custom.003` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.173+custom.004` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.176+custom.001` | `v0.1.176` | `e803e3851c0a7e222cfadeafad7b8636ab959d11` | published |
| `v0.1.176+custom.002` | `v0.1.176` | `e803e3851c0a7e222cfadeafad7b8636ab959d11` | published |
| `v0.1.177+custom.001` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.177+custom.002` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.177+custom.003` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.178+custom.001` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.002` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.003` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.004` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.005` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.183+custom.001` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.002` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.003` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.004` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.2.0+custom.001` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.0+custom.002` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.0+custom.003` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.1+custom.001` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.1+custom.002` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.1+custom.003` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.4+custom.001` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.002` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | planned |

`v0.1.166+custom.007` is marked invalid because its tag contains embedded and
documented version `0.1.166+custom.006`. Remote Release and OCI artifact status
still require a maintainer audit. Do not reuse or retag `.007`.

## Current Version

```text
Git/GitHub: v0.2.4+custom.002
Application: 0.2.4+custom.002
GHCR: ghcr.io/luckykuang/sub2api-plus:v0.2.4-custom.002
```

## Naming

- Git tags and GitHub Releases: `vX.Y.Z+custom.NNN`
- Embedded application versions: `X.Y.Z+custom.NNN`
- OCI tags: `vX.Y.Z-custom.NNN`
- `NNN` is a three-digit iteration from `001` to `999`.

Increment the iteration on the same official baseline and reset it to `001`
after importing a newer official release.

## Distribution and Repository Roles

- `origin` is the custom repository:
  `https://github.com/LuckyKuang/sub2api-plus.git`.
- `upstream` is the official source:
  `https://github.com/Wei-Shaw/sub2api.git`.
- Installation, update, rollback, and release links use the custom repository.
- The official repository is an input for maintainers, not a distribution
  source for Sub2API Plus.

Local clones may need to add the `upstream` remote before an upstream sync.
Preserve intentional Plus changes during merges and update this mapping in the
same release-preparation change.

Historical `-custom.NNN` Git naming was migrated to the canonical
`+custom.NNN` form. OCI tags continue to use `-custom.NNN` because OCI tags do
not support `+`.
