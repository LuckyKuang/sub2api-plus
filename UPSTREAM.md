# Upstream Mapping

| Custom Release | Official Sub2API Release | Official Commit |
| --- | --- | --- |
| `v0.1.164+custom.001` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.003` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.004` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.164+custom.005` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |
| `v0.1.165+custom.001` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165+custom.002` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165+custom.003` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.165+custom.004` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| `v0.1.166+custom.001` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.002` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.003` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.004` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.005` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.006` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |
| `v0.1.166+custom.008` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` |

The current application version uses SemVer build metadata to identify the
custom build: `v0.1.166+custom.008`. The matching OCI image tag is
`ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.008`.

## Release Versioning

All new custom releases use one canonical Git and GitHub Release tag:
`vX.Y.Z+custom.NNN`. The application embeds the same version without the
leading `v`. OCI registries do not permit `+` in image tags, so the release
workflow preserves the leading `v` and replaces only `+` with `-`.

| Surface | Canonical format | Current example |
| --- | --- | --- |
| Git tag and GitHub Release | `vX.Y.Z+custom.NNN` | `v0.1.166+custom.008` |
| Embedded application version | `X.Y.Z+custom.NNN` | `0.1.166+custom.008` |
| GHCR image tag | `vX.Y.Z-custom.NNN` | `v0.1.166-custom.008` |

For another release on the same official baseline, increment `NNN` by one.
When the official baseline changes, reset the custom iteration to `001`.
Examples: `v0.1.166+custom.008`, then `v0.1.167+custom.001` after merging
official `v0.1.167`. `NNN` is always a three-digit number from `001` to `999`.

Historical releases were migrated to this canonical form on 2026-07-29. The
legacy Git tags, GitHub Releases, and OCI image tags that used
`-custom.NNN` were removed after their canonical replacements were verified.

## Historical Naming Migration Audit

| Removed legacy tag | Canonical replacement | Result |
| --- | --- | --- |
| `v0.1.165-custom.003` | `v0.1.165+custom.003` | Release rebuilt; legacy GHCR tag removed |
| `v0.1.165-custom.004` | `v0.1.165+custom.004` | Release rebuilt; legacy GHCR tag removed |
| `v0.1.166-custom.001` | `v0.1.166+custom.001` | Release rebuilt; legacy GHCR tag removed |
| `v0.1.166-custom.002` | `v0.1.166+custom.002` | Release rebuilt; legacy GHCR tag removed |
| `v0.1.166-custom.003` | `v0.1.166+custom.003` | Release rebuilt; legacy GHCR tag removed |

The previous release artifacts and Git objects were retained in a local
audit backup at `/private/tmp/sub2api-release-migration-20260729/` before
the legacy remote references were deleted. This backup is not a distribution
source and must not be used for installation or upgrades.

## Distribution Source

All operator-facing installation, update, rollback, and release links use the
fork repository: `https://github.com/luckykuang/sub2api-plus`.

- GitHub Release tags use `vX.Y.Z+custom.NNN`.
- Release archives use the matching suffix, for example
  `sub2api_0.1.166+custom.008_linux_amd64.tar.gz`.
- GHCR image tags use the OCI-safe derived suffix, for example
  `ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.008`.
- The official repository is an upstream source for maintainers only; it is
  never used by installation or automatic update paths.

## Repository Topology

- `main`: custom development branch
- `origin`: `https://github.com/luckykuang/sub2api-plus.git`
- `upstream`: `https://github.com/Wei-Shaw/sub2api.git`
- `upstream/v0.1.166`: imported official baseline tag
- `v0.1.166+custom.008`: custom release tag, created after the release commit

## Upgrade Workflow

1. Fetch the target official release from `upstream`.
2. Merge the official release into `main`.
3. Resolve only intentional custom conflicts.
4. Increment the custom iteration for the same official baseline, or reset it
   to `custom.001` after an upstream version increase.
5. Update this mapping and version metadata.
6. Run backend and frontend checks, then publish the matching GitHub Release
   tag, for example `v0.1.166+custom.008`.

## Custom Release Checklist

For every custom release, update the version before creating a Git tag. A tag
must always point to the release commit; never create a tag first and attempt
to retrofit the version afterward.

1. Select the next `X.Y.Z+custom.NNN` version. Increment `NNN` on the same
   upstream baseline, or reset it to `001` after an upstream version change.
2. Update the runtime and local-build version sources together:
   - `backend/cmd/server/VERSION` to `X.Y.Z+custom.NNN`.
   - every `ARG VERSION=` in `Dockerfile` and `backend/Dockerfile` to the same
     value.
3. Update the current-release examples in `README.md`, `README_CN.md`,
   `README_JA.md`, `deploy/README.md`, `deploy/DOCKER.md`,
   `deploy/APPLE_CONTAINER.md`, and `deploy/.env.example`; retain historical
   version rows in this mapping document.
4. Run the version audit before committing. Set `RELEASE_VERSION` to the
   selected version without its leading `v`:

   ```bash
   RELEASE_VERSION=0.1.166+custom.008
   git grep -n "$RELEASE_VERSION" -- backend/cmd/server/VERSION Dockerfile backend/Dockerfile
   git grep -n "v${RELEASE_VERSION/+/-}" -- README.md README_CN.md README_JA.md deploy
   git grep -n '^ARG VERSION=' -- Dockerfile backend/Dockerfile
   ```

   The current runtime and documentation references must agree with the
   planned release; older values are permitted only in historical mappings.
5. Run the required checks, commit and push `main`, then create and push the
   annotated Git tag `vX.Y.Z+custom.NNN` on that commit.

The release workflow independently verifies the embedded version and Docker
build defaults against the tag before it builds or publishes any artifacts.
