## Context

The gateway already stores an explicit mode on each real OpenAI OAuth
credential owner and stages one set of IDs for the ordinary HTTP path. The
remaining defects are orchestration defects: request classification is too
coarse, preparation is duplicated or missing, and WebSocket handshake/payload
builders do not consistently consume the staged state.

Fingerprint session metadata and Plus cache session aliases have different
owners. Fingerprint mutation runs first; the finalized body
`prompt_cache_key` remains authoritative for `session-id` and `session_id`.

## Decisions

### Use an explicit request policy

The fingerprint layer classifies requests as ordinary, native compact, the
ChatGPT Codex OAuth legacy compact compatibility path, or non-session. Ordinary
and native requests resolve IDs from the credential owner's configured mode.
Legacy compact resolves no IDs for `off` and an installation-only set for every
other mode. Non-session callers do not invoke preparation. Response retrieve,
cancel, and arbitrary non-create subpaths remain non-session even though the
gateway can forward them.

### Centralize preparation, keep body adapters small

One service helper resolves the credential owner, clears stale request state,
and computes IDs exactly once. Map and raw-body adapters apply that same set to
`client_metadata` and store it for final HTTP or WebSocket header builders.
Every account attempt clears the stage before account-type branching, so a
retry that changes from OAuth to API-key transport cannot reuse another
owner's stage.

### Compose fingerprint and cache identity

Header builders apply staged fingerprint carriers after bounded inbound/header
mutation and before final outbound identity. They then reapply the finalized
Plus prompt-cache session aliases. `client_metadata.session_id` is not forced
to equal the final cache session when their policies intentionally differ.

### Treat direct WebSocket frames as turns

Every accepted `response.create` frame prepares a fresh turn set. Stable
installation/session/thread/window values remain deterministic for the
credential owner and client-original session, while the turn identifier and
timestamp are generated for that frame. The first set is applied to the
upstream handshake. Follow-up
frames update body metadata; a newly acquired handshake consumes the latest
set before compatibility selection. Connection-pool compatibility compares the
final stable handshake carriers directly. It does not infer which fields matter
from the scheduled account's mode, because `off` and `device` preserve
client-owned values and a credential shadow does not persist an independent
mode.

### Repair only existing embedded carriers

If `x-codex-turn-metadata` is absent, fingerprinting does not add it. If it is
present and a valid object, non-owned fields are retained. Invalid JSON, null,
arrays, and scalars are replaced with the minimum object containing the owned
fields. A nil map is never written to.

### Keep the Plus persistence model

This change does not add `codex_fingerprint_seed`, alter migration 224, rotate
stable IDs, or rewrite a root cache key to match fingerprint metadata. An
opaque namespace, if ever required, is a separate persistent-data change.

## Rejected Alternatives

Restoring the complete upstream v0.1.178 fingerprint implementation would
replace the Plus mode default, stable-ID derivation, and cache authority.
Keeping all compact traffic excluded leaves native mode configuration
ineffective. Applying full convergence to legacy compact would overwrite its
protocol-specific cache/session namespace.
