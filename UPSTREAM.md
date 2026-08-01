# Upstream Mapping

This file maps Sub2API Plus releases to their official Sub2API baseline. Release
procedures are documented in [`docs/RELEASING.md`](docs/RELEASING.md).

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

`v0.1.166+custom.007` is marked invalid because its tag contains embedded and
documented version `0.1.166+custom.006`. Remote Release and OCI artifact status
still require a maintainer audit. Do not reuse or retag `.007`.

## Current Version

```text
Git/GitHub: v0.1.168+custom.001
Application: 0.1.168+custom.001
GHCR: ghcr.io/luckykuang/sub2api-plus:v0.1.168-custom.001
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
