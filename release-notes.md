Sub2API Plus v0.2.1+custom.002

## Highlights

Hardens client-disconnect risk control by tracking each logical session
independently, and accepts current official Codex search client profiles.

## Changed

- Scopes disconnect counters, blocking decisions, and administrative event
  views to the resolved session so unrelated sessions no longer affect one
  another.
- Preserves session identity throughout disconnect lifecycle cleanup and adds
  migration-backed storage for the new scope.
- Recognizes the official Codex search client profile while preserving the
  credential-owned identity precedence rules.
- Updates `docker/setup-buildx-action` to 4.3.0 and `google.golang.org/grpc` to
  1.83.1.

## Compatibility and migration

Database migration 256 runs automatically, preserves existing disconnect
events under the `legacy` scope, advances the processing generation, and
rebuilds risk state per session. The migration disables consecutive-disconnect
banning so administrators can review the new scope before re-enabling it.

## Known issues

No release-specific known issues.

## Upstream baseline

Official release: v0.2.1
Official commit: 578785ee7fb35030b094b69624efe25670a36f5f
