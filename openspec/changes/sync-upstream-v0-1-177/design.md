## Baseline

| Item | Value |
| --- | --- |
| Plus HEAD | `b01014ca7d0c0a83bce9c22e5c97c351431d6579` (`v0.1.176+custom.002`) |
| Current official baseline | `v0.1.176` / `e803e3851c0a7e222cfadeafad7b8636ab959d11` |
| Merge input | `v0.1.177` / `073e92d17178a1ccdb0a27017f572f10c9c7ab62` |
| Prepared Plus version | `v0.1.177+custom.001` |

Only the annotated `v0.1.177` tag is merged. `upstream/main` and later commits
are outside this change.

## Ownership

| Area | Owner | Merge rule |
| --- | --- | --- |
| Module path, workflows, release metadata, `UPSTREAM.md` | Plus | Retain Plus repository and dynamic toolchain checks; prepare custom.001 as unpublished. |
| Migrations 222/223 and rollup lifecycle | Official | Import as one unit: startup/schedule sync, query, recompute/cleanup/partition invalidation, timezone rebuild, API, and UI. |
| Rollup timezone | Official | Use server application timezone with `TZ > TIMEZONE > config/default`; never accept browser timezone for these buckets. |
| Native remote compaction v2 | Official plus Plus guards | Keep `/responses`, require stream plus compaction trigger, preserve capability and profit admission. |
| Legacy `/responses/compact` | Existing composition | Keep unary semantics, compact model mapping, and compact fingerprint exclusion. |
| OAuth session policy | Plus | Authorize and resolve namespace before fingerprint or turn-state mutation. |
| Codex identity triple | Plus, immutable | Valid credential-owner UA > valid global UA > compiled default. Keep selected client family, Originator, OS, architecture, and terminal fingerprint. |
| Fingerprint mode default | Plus | Missing, empty, invalid, and `session` use session convergence; explicit `off`, `device`, and `full` remain addressable. |
| Turn-state protocol | Official behavior, Plus owner normalization | Relay only for the credential owner and record provenance only when committed to the client. |
| Quota, TPS, output timing, usage completeness | Plus | Preserve local response headers and accounting/observability behavior on all outgoing paths. |
| Grok fixes and account refresh preference | Official | Adopt long-context billing, media-family exclusion, and module-initialization preference restore. |

## Fingerprint Composition

There is one account-aware staged fingerprint ID set for HTTP forwarding. The
HTTP request builder resolves the credential-owning account first, derives IDs
once from original client headers, and reuses those IDs and
`turnStartedAtUnixMS` for headers and map/raw `client_metadata`.

| Stored/input state | Effective mode |
| --- | --- |
| Missing key | `session` |
| Empty value | `session` |
| Invalid value | `session` |
| Explicit `off` | `off` |
| Explicit `device` | `device` |
| Explicit `session` | `session` |
| Explicit `full` | `full` |

Each HTTP attempt replaces the staged value after account selection: the
passthrough path clears it on entry, while the transformed path stores either
the selected owner's IDs or nil after OAuth normalization. Stored IDs are used
only when their account ID matches the selected credential owner, preventing
failover from account A to B from reusing A's identifiers. Native and legacy
compact requests store nil and skip fingerprint header/body convergence.

WebSocket forwarding is outside this convergence guarantee. Its
`response.create` payload continues to use the existing transformed metadata,
while pooled connection handshake headers retain their existing compatibility
and reuse behavior. Making request-scoped fingerprint headers part of a pooled
handshake requires a separate connection-key and lifecycle design; this merge
does not claim or introduce that change.

Frontend create/edit/bulk forms display `session` for missing or invalid
values. Saving `session` deletes `codex_fingerprint_mode`; saving `off`,
`device`, or `full` writes the explicit value. Because bulk account updates
merge JSONB fields, the bulk form sends JSON `null` as a deletion sentinel and
the repository removes the key instead of persisting JSON null.

## Request Routing

Responses routing matches registered route prefixes exactly, then validates
any forwarded suffix with the existing structural character allowlist and
depth limits. Only a route-compatible trailing slash is normalized; leading or
trailing whitespace remains visible and is rejected.

Native compaction v2 is a bare `/responses` request with `stream: true` and an
input item of type `compaction_trigger`. It keeps the upstream path, uses
Responses capability and Plus text-profit admission, and receives the
session-level `remote_compaction_v2` beta feature. Legacy compact is the
endpoint family rooted at `/responses/compact`, including existing structurally
safe forwarded subpaths; it uses compact capability/model mapping and stays
unary.

## Turn-State Provenance

Provenance keys normalize shadow/Spark accounts to their credential-owning
account. A known blob from another owner is stripped; owner/shadow transitions
for the same credential are permitted. New provenance is recorded only at the
response-header commit point. Failed, abandoned, or retried upstream attempts
must not populate provenance.

The implementation uses an optional `OpenAICodexTurnStateOriginStore` provided
by the existing gateway cache. Redis stores a SHA-256 digest of the API-key and
session seed with the same TTL as the sticky session, so raw session material
does not enter Redis keys. The process-local map remains a cache and fallback
when the shared store is unavailable.

## Rollup Consistency

Migrations 222 and 223 remain forward-only and unmodified. Application and
database session timezones are aligned. Recompute, retention cleanup, and
partition deletion invalidate affected rollup days; startup/scheduled sync
rebuilds missing data. A configured timezone-name change forces rebuild so one
deployment never mixes bucket definitions.

## Verification

Run focused backend and frontend tests during resolution. Final checks include
no conflict markers, no active official module imports, migration tests,
`go mod tidy -diff`, frozen pnpm locking, locale parity, OpenSpec strict
validation, and version/upstream mapping agreement. Full push/release matrices
remain deferred to their repository skills because no publication was asked.
The integration-tagged PostgreSQL rollup tests and the full backend integration
matrix passed against isolated PostgreSQL 18 and Redis 8 services on the Apple
Container network. Pooled WebSocket handshake fingerprint convergence remains
a known pre-existing limitation rather than part of this sync.
