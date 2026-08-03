## Why

Sub2API Plus is distributed, installed, updated, and released from
`github.com/LuckyKuang/sub2api-plus`, but its backend Go module and internal
imports still identify the official upstream repository. This creates a
mismatch between the source identity of the fork and its actual repository.

## What Changes

- Change the backend Go module path from `github.com/Wei-Shaw/sub2api` to
  `github.com/LuckyKuang/sub2api-plus`.
- Update backend Go imports and static-analysis package references to the Plus
  module path.
- Regenerate Ent and Wire output so generated imports use the new module path.
- Preserve the official upstream URL in `UPSTREAM.md` and retain existing
  repository-root links used for releases, installation, updates, images, and
  documentation.

## Non-goals

- Moving `backend/go.mod` to the repository root or adding a `/backend` module
  suffix.
- Changing runtime APIs, database schemas, migrations, configuration keys, or
  release versioning.
- Replacing references that intentionally describe the official upstream.

## Impact

- Affected capability: `go-module-identity`.
- Affected code: backend module declaration, Go source imports, Ent and Wire
  generated output, and Go lint configuration.
- Compatibility: source-level module identity changes to match the Plus fork;
  application runtime behavior is unchanged.
