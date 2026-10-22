# Official agent request state

The reviewed source commits and entrypoints are in
[OFFICIAL_AGENT_BASELINES.md](OFFICIAL_AGENT_BASELINES.md). This contract supplements
[OUTBOUND_IDENTITY.md](OUTBOUND_IDENTITY.md): request state cannot select a UA,
client identifier, version or credential owner. OAuth and API-key paths both
apply it, including retries, probes, HTTP/WS and auxiliary calls.

| Agent | Field | Format and lifetime |
| --- | --- | --- |
| Codex | installation | Persistent UUIDv4 for the credential owner; carried in protocol metadata, not a direct Responses installation header. |
| Codex | session, thread, turn | UUIDv7. Mapping a valid v7 preserves its 48-bit creation timestamp; independently mapped random bits preserve references. Legacy or synthetic IDs receive persistent random v7 values. |
| Codex | window ID | Mapped thread UUIDv7 plus the original `:<counter>`. A retry does not reset the counter. |
| Codex | context window ID | UUIDv7 with its own compaction-window mapping; changes only when the original context window advances. |
| Codex | parent/root/fork references | Use the same mapping as their corresponding thread or turn, including embedded turn metadata. Missing turns or start timestamps are not invented. |
| Grok | OAuth state, nonce | New UUIDv7 per authorization. The stored nonce is checked against the signed ID token. |
| Grok | device, agent | Persistent random UUIDv4 device seed; official UUIDv5 agent derivation. Process restarts do not create a new device. |
| Grok | conversation/session | UUIDv7, scoped to the authenticated tenant, logical conversation and credential owner. |
| Grok | sampler request | UUIDv4 per logical prompt; unchanged across retries of that prompt, new on the next WS turn. Auxiliary prompts, including each Composer image caption, keep independent request IDs while retaining the parent conversation. |
| DeepSeek | harness session | `session-<UUIDv4>`, persistent for the tenant/conversation/owner. The separate anonymous user ID keeps its existing persistent owner scope. |
| MiniMax | Mavis session | `mvs_<32 lowercase UUIDv4 hex digits>`, persistent for the tenant/conversation/owner. |
| Kimi | device | Existing configured/persisted device UUID and selected device name; client defaults now track Kimi Code 2.3.0. |
| ZCode | device mid | Valid configured device ID, otherwise persistent random UUIDv4. Control header and body use the same device; model requests use model declarations. |
| ZCode | request/trace | Request UUIDv4 changes per physical send; trace stays with the logical operation across retries. Session/query declarations are emitted only with a corresponding conversation/query. |
| Step-Code | request identity | The official versionless inference UA and pinned SDK fingerprint remain; no foreign agent ID headers are added. |

## Persistence and isolation

Migration 294 creates `outbound_identity_ids`. Its composite key is a namespace
and SHA-256 scope digest; the value is the first random canonical UUID retained
atomically across concurrent replicas. Reads survive process restarts and do
not inherit a short scheduler-cache TTL. Rows contain no raw prompt, session,
token or credential. Mapping failures stop preparation before upstream writes;
there is no production process-local fallback. Test doubles explicitly supply
an isolated store.

A reliable conversation identifier is scoped to the authenticated tenant and
credential owner. When no such identifier exists, the random session survives
only within the current request/retry scope: identical prompt text does not
establish shared conversation identity. New domestic OAuth authorizations store
an `oauth_login_id`, retained by routine refresh. Existing legacy authorizations
use a stable account-row authorization scope until reauthorization. Domestic, Grok and Codex API-key
replacement uses a key digest to isolate the new grant. Raw keys never appear
in the mapping table. Shadow accounts resolve the credential-owning parent.

DeepSeek and MiniMax's previous bare UUIDv5 session values cannot be retained
while adopting their official UUIDv4 formats. Their first request under this
version creates the persistent v4 mapping; the logical route key and selected
account affinity stay unchanged. Existing valid device pins remain unchanged.
Drain older workers during deployment so a conversation does not alternate
between the old and new outbound session formats.

Codex installation remains scoped to its configured device/account, so API-key
replacement does not reset the device. Execution IDs additionally isolate the
actual API-key digest. OAuth execution uses verified ChatGPT account/user claims
when available; legacy OAuth rows without principal claims retain their local
row scope until usable principal data is available. Such unknown principals
cannot authorize opaque state replay. Short-lived token refresh does not reset
the same principal's execution mapping.

Responses HTTP, passthrough, Chat Completions and Messages acquire the outbound
credential and freeze its owner before mapping client metadata or staging
fingerprints. Body IDs, header IDs and installation projection use that same
snapshot, including credential-shadow parents and refresh results. A later
change to the selected account row cannot replace the acquired grant's
namespace. Missing or ambiguous acquired-owner evidence stops preparation;
failover acquires and freezes the replacement owner before mapping the original
logical IDs again. See
[CODEX_CLIENT_PROFILES.md](protocols/CODEX_CLIENT_PROFILES.md).

