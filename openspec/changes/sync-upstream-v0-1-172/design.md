## Baseline and ownership

The merge input is official tag v0.1.172 at commit
155c494964c3ea6ecc31f52679525c1034bf0f16. Official behavior is authoritative
for security, protocol compatibility, billing correctness, and fault recovery.
Plus remains authoritative for module identity, independent distribution and
release naming, five-hour subscription quotas, and its unified outbound Codex
identity resolver.

## Migration and generated code

The official migration filenames 194 and 195 collide numerically with released
Plus migrations. Before this merge is committed, their unchanged SQL is added
as 200_add_usage_log_upstream_response_model.sql and
201_add_usage_log_upstream_model_mismatch_index_notx.sql. No released Plus
migration is changed, removed, or renamed. The resolved UsageLog schema is the
source for regenerated Ent and Wire output; generated files are not hand-merged.

## Subscription windows

Daily quota windows are calendar windows and use timezone.StartOfDay(now).
Weekly and monthly windows remain anchored to the renewal or activation time.
The Plus five-hour window also uses the activation/reset time. Repository
interfaces and all implementations, mocks, and tests take a daily anchor and a
periodic anchor, with the latter used for the five-hour reset.

## Codex identity

The standard fallback identity is User-Agent
`codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color`, Originator
`codex-tui`, and Version `0.147.0`. The Plus resolver remains the only authority
that chooses an outbound identity source, in this immutable order: a valid UA
from the credential-owning account, then a valid global UA, then the compiled
default as the final fallback. Spark shadows use their credential parent. An
empty or invalid candidate falls only to the next source; inbound headers,
generic header overrides, request classification, retries, and probes never
participate in source selection.

Version resolution is a separate step after source selection: a valid,
upstream-supported explicit administrator version wins, otherwise an
auto-synced stable version is used only when it is not older than the compiled
version, otherwise the compiled version is used. Rebuilding may update the
leading and official trailing version declarations of the selected UA, but
cannot replace its source, client family, Originator, OS, architecture, or
terminal fingerprint. Every path must therefore emit one coherent User-Agent,
Originator, Version triple without allowing version synchronization to turn an
account or global identity into the default identity.

## Documentation and release

Plus documentation and sponsor-asset removals stay in place. Upstream README
sponsor changes are intentionally excluded. The embedded version, release
metadata, and UPSTREAM.md use v0.1.172+custom.001, but no release publication
occurs in this implementation.
