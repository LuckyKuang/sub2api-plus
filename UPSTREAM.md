# Upstream Mapping

This file is the authoritative mapping of Plus tags to official baselines and
publication status. Release naming and procedures live in
[RELEASING.md](docs/RELEASING.md).

## Integrated Baseline

The current tree integrates official `v0.2.13`: tag object
`7d0c0067f406c380f0a94cfc3879cdae7049b467`, peeled commit
`3040209f205472038c1ba745a1bedd2edd9053b1`. The merge base is the previously
integrated official `v0.2.12` commit
`5106065716e494204fc0e8db16f68f6e9d576be0`. Importing source does not publish a
Plus release or change its embedded version.

Preserve intentional Plus behavior during every import. Current contracts live
in [outbound identity](docs/OUTBOUND_IDENTITY.md),
[ingress audit](docs/SECURITY_AUDIT_CONTENT_COVERAGE.md),
[provider/protocol documentation](docs/README.md),
[pricing](docs/CHANNEL_PRICING.md) and
[upgrade prerequisites](backend/migrations/README.md#upgrade-prerequisites).
Completed integration narratives remain in Git history.

## Current Version

```text
Git/GitHub: v0.2.13+custom.001
Application: 0.2.13+custom.001
GHCR: ghcr.io/luckykuang/sub2api-plus:v0.2.13-custom.001
```

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
| `v0.2.4+custom.002` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.003` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.004` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.005` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.006` | `v0.2.4` | `badfad8b7248b8aac0e6b503a06e392aa31cb294` | withdrawn |
| `v0.2.5+custom.001` | `v0.2.5` | `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea` | published |
| `v0.2.7+custom.001` | `v0.2.7` | `aea725f2ea644d5592d0bbb1d63b607efa7e200a` | published |
| `v0.2.8+custom.001` | `v0.2.8` | `fd80b08c90b55edcad5b00171b53f08721d30da1` | published |
| `v0.2.8+custom.002` | `v0.2.8` | `fd80b08c90b55edcad5b00171b53f08721d30da1` | published |
| `v0.2.9+custom.001` | `v0.2.9` | `4c00df2e0183e2c70b7fa8ba45914205e36aad0c` | published |
| `v0.2.10+custom.001` | `v0.2.10` | `2f3fed2fdb0787141294cec81487a5df30426f7f` | published |
| `v0.2.11+custom.001` | `v0.2.11` | `96f4c115c9749078f90cbf210a01d39baf3f53b6` | published |
| `v0.2.11+custom.002` | `v0.2.11` | `96f4c115c9749078f90cbf210a01d39baf3f53b6` | published |
| `v0.2.13+custom.001` | `v0.2.13` | `3040209f205472038c1ba745a1bedd2edd9053b1` | published |

`v0.2.4+custom.006` is marked withdrawn because official `v0.2.5` was imported before that snapshot overlay was published. Do not reuse or retag `.006`.

`v0.1.166+custom.007` is marked invalid because its tag contains embedded and
documented version `0.1.166+custom.006`. Remote Release and OCI artifact status
still require a maintainer audit. Do not reuse or retag `.007`.

## Repository Roles

- `origin`: [Sub2API Plus](https://github.com/LuckyKuang/sub2api-plus), the
  installation, update, rollback and release source.
- `upstream`: [official Sub2API](https://github.com/Wei-Shaw/sub2api), the source
  input for imports.

Update this mapping in the same change as an upstream integration. Keep tag,
embedded version and OCI naming synchronized through the release workflow;
published tags and artifacts are immutable.