## Cache and opaque state

Native Codex Responses/WS preserve an absent `prompt_cache_key`. A caller's
custom cache key and session are independently scoped; distinct native threads
are not collapsed merely because they share a session. Ordinary routing
failover retains the tenant's logical cache identity. Explicit OAuth session
sharing remains an authorization boundary: crossing its owner group re-scopes
the cache identity.

Codex turn-state provenance binds the execution scope, opaque blob digest and
actual transmitted credential owner. Local and Redis provenance use the same
contract. Native WS handshakes and HTTP bridge responses record provenance
using the acquired immutable credential snapshot even without session routing;
a relink of the selected account row cannot transfer the response to its new
owner. Unknown, expired or previous-owner state is not forwarded. Live call
records retain the creating call's complete outbound identity/ID snapshot;
creation resolves the client identity and proxy from the acquired credential
owner, including a refreshed shadow parent. The selected row still owns routing
and resource accounting. Sideband and reconnect reuse the creation identity/IDs
and use the proxy frozen by their own credential acquisition. A normal refresh
may resume the same owner, while a relink to another principal cannot attach to
the old call. Records without a complete valid creation snapshot require a new
call.

## Conditional state and failover

Quota, billing and reactive errors belong to the grant that produced them.
Conditional repository mutations compare the transmitted credential snapshot,
platform, account type and proxy before changing state. The state and scheduler
outbox update are atomic. A mismatch or storage error cannot trigger an
unconditional follow-up write or publish a cooldown for a replacement grant.
An unsuccessful refresh CAS also leaves the current grant's access-token cache
untouched.
Immediate runtime quarantine remains bound to the observed credential generation
while an uncertain commit outcome is resolved. Successful refresh and state
mutations evict the scheduler snapshot with a
bounded cleanup context; the atomic outbox drives authoritative reload. They
never publish a row read before a concurrent login could replace it.
Runtime blocks and Grok model/quota/billing snapshots carry the credential
generation. Slim scheduler projections retain that non-secret digest, but cannot
act as mutation evidence.
CN quota projections additionally retain the existing CN credential/proxy-status
digest, `<provider>_quota_generation`, recognized window values/reset times and
unlimited flags. Hydrated rows recompute these digests from the complete grant;
slim rows cannot become full credential snapshots or authorize quota mutations.
This preserves both threshold pauses and MiniMax OAuth's observed full-exhaustion
exclusion after metadata serialization. Existing metadata receives the new
projection on the next account snapshot refresh.

Session affinity remains preferred while the selected grant is eligible.
Confirmed exhaustion or a terminal authentication failure follows the existing
bounded next-account policy; the next owner receives its own outbound state.
Transient request errors retain their existing bounded retry classification.
A response from an old grant cannot disable or authorize its replacement.

Grok login validates the ID-token signature, allowed asymmetric algorithm,
issuer, audience, expiry, required claims and stored nonce. Discovery/JWKS use
the trusted login identity, fixed issuer origin and no bearer or redirects.
Refresh does not reuse a login-only nonce requirement. Official team-response
exceptions require the trusted token response; unverified JWT fields cannot
select an exception.

MiniMax OAuth quota lookup uses the personal workspace and region-specific
Matrix/token-plan APIs. Its Matrix operations, including the identity request at
`/v1/api/user/info`, use `MiniMaxCode`; managed
inference uses `MiniMaxAgent`, and BYOK inference uses its pinned Anthropic SDK.
The identity policy's deliberate UA addition where official fetch specifies
none remains documented in [OUTBOUND_IDENTITY.md](OUTBOUND_IDENTITY.md).

## Audit and verification

Outbound identity, metadata and state preparation occurs after ingress audit.
Every subsequent Responses WS turn enters its canonical audit hook after basic
validation and before metadata/fingerprint preparation or persistent ID-store
access, in both native connection-pool and HTTP bridge modes. Upstream retries
reuse that audited turn rather than invoking audit again.
These IDs are protocol context, not prompt content. Unknown valid structures
still pass through, and recognized sibling content remains auditable by both
engines. The HTTP/WS and account-type side-effect-order regressions remain
mandatory; see [SECURITY_AUDIT_CONTENT_COVERAGE.md](SECURITY_AUDIT_CONTENT_COVERAGE.md).

Regression owners include `outbound_mapped_id*`, `credential_state_mutation*`,
`openai_codex_typed_identity_regression*`, `openai_live_identity_snapshot*`,
`grok_snapshot_owner*`, `domestic_request_contract*`, `domestic_credential_owner*`,
the repository SQL/Redis tests and provider wire tests. Expected UUID versions,
header/body associations and forbidden cross-owner writes are asserted from
this contract, independently of the implementation.
