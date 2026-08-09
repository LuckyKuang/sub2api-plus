## Baseline and ownership

The merge input is official tag v0.1.173 at commit
29009f0b2ea14edf3b11ae2564fb617ff91a03b4. Official behavior is authoritative
for Grok protocol support, Gemini compatibility, pricing fields, registration
policy, and channel monitor V2. Plus remains authoritative for its independent
module and distribution identity, immutable Codex outbound identity precedence,
five-hour quotas, safe monitoring defaults, and already-published migrations.

## Migrations

Official migration numbers overlap the Plus migration history. The upstream SQL
is therefore represented by new, unique Plus filenames 202 through 218.
Migrations published through v0.1.172+custom.001 remain byte-for-byte immutable.
Because the new migrations have not yet shipped in a Plus release, only their
final checksums are valid; draft checksums do not receive compatibility rules.

Migration 203 seeds channel monitor mode as V1 so upgrades do not implicitly
disable active probes. Migration 218 preserves composite-group video pricing
and uses the Plus-owned backup table name associated with its local prefix.

## Grok safety boundaries

Cross-client model mapping is an explicit opt-in. A missing, empty, malformed,
or `false` setting must produce `false`; only the exact persisted value `true`
enables mapping. This prevents an upgrade from silently translating existing
GPT or Claude model names into Grok models.

Password-to-SSO Grok OAuth remains hard-disabled at the service boundary. The
configuration field is retained so existing configuration files still parse,
but its value is ignored. Capabilities always report the feature disabled and
the password endpoint rejects requests before contacting the upstream client.

## Gemini and Antigravity compatibility

Gemini pool-mode accounts without custom error-code policy stay schedulable on
429 responses; bounded same-account retry and failover own the recovery path.
Custom error-code policy continues to take precedence.

Gemini image usage is derived from actual inline image outputs when available,
then falls back to requested or mapped image-model recognition. Each forwarding
attempt resets its request-level observer so failover cannot carry image counts
between accounts. Antigravity SSE response-model observation reads the original
event envelope because model metadata can exist outside the unwrapped payload.

## Merge-sensitive gateway invariants

OpenAI OAuth session identifiers must be resolved through the Plus sharing
policy on ordinary Responses HTTP, passthrough HTTP, and Responses WebSocket
paths. Allowed groups for the same user and credential-owning account share the
same upstream namespace; unauthorized groups fail closed. This changes only
session namespace selection and does not alter the immutable outbound identity
precedence.

Responses streaming wrappers retain any result already observed by the shared
stream parser when the parser also returns an error. The partial result carries
usage, response and request identifiers, first-token/first-output timing, output
kind, search/image counts, and client-disconnect state so billing and metrics do
not silently lose completed upstream observations.

Anthropic Messages requests routed to Grok use the configured global default
base-URL mode both initially and for the encrypted-content retry. OpenAI usage
aggregation includes audio output tokens alongside text, cache, and image token
classes.

## Release preparation

The embedded version, Docker build arguments, deployment examples, release
notes, and UPSTREAM.md describe v0.1.173+custom.001 based on official v0.1.173.
Release publication remains a separate explicitly authorized operation.
