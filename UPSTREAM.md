# Upstream Mapping

| Custom Release | Official Sub2API Release | Official Commit |
| --- | --- | --- |
| `v0.1.164+custom.001` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.003` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.004` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.005` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.165+custom.001` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165+custom.002` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165-custom.003` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165-custom.004` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.166-custom.001` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166-custom.002` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166-custom.003` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |

The current application version uses a valid SemVer prerelease suffix to
identify the custom build. The matching Docker image tag is
`ghcr.io/luckykuang/sub2api-plus:0.1.166-custom.003`.

## Distribution Source

All operator-facing installation, update, rollback, and release links use the
fork repository: `https://github.com/luckykuang/sub2api-plus`.

- GitHub Release tags use `vX.Y.Z-custom.NNN`.
- Release archives use the matching suffix, for example
  `sub2api_0.1.166-custom.003_linux_amd64.tar.gz`.
- GHCR image tags use the same suffix, for example
  `ghcr.io/luckykuang/sub2api-plus:0.1.166-custom.003`.
- The official repository is an upstream source for maintainers only; it is
  never used by installation or automatic update paths.

## Repository Topology

- `main`: custom development branch
- `origin`: `https://github.com/luckykuang/sub2api-plus.git`
- `upstream`: `https://github.com/Wei-Shaw/sub2api.git`
- `upstream/v0.1.164`: imported official baseline tag
- `custom/v0.1.164-r001`: custom release tag, created after the release commit

## Upgrade Workflow

1. Fetch the target official release from `upstream`.
2. Merge the official release into `main`.
3. Resolve only intentional custom conflicts.
4. Increment the custom iteration for the same official baseline, or reset it
   to `custom.001` after an upstream version increase.
5. Update this mapping and version metadata.
6. Run backend and frontend checks, then publish the matching GitHub Release
   tag, for example `v0.1.166-custom.003`.
