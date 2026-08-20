Sub2API Plus v0.1.177+custom.003

## Highlights

- Add an administrator support view for inspecting a selected user's safe
  usage, subscription, API key, payment, and asynchronous-image information
  without changing authentication state or exposing mutation operations.
- Default OpenAI OAuth account Codex fingerprints to the stable per-device
  mode across account creation, editing, bulk updates, and persisted data.
- Keep OpenAI prompt-cache identity and multi-turn account routing stable
  across Responses, Chat Completions, HTTP, and WebSocket paths.

## Changed

- Backfill every eligible top-level OpenAI OAuth account with an explicit
  Codex fingerprint mode and enforce valid persisted modes on later writes.
- Mark administrator support responses as non-cacheable and return only safe
  API key summaries without plaintext credentials.
- Align every build image with the declared Go 1.26.6 toolchain, increase the
  frontend build heap limit, and update the Go archive dependency.

## Fixed

- Revalidate durable account eligibility and billing before WebSocket
  continuation turns while preserving one stable session route whenever the
  current account remains eligible.
- Derive prompt-cache identity from stable request and account properties
  instead of allocation-dependent tool-schema state.
- Repair IP login-failure threshold transactions, permanent manual blocks,
  concurrent updates, setting bounds, and the corresponding administrator UI.
- Avoid stale administrator support responses after rapid target changes and
  keep target-user reads side-effect free.
- Stabilize static-analysis, tool-schema allocation, and Windows WSL path
  validation coverage used by the release gate.

## Compatibility and migration

- Migration 224 backfills missing, empty, or invalid fingerprint modes to
  `device` for top-level OpenAI OAuth accounts. Existing explicit `off`,
  `device`, `session`, and `full` values remain unchanged.
- The migration removes the fingerprint-mode field from ineligible account
  types and installs a database trigger that rejects invalid future values.
- No new configuration field or manual operator action is required.

## Known issues

None.

## Upstream baseline

Official release: v0.1.177
Official commit: 073e92d17178a1ccdb0a27017f572f10c9c7ab62
