## Why

The Release workflow already orders `Build and publish` after its complete
verification matrix, but the `release` Environment adds a second manual
approval after the maintainer has explicitly authorized publication with
`release-cli publish`. Automating that approval through a privileged CLI would
collapse the separation between publisher and approver while retaining the
complexity and credential exposure of a manual gate.

The repository needs one explicit irreversible authorization point. After an
exact annotated release tag is reviewed and published, GitHub should run the
verified build and publication automatically. Environment and tag governance
must compensate by failing closed before the tag push and by making published
custom tags immutable.

## What Changes

- Treat explicit `release-cli publish` as the publication authorization point;
  keep `publish`, `monitor`, `verify`, and `finalize` separate and resumable.
- Require the `release` Environment to have no reviewer, timer, or custom
  deployment gate, to disable administrator bypass, and to accept only
  `v*+custom.*` tags.
- Require an active repository Tag ruleset with no bypass actors that allows
  initial custom-tag creation but blocks updates and deletion.
- Make `release-cli publish` verify both external policies before pushing the
  tag, and treat a later waiting Environment job as policy drift.
- Keep `Build and publish` dependent on the complete release verification job,
  then let GitHub start it automatically.
- Make the group-usage rollup trigger integration fixtures independent of the
  CI wall clock and explicit about their database session timezone after the
  previous release finalization exposed a midnight-only false failure.

## Capabilities

### New Capabilities

- `automated-release-publication`: Defines explicit publication authorization,
  automated post-verification build and publication, and compensating
  Environment and Tag ruleset controls.

### Modified Capabilities

None.

## Impact

- **Release operations**: Maintainers no longer approve `Build and publish`
  after pushing a reviewed tag. They continue to run monitor, verify, and
  finalize explicitly.
- **Repository governance**: The GitHub `release` Environment and a new Tag
  ruleset become externally enforced prerequisites checked by release-cli.
- **Security**: The manual reviewer gate is replaced by a narrow explicit
  publish boundary, exact tag-only deployment policy, disabled admin bypass,
  immutable tags, least-privilege Actions permissions, and immutable assets.
- **Compatibility**: Existing tags, Releases, packages, application APIs, and
  persistent data are unchanged.
- **Test reliability**: Fixed timestamps replace a daily eight-hour timezone
  ambiguity in two rollup-trigger integration scenarios.
