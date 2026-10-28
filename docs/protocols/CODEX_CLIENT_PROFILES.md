# Codex Client Profiles

## Purpose

For an OpenAI OAuth account with **Approved Codex client profiles only**
enabled (`extra.codex_cli_only = true`), the gateway admits supported Codex
request profiles. This is an access-control compatibility policy for request
shapes; it is **not** a cryptographic attestation of an installed official
client and it cannot, on its own, prove or disprove account sharing.

The policy applies before upstream credential use on these ingress paths:

- OpenAI Responses (`/v1/responses`)
- OpenAI Chat Completions (`/v1/chat/completions`)
- Anthropic Messages routed to an OpenAI account (`/v1/messages`)
- Anthropic Messages Count Tokens routed to an OpenAI account (`/v1/messages/count_tokens`)
- OpenAI Alpha Search (`/alpha/search`)
- OpenAI Responses WebSocket

The Messages Count Tokens bridge audits the original inbound Messages body
before routing, billing eligibility, account selection or AgentIdentity task
registration. Both audit engines follow the shared
[content coverage contract](../SECURITY_AUDIT_CONTENT_COVERAGE.md), including
unknown-content pass-through and auditing recognized siblings.

## Built-in official profiles

The built-in registry is a closed, source-controlled list of the underlying
wire identities used by supported OpenAI Codex clients. The public Codex source
explicitly identifies `codex_cli_rs`, `codex-tui`, `codex_vscode`,
`codex_atlas`, `codex_chatgpt_desktop`, and the case-sensitive `Codex ` product
family. The latter covers the documented first-party product surfaces whose
wire identity is in that family, such as Codex Desktop and Codex JetBrains.
The reference implementation is the public
[OpenAI Codex repository](https://github.com/openai/codex); profile changes
are reviewed against a specific upstream source or release rather than fetched
at runtime.

The current registry was checked against upstream commit
[`a7ab2d66`](https://github.com/openai/codex/blob/a7ab2d66d781b903cb060288a89e26e8d2b9a05f/codex-rs/login/src/auth/default_client.rs#L40-L165).
The default HTTP client supplies the process-level `User-Agent` and
`originator`. A thread may then override only `originator`; it does not rewrite
the process-level User-Agent. The reviewed product-service sources are
`chatgpt_cca`, `codex_work_desktop`, `codex_work_web`, `codex_work_mobile`, and
`codex_work_cca`, as defined by the upstream
[`ThreadManager`](https://github.com/openai/codex/blob/a7ab2d66d781b903cb060288a89e26e8d2b9a05f/codex-rs/core/src/thread_manager.rs#L300-L313).
The upstream Alpha Search integration test specifically sends
`originator: chatgpt_cca` while using the shared Codex HTTP client.

For a built-in profile, all of the following are required:

1. `User-Agent` and `originator` are both present.
2. The leading User-Agent client name is in the reviewed transport registry,
   or is the exact case-sensitive upstream `Codex ` product family.
3. The leading User-Agent version is valid semantic version text.
4. The originator either exactly matches the leading User-Agent transport
   identity or is one of the reviewed product-service sources above. A
   different transport identity is not accepted as a thread override.
5. At least one known, non-empty Codex request header is present:
   `x-codex-installation-id`, `x-codex-routing-hint`,
   `x-codex-turn-state`, `x-codex-turn-metadata`,
   `x-codex-parent-thread-id`, or `x-codex-window-id`.

The public Responses WebSocket implementation sends `x-codex-window-id` on
its handshake (and may send parent-thread and turn metadata context). The
gateway preserves the bounded supported context headers. It derives a fresh
routing hint after selecting the destination account, rather than forwarding a
caller-supplied hint for a different account.

An arbitrary `X-Codex-*` header, a User-Agent substring, a trailing User-Agent
identity, an unknown or missing `originator`, or a case-rewritten official
identity does not pass this gate. Product-service originators do not make an
unknown transport User-Agent official. The optional global minimum/maximum
Codex version bounds apply to built-in profiles. Policy versions use strict
SemVer 2.0: they require a complete `MAJOR.MINOR.PATCH` core without a `v`
prefix or leading zeroes.
Valid prerelease and build metadata are accepted, with normal SemVer
precedence (`0.164.0-alpha.4` is lower than `0.164.0`, and build metadata does
not change precedence). Historical outbound version normalization remains a
separate compatibility concern and does not relax these policy bounds.

## Legacy profile compatibility mode

The global **Legacy Codex Client Profile Compatibility** switch is disabled by
default. When an administrator explicitly enables it, the gateway temporarily
recognizes exactly these historical wire identities:

- `codex_app`
- `codex_exec`
- `codex_sdk_ts`
- `codex_vscode_copilot`

This is a closed migration list, not an alternate official registry. A legacy
match is recorded as `codex-legacy-compatible`, never as an official profile.
It must still have the exact, case-sensitive leading `User-Agent` identity and
`originator`, a complete semantic version, and a known non-empty Codex evidence
header. The same version bounds apply. The switch does not allow other
`codex_*` values, case variants, substrings, or a trailer-derived identity.

The switch applies consistently to both enabled-account ingress policy and
administrator-configured account/global outbound User-Agents. If it is turned
off, a stored legacy outbound UA is invalid and resolution falls through to the
next source in the normal account → global → compiled-default order. To turn
the switch off while the global UA is legacy, clear or replace that global UA
in the same settings save.

## Compatibility clients and policy precedence

The global **User-Agent/Originator whitelist** remains available for a
non-official client that has been explicitly reviewed. A whitelist entry is a
compatibility exception, not an official profile. It needs an exact configured
originator, every configured User-Agent marker, a coherent leading User-Agent
identity, and one of the known Codex headers above.

The global blacklist is checked first and always wins. The retired generic
App Server allow switch, per-account App Server override, generic body/header
fingerprint rules, and fingerprint-bypass option do not affect authorization.
`gateway.force_codex_cli` is not an identity source and cannot bypass inbound
access control or replace the selected outbound identity.

## Official client turn-context headers

The official Codex client emits bounded per-turn context headers on Responses
requests. Because the public WebSocket handshake reuses the Responses header
builder, the same context can also appear on the Responses WebSocket handshake:

- `x-openai-subagent` labels a subagent turn with `review`, `compact`,
  `memory_consolidation`, `collab_spawn`, or a configured label.
- `x-openai-memgen-request` marks a memory-generation request.
- `x-responsesapi-include-timing-metrics` opts a turn into timing metrics;
  the official default is off, so the gateway only forwards it when the
  client explicitly sends it.

The gateway forwards these headers verbatim to the upstream on the Responses
HTTP path and the WebSocket handshake copy list, in both passthrough and
non-passthrough modes. It never rewrites, synthesizes, or drops them for
recognized clients, and account-level header overrides cannot replace them
with static values: they must reflect the live client turn state.

## Outbound fingerprint convergence

### Session ownership and credential state

Native OpenAI sessions with a stable client session signal use a durable route
inside the authenticated API key and routing group. A healthy owner takes
precedence over weighted scoring, price, latency and concurrency escape settings.
Local concurrency pressure waits on that account's existing bounded wait plan;
queue exhaustion or a temporary profit veto does not move the conversation.
Before creating a durable route, HTTP and WebSocket selection skip candidates
whose inbound client-profile policy denies the request. Rejected candidates
release their slots without becoming owners or receiving a credential-health
penalty. If all candidates deny the request, HTTP returns a policy 403 and WS
closes with a policy violation without creating a route. A denied existing
owner, including a concurrent first-route winner, remains authoritative: the
request is rejected rather than using that denial to authorize migration.
The canonical-owner path still enforces public-model channel restrictions and
the OAuth shared-response user/group/policy authorization boundary. A denied
continuation cannot use affinity as permission to send to another account.
Clients without a stable session signal retain the existing content fallback;
it cannot guarantee affinity across changing requests or WS reconnects.

Durable ownership is limited to native OpenAI text generation (Responses,
Chat Completions and Messages adapters) and Responses WebSocket turns. Other
OpenAI-compatible platforms, including Composite routes resolved to those
platforms, retain their existing Redis TTL affinity and failover semantics.
Images, embeddings, alpha search, Live, token counting and legacy
`/responses/compact` do not read or write the durable route, acquire migration
leases, or change a text/WS owner. Their selection and post-slot binding use
ordinary affinity even when the durable store is configured or unavailable.
Native remote compaction inside a Responses generation retains that
generation's ownership lifecycle.

Recoverable failures retain the selected owner during existing bounded retry
budgets. OAuth keeps its credential-refresh and transient-429 window. Native
API keys use the existing same-account count for short transient 429s before
migration, without parking that credential before its retry. A provider
`Retry-After` beyond the existing short retry delay bound returns a retryable
response to the client instead of scanning other owners. Explicit quota,
administrative, authorization and model restrictions retain their own gates.
Request/provider failures do not scan the account pool.

Migration uses the generation observed at selection and a candidate lease.
Admission and HTTP/WS handshakes leave the previous owner in place. Valid
semantic output or a verified successful terminal commits the candidate before
that result is delivered. Chat Completions without text require a successful
finish and positive usage; structural chunks stay buffered until acceptance.
Responses output arrays containing only empty message containers are structural
too; without semantic output, a successful terminal needs positive usage.
Failed candidates and lost leases retain the old
owner. Once committed, recovery of the old account does not reclaim the session.
During WS passthrough migration, classify upstream failures before testing
candidate acceptance. A `cyber_policy` rejection retains its risk-control mark,
usage evidence and client event, with no retryable routing error, credential
penalty or owner commit. Ordinary credential failures retain their account
health effects; pre-output rate limits retain bounded same-account recovery.
An unavailable shadow credential owner can make the shadow ineligible, but a
failed parent query cannot establish unavailability. Query failures stop
selection, including candidate filtering, post-acquisition rechecks and wait
fallbacks, without sending to or committing a replacement account.
PostgreSQL is read directly for ownership: stale Redis fills cannot authorize a
send. Route-store failures stop admission or result delivery. An ordinary sticky
bind cannot change a committed owner.

The migration lease lasts one minute and renews every twenty seconds until
acceptance, bounded by the existing first-output timeout (one minute when no
timeout is configured). Cancellation, expiry and failed renewal fence detached
usage-draining requests too. Route errors return a retryable
`session_route_unavailable` HTTP 503 or a WS reconnect result; they do not count
as account health failures. Measured upstream usage on a rejected commit stays
attributed to that candidate as incomplete usage, without automatically
replaying the request. This does not promise exactly-once upstream execution.

OAuth and API-key candidates retain their own endpoints, authentication and
trusted identity snapshots. Logical cache identity is separate from upstream
execution state. A previous response reference is private to its credential
owner; a text delta, or matching tool IDs alone, does not prove full history.
Cross-owner continuation requires complete replayable history. Otherwise the
client must resend full history without the old reference or wait for its owner.
An `item_reference` cannot supply the referenced history on a different owner.
Responses, Chat and Messages adapters all require actual matching tool-call
history for tool outputs before attempting cross-owner forwarding.
WS replay history is complete only when it originates in an observed complete
turn and all required tool dependencies remain available.

Opaque turn-state provenance binds the execution scope, the exact blob's
irreversible digest, and the actual upstream principal. A reused local account
row is not owner evidence. A normal access-token refresh for the same principal
preserves state; relinking that row or changing owner strips it. Late responses
cannot relabel another blob. Unknown, expired and legacy provenance cannot
authorize replay. HTTP, WS and shared-cache replicas enforce the same rule;
credential-shadow requests use their immutable transmitted parent snapshot.
Messages compatibility continuations and cached turn-state use that same
acquired principal in addition to their tenant/sharing scope. Acquisition
precedes replay lookup and history trimming; API-key replacement and OAuth
relink start with full history and no previous owner's opaque state. OAuth
claims supplied on API-key or setup-token rows cannot override their actual
bearer identity.
Session/response connection references also retain their credential owner and
execution scope. All replicas must be upgraded together before relying on the
new mapping and provenance format; old processes do not enforce it.

API-key replacement on the same account record retains its installation/device
identity but changes execution session, thread, turn, parent and window mappings.
The execution namespace includes the actual key's irreversible digest; a stable
account ID alone cannot authorize old turn-state or response references. This
is independent of the logical cache namespace, which ordinarily survives
credential-owner failover within its authorized tenant scope.

Every credential-owning OpenAI OAuth account stores an explicit
`extra.codex_fingerprint_mode`. New accounts default to `device`; missing,
empty, null, or malformed legacy values are normalized to `device`. API-key,
setup-token, and credential-shadow accounts do not own this setting.

| Mode | Upstream-visible identity behavior |
| --- | --- |
| `off` | Disable optional convergence. Typed owner isolation, official carriers, cache, security, sharing and compact policy still apply. |
| `device` (default) | Converge only the installation identifier to an account-level stable value; preserve each client's session and thread boundaries. |
| `session` | Converge installation and metadata session identifiers; retain distinct mappings for actual threads and their parent/fork relationships. |
| `full` | Converge installation, session, and thread identifiers to account-level values. |

Administration create, edit, bulk edit, Codex import, PAT creation, and CRS
synchronization persist the selected value rather than representing a default
by deleting the key. Scheduler metadata snapshots retain the explicit mode so
a selected account does not silently fall back to the default.

Request policy is endpoint-specific:

Here `legacy` names the ChatGPT Codex OAuth compatibility branch used by this
gateway. It does not mean the public API-key
[`/v1/responses/compact`](https://developers.openai.com/api/reference/java/resources/responses/methods/compact)
endpoint is unavailable.

| Request path | Fingerprint policy | Final session/cache authority |
| --- | --- | --- |
| Ordinary Responses create turns and Chat/Messages Responses bridges | Configured mode | Independent execution-session and cache policy |
| Native remote Compact v2 (`/responses` with `compaction_trigger`) | Configured mode | Independent execution-session and cache policy |
| ChatGPT Codex OAuth legacy compact compatibility path | `off`, or installation-only for every other mode | Legacy compact session/cache/thread namespace |
| HTTP-to-WebSocket and direct Responses WebSocket `response.create` turns | Configured mode per turn | Independent execution-session and cache policy |
| Count-tokens, alpha-search, response retrieve/cancel subpaths, and other non-session endpoints | No fingerprint mutation | Endpoint policy |

Personal access token and Agent Identity accounts are OpenAI OAuth credential
owners and follow this endpoint matrix. API-key and setup-token accounts are
excluded. Credential shadows read the mode and stable installation source from
their credential-owning parent; the shadow never creates an independent
fingerprint identity.

Responses HTTP, passthrough, Chat Completions and Messages freeze the owner of
the acquired credential before mapping client metadata or staging fingerprints.
The upstream destination, proxy, configured headers, trusted client identity,
header/body IDs and installation projection share that immutable owner,
including resolved shadow parents and refresh results. Later selected-row
changes cannot alter its namespace; ambiguous acquired-owner evidence stops
preparation. A replacement account acquires its own credential snapshot and
maps the original logical IDs for that owner.

Each subsequent native or HTTP bridge Responses WS turn completes basic
validation and canonical ingress audit before metadata/fingerprint preparation
or persistent ID-store access. Audit receives the original frame and runs once
per accepted client turn; retries retain that audited turn. Unknown valid
siblings preserve the shared extraction/pass-through rules.

Native Responses HTTP/WS requests keep explicit execution session and custom cache
keys separate; omitted cache keys remain omitted for both roots and subagents.
Gateway-created compatibility adapters and legacy compact retain their own
explicit fallback policies. Native OAuth and API-key requests use only
`session-id`, removing legacy aliases and direct installation headers.
Installation appears in `client_metadata` and existing canonical turn metadata.

Session/thread/turn/context-window IDs use UUIDv7, preserving the timestamp of
valid incoming v7 values. Legacy values use the durable atomic mapping store;
installation defaults use persistent random v4 values, and valid device pins
remain stable. Window IDs retain `<mapped thread>:<actual counter>`. Parent,
root and fork references use the target object's mapping. Original turn start
time is preserved, repeated generations of the same turn retain its ID, and
missing turns/prewarm do not acquire fabricated IDs. Metadata session/full
convergence never changes the routing session's authorization domain.

Malformed embedded metadata that already has a carrier is rebuilt with only
known installation information; it does not invent logical session, thread,
turn or window state. Unknown valid fields stay intact. Missing canonical
carriers are not fabricated. WebSocket reuse compares final stable handshake
carriers in all four modes.

Outbound User-Agent identity has one immutable source order: a valid
credential-owner `credentials.user_agent`, then a valid global
`openai_codex_user_agent`, then the compiled default. Originator and Version
are derived coherently from that selected client family. Manual version updates
may update only its version declaration and cannot replace the selected source,
OS, architecture, terminal fingerprint, client family, or Originator.
The account-aware resolver is the final identity authority for Messages,
native Alpha Search, the PAT Responses web-search fallback, and OAuth model
manifest synchronization. Endpoint header staging, inbound identity headers,
gateway.force_codex_cli, and model-manifest URL construction cannot select a
different source or split the three declarations.
Agent Identity task registration and its immediately retried upstream request
reuse one resolved snapshot; a concurrent settings update takes effect only on
the next independently resolved request.
The Agent Identity principal binds the runtime and signing public-key
fingerprint, including when no bearer token exists. A task-ID update for that
same principal may be adopted while preserving the frozen identity, execution
IDs and state owner. A concurrent runtime, signing-key or proxy change fails
before signing or sending; registration uses a credential compare-and-swap so
its result cannot overwrite a newer account binding.

### Mandatory source-priority matrix and exact default

| Credential-owner account candidate | Global candidate | Selected source |
| --- | --- | --- |
| Valid | Any | `account`; retain that account's client family and fingerprint |
| Empty or invalid | Valid | `global`; retain the configured global family and fingerprint |
| Empty or invalid | Empty or invalid | `compiled_default` |
| Raw candidate containing CR/LF or other invalid header bytes, including leading/trailing bytes | Any | Treat that candidate as invalid before whitespace normalization and proceed to the next source; cached global reads preserve this behavior |
| Empty or invalid | Missing, blank, or unavailable persisted setting, including a cached read | `compiled_default`; the settings reader must not synthesize a global candidate |
| Credential shadow | Any | Resolve the credential-owning parent and apply the same matrix; a shadow does not supply a UA |
| Legacy candidate with compatibility disabled | Any | Treat that candidate as invalid and proceed to the next source |
| Independent monitor/audit supplier token, with no account UA candidate | Valid, otherwise empty/invalid | `global`, otherwise `compiled_default`; never inherit a forwarding account or its cached identity |

This source matrix applies identically to OAuth and API-key HTTP/WS provider
requests, including retries, discovery and probes. Transport preference and
inbound client classification never change the selected source.
The October 28 manual upgrade selects stable `0.165.0` after this matrix;
it changes the version declaration only. Valid administrator version overrides
retain priority and every selected family, OS, architecture and terminal stays intact.

OpenAI monitor checks, Prompt Audit scans/model probes and Content Moderation
requests resolve the native Codex global/default identity and emit the same
UA/Originator/Version triple as API-key inference. Monitors for other providers
follow their native or permitted compatible API-key policy under
[Outbound Identity](../OUTBOUND_IDENTITY.md).
Each independent operation resolves its own supplier snapshot; discovery and
inference within one probe, chunks and same-credential retries reuse it.

Account and global candidates share the same configured-UA validation, including
the 512-character limit. Create, single update and bulk update reject invalid
Codex UA candidates. Bulk updates validate all OpenAI targets before any write;
omitting `credentials.user_agent` preserves its value, while null or a blank
string clears it through an explicit null in the JSONB update.

The exact compiled identity is:

```text
User-Agent: codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color
Originator: codex_cli_rs
Version: 0.165.0
```

The version resolver runs after source selection: a valid administrator version
override, then the compiled release version. Historical synchronization values
are ignored even before cleanup migration 290; the sync service, admin endpoint
and UI have been removed. Official release upgrades are reviewed in code, never
read from the development Cargo placeholder `0.0.0`. This may change only the selected version declarations. The configured
UA parser follows the client-profile policy; the outbound version field retains
its historical normalization and minimum-version checks, including accepted
two-part versions. Four-part versions still fall back under the existing version
comparison rules; this change does not broaden the accepted version set.
OAuth and metadata adapters must preserve an already approved complete identity
and must not turn this version distinction into a client-family fallback.

One operation retains the first resolved triple for each credential owner.
HTTP forwarding, Messages and Chat Completions bridges, token counting, image/embedding requests,
WebSocket handshakes/reconnection, Live creation/Sideband credential operations,
Agent Identity registration/recovery, model
discovery, account probes, and token refresh/enrichment share this contract.
Handler-level retries reuse the request scope; another owner gets its own
snapshot. A fresh request or WS connection observes updated settings. Detached
manifest refreshes retain the scope that produced their headers and cache key.
Pooled WS reuse and prewarm targets compare all three identity declarations;
an updated triple cannot reuse an idle connection with the old handshake.
Background prewarm retains the originating request scope during Agent Identity
header renewal.
Live creation persists a call-owned snapshot containing the selected
UA/Originator/Version, UUIDv7 session/thread, residency and a digest of the real
principal. Controllers, observers and reconnects load that same shared snapshot,
including across replicas. Changes to account/global UA affect new calls only.
Authentication tokens may refresh for the same principal and are not stored in
the snapshot; relinked accounts cannot attach to an existing call. A legacy call
record without a complete snapshot cannot reconstruct its creation identity
and fails with `ErrLiveIdentityMismatch`; start a new call after upgrade. The
snapshot is validated before any sideband dial.

Final header application removes every case variant and duplicate of managed
identity headers, including foreign SDK declarations, before rendering the
selected identity. Native Codex OAuth/ChatGPT and API-key inference/discovery requests send
Originator and Version with the selected UA, including the default `codex_cli_rs`
Originator. Official Codex source `1b6e0d8145` sets UA/originator in
`login/src/auth/default_client.rs` and Version in the built-in OpenAI provider
in `model-provider-info/src/lib.rs`, independently of authentication type. WHAM usage/credits and backend auxiliary requests keep that UA but
omit Originator and Version, as specified below. Native Codex Platform API-key
requests include both Originator and Version, including `responses/compact`. Header presence is
determined by the endpoint protocol, never by an inbound or generic override
value. Explicit compatible presets retain their own protocol header mappings.
Inbound thread originators (`chatgpt_cca` and `codex_work_*`) remain valid
classification evidence but cannot replace the selected outbound Originator.
All three declarations stay on the credential-owning snapshot.
The OAuth credential endpoint follows official Codex auth clients:
authorization-code exchange uses the raw client and sends no User-Agent,
Originator, or Version; refresh uses the default client and sends the
selected User-Agent and Originator. Neither request receives the inference-only
`Version` header. Refresh and revoke also send
`x-openai-internal-codex-residency` when the global `codex_residency` setting
is `us`.
Device-code start/poll uses the same raw client (no User-Agent, Originator, or
Version) and then exchanges the returned authorization code. Device-code
sessions do not pre-bind an account; the credential is matched afterwards.
Browser authorization-code re-authorization still stores the account on the
session.
`/oauth/revoke`
uses the refresh-style client (User-Agent + Originator, no Version, plus
managed residency when enabled); `client_id`
is sent only when revoking a refresh token. Login no longer PATCHes
ChatGPT `training_allowed`; administrators can still force privacy later.

## Environment-context timezone alignment

Official Codex renders a model-visible `<environment_context>` block
(cwd, shell, `<current_date>`, `<timezone>`) from the user's machine, so a
gateway-forwarded request can carry a visible timezone that contradicts the
egress location. Plus can rewrite that pair at the outbound build stage:

- Resolution order: account `extra.codex_environment_timezone`, then the
  bound proxy's `egress_timezone` annotation (admin-maintained in the proxy
  management UI, so the visible time follows the actual egress location and
  automatically tracks proxy rebinding / failover), then the global
  `openai_codex_environment_timezone` setting, then off. Misconfigured values
  degrade to the next source and never block traffic. Only Codex-protocol
  OpenAI accounts participate.
- **The alignment defaults to off.** With no effective timezone at any level the
  client's own `<timezone>` / `<current_date>` pair reaches the upstream
  unchanged, exactly as the official client renders it. A compiled non-empty
  default would rewrite the model-visible time on every Codex request with no
  administrator decision, so the global default is empty; a deployment that
  already stored a value keeps it. Administrators opt in at the global, proxy,
  or account level.
- The pair is always written together and never contradicts itself: both tags
  are replaced with the configured IANA timezone and that timezone's current
  date (`YYYY-MM-DD`); a missing tag of an existing pair is injected before the
  closing tag in the official layout. A block without either tag is untouched.
- Only `role=user` messages whose content (string or `input_text` part) is
  exactly one `<environment_context>` block are touched; quoted logs, mixed
  prose, and other message roles stay unchanged.
- Applied at the final outbound construction on every transport (HTTP forward,
  HTTP passthrough plus WS→HTTP bridge, WS `response.create` payloads, WS v2
  passthrough frames), so failover to another account rewrites the pair to the
  new account's timezone. Rewrites are idempotent.
- Any parse or mutation failure keeps the original block — the client's own
  timezone/date pair is self-consistent — and never fails or closes a request.
  The rewrite runs after ingress security audit consumed the original body and
  never on the audit path itself.

The related egress-country declaration resolves account `extra.egress_country`,
then the bound proxy's `egress_country`, then the global
`openai_codex_egress_country`. Fresh or never-saved installations default the
global value to `US`; an explicitly saved empty value disables the declaration.
Saved values are normalized to upper-case and must be an assigned ISO 3166-1
alpha-2 code. Invalid stored values fall through to the next source and never
block forwarding.

Official Codex never sends a `conversation_id` header, so Codex-protocol
outbound requests never carry one. The official wire format spells the
session header `session-id` only: every Codex-protocol outbound path drops
the legacy `session_id` alias regardless of fingerprint mode, while non-Codex
compatible-supplier paths with an explicitly non-Codex preset keep the Plus
`session-id` + `session_id` dual spelling and inbound `session_id` remains
accepted for sticky routing. The chatgpt.com backend-api auxiliary surface
(accounts check, subscription enrich, settings PATCH, WHAM usage and credit
endpoints) uses the regular HTTP client without browser TLS impersonation and
the official backend-client header surface: selected User-Agent,
Authorization, optional `chatgpt-account-id`, and optional `x-openai-fedramp`;
it omits Originator and Version. `/backend-api/subscriptions` is a Plus-only
enrichment endpoint with no official equivalent; official expiration data comes
from `accounts/check` entitlements.
ChatGPT WHAM `/backend-api/wham/usage` and rate-limit credit endpoints follow
official backend-client headers: selected User-Agent, Authorization,
`chatgpt-account-id`, and optional `x-openai-fedramp`. They omit Originator and
Version. `/backend-api/wham/accounts/check` uses the same backend-client
surface during login enrich (Bearer + User-Agent, no Originator/Version).
Generic override saves reject managed identity headers with
`INVALID_HEADER_OVERRIDE`; runtime filtering ignores previously stored entries.
Authentication, session, routing and protocol-capability fields keep their own
ownership. The superseded ForceCodexCLI UA staging and disable-enforcement
branch have been removed; classification does not select an outbound identity.
The OpenAI HTTP passthrough switch, enabled or disabled, retains this same source
matrix and header contract. Authentication and identity remain gateway-managed;
protocol handling, safety filtering, audit, billing and concurrency remain in
effect. The switch does not change the WebSocket mode.
The existing CI identity check guards the HTTP/WS finalizers, OAuth and detached
manifest scopes and the shared override/SDK filter, rejects the retired override
functions and header-presence inference, and compares this exact
default block with the compiled declarations. Behavioral regressions exercise
source selection, HTTP passthrough on/off for OAuth and API keys, WS headers,
compatible presets and same-owner retry/refresh stability alongside those checks.
The five-entry forwarding matrix includes Responses, Messages, Chat Completions,
image generation and standalone search. The Chat Completions entry retains its
snapshot across both handler retries and automatic Responses-to-Chat fallback.
Shared HTTP/TLS transports preserve the selected identity on every destination.
Grok CLI rejection responses are returned without an automatic public API replay.

## Managed residency

`x-openai-internal-codex-residency` follows the official managed-residency
header. The only source is the global setting `codex_residency` (`off` by
default, or `us`). There is no account-level value. When the setting is `us`,
Codex-protocol inference (HTTP and WebSocket), Codex models discovery (OAuth and
API Key with the Codex preset), token refresh, token revoke,
and the chatgpt.com backend-api auxiliary surface (accounts check, WHAM usage
and credits, and the settings call) send `us`. Authorization-code exchange and
device-code start/poll stay on the raw client and do not send it. Inbound
headers and generic account header overrides cannot set or clear it.

Models cache keys include the final residency declaration; changing `off`/`us`
cannot reuse a catalog fetched under the other policy. Other API-key identity
presets do not acquire Codex residency through the models adapter.

## Codex source compatibility boundaries

The current protocol review baseline is the pinned Codex commit in
[Official agent outbound baselines](../OFFICIAL_AGENT_BASELINES.md). Earlier
commit links in this document record evidence for individual features rather
than a different current baseline. Identity-shape alignment does not mean every
client feature, body, OS fingerprint or transport is identical.

- HTTP-to-WS converts the Responses Lite header into the per-generation
  `client_metadata.ws_request_header_x_openai_internal_codex_responses_lite`
  string `"true"`, preserving other metadata. A non-object metadata shape that
  cannot be augmented remains on HTTP. Existing native WS metadata is retained.
- OAuth Responses retains `stream_options.reasoning_summary_delivery`, including
  official `sequential_cutoff`. Only the incompatible Chat `include_usage` option
  is removed (and its object when otherwise empty); unknown sibling options are
  preserved. The gateway does not enable concurrent reasoning summaries itself.
- Streaming API-key Responses explicitly sends `Accept: text/event-stream` in
  normal and passthrough paths; compact remains unary `application/json`.
- Guardian roles are bounded: `reviewer` is forwarded only to `codex-auto-review`
  with Codex backend OAuth-like credentials; `classifier` requires the Guardian
  subagent declaration and those credentials. API-key failover, compact, unknown
  roles, duplicate values and unrelated reviewer models do not inherit the role.
  These context headers neither authorize requests nor bypass either audit engine.
- Native root and subagent requests retain their explicit session separately
  from `prompt_cache_key`. Omitted cache fields remain omitted. Execution IDs
  follow owner and tenant/sharing scope; ordinary logical cache keys retain
  their separate tenant scope across failover. HTTP and WS follow the same rule.
- OAuth-like HTTP Responses uses Zstd level 3 at the final HTTP/plugin transport
  boundary, after JSON adaptation. The encoded body has a replayable `GetBody`
  and matching length/encoding declarations. This covers probes and HTTP bridges
  that use the shared outbound preparation. API keys, WS frames, unary endpoints
  and signed Agent Identity requests are not compressed by this policy. A
  conflicting pre-existing Content-Encoding fails before transmission.

Official source `770ed6dcea40` advertises `remote_compaction_v2` unconditionally
from `core/src/session/mod.rs::build_model_client_beta_features_header`,
independently of authentication. Native Codex OAuth, setup-token and API-key
Responses, compact and WebSocket handshakes therefore always include that token
in `x-codex-beta-features`. Existing other feature tokens are preserved, and an
already present v2 token is not appended again. A partial client declaration is
not an opt-out. The trusted credential-owner identity snapshot determines this
policy across retries and failover; a compatible supplier using another preset
does not receive a synthesized session capability header. An explicitly native
v2 compaction request retains its required v2 declaration for any supplier.
The same rule covers Responses account/adaptive probes, image-tool Responses
bridges, and JSON/SSE channel-monitor Responses probes. This policy applies to
Responses-family builders only; direct images, standalone search, models,
Chat Completions, token exchange/refresh and WHAM requests do not acquire it.

Intentional Plus differences remain: credential-owner identity precedence,
tenant namespaces and fingerprint settings, account-scoped WS pooling/cooldown,
API-key catalog Lite restrictions, and configured body adapters. Ordinary
Responses platform attestation is not implemented; Live requires a real platform
provider. The auxiliary `/memories/trace_summarize` endpoint is not exposed.
Neither capability is implied by matching UA/Originator/Version.

## User-Agent suffix

An administrator User-Agent may end with the official ` ({suffix})` group, for
example `codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color (mcp: server-a)`.
Pairing, validation, and manual version updates accept that form and keep the
suffix unchanged. The gateway does not generate a suffix.

## October 28 protocol compatibility

The reviewed source and release are pinned in
[Official agent baselines](../OFFICIAL_AGENT_BASELINES.md#october-28-codex-alignment).

**Capacity errors.** Native HTTP `503` with exact
`error.code = policy_capacity_unavailable` and SSE/WS error events with that
exact code are request-scoped capacity failures. The gateway keeps the code
across JSON, SSE, native WS and HTTP-to-WS conversion, and carries only bounded
`Retry-After` / `Retry-After-Ms` advice from embedded error headers. Synthesized
WS errors include top-level `status: 503` so the official client can classify
them. Plus does not penalize credentials, retry that account or scan the pool
for these failures. Partial output is not replayed. The same code in a native
HTTP 401/403 does not suppress ordinary authentication/access handling; a
policy-like type/message alone does not select this error class.

**Government API keys.** Native OpenAI API keys with the case-sensitive official
prefix `sk-proj-fedramp-` or `sk-svcacct-fedramp-` default to
`https://gov.api.openai.com/v1` for inference and standard model lists. Their
Codex manifest default is `https://gov.chatgpt.com/backend-api/codex/models`.
An explicit account base URL retains precedence, including an explicitly chosen
commercial URL. `X-OpenAI-Fedramp: true` derives from the acquired key's issuance
prefix; ordinary keys remove stale/injected declarations. OAuth and setup-token
accounts use the credential owner's government-workspace claim. Inbound headers and generic
header overrides cannot select the region or substitute another credential.
These routing hints do not validate a token or grant model entitlement.

**Named context outputs.** Official additional context can be a
`function_call_output` with `name`, optional `namespace`, `output` and an absent
or null `call_id`. This named context requires no preceding function call and
cannot establish a continuation anchor. Ordinary tool outputs still require
their paired context. The canonical audit extractor processes recognized output
content and siblings before account selection or side effects, with the same
unknown/incomplete-content pass-through contract for both audit engines.
See [content coverage](../SECURITY_AUDIT_CONTENT_COVERAGE.md).

**Workspace imports.** Official `auth.json` workspace switching may retain an
old ID Token while `tokens.account_id` and the access token name the newly
selected workspace. Import selects the access token's account, plan and
government flag only when the selected account matches and both tokens name
the same known user. Conflicting workspace/user declarations fail that item
before create/update. The original ID/refresh token is retained, and an
existing row for a different workspace is not overwritten by a shared refresh
token. JWT metadata parsing is not signature validation; provider authentication
continues to validate the transmitted bearer token.
Account-owned refresh keeps that selected account and known user, rejects
conflicting returned principals before enrichment or persistence, and uses
matching access-token metadata when an ID Token still describes the old
workspace. Returned ID/access principals must also agree when the stored user
is unknown. An explicit false government flag clears the previous true value;
the existing conditional refresh persistence remains mandatory.

**Terra catalog.** The official `gpt-6.1-terra` descriptor supplies a 500,000
default context window, 872,000 maximum context window, text/image input,
`low` through `max` plus client-side `ultra`, and the priority tier. The gateway
retains the descriptor and supports configured aliases; disabled reasoning
(`none` / `minimal`) is rejected on compatibility paths. Native API-key catalogs
retain the standard Responses boundary with Responses Lite disabled. Catalog
visibility does not assert account availability. The v0.2.19 Plus release adds
Terra's independently verified [API pricing](https://developers.openai.com/api/docs/pricing)
as a missing-catalog fallback, preserving configured prices and frozen billing.
The pinned client descriptor remains distinct from the current API model limits.

## Standalone search

`/v1/alpha/search` has its own request builder and protocol-header policy. The
reviewed official Codex source at commit
`c4017a87aacc7558002b7cb510025e967c1d765e`
uses [`SearchClient`](https://github.com/openai/codex/blob/c4017a87aacc7558002b7cb510025e967c1d765e/codex-rs/codex-api/src/endpoint/search.rs)
to POST JSON to `alpha/search` through the provider/auth session. Its
[`search_request_headers`](https://github.com/openai/codex/blob/c4017a87aacc7558002b7cb510025e967c1d765e/codex-rs/ext/web-search/src/tool.rs)
adds turn metadata and an optional thread originator. In this gateway, inbound
thread originator never replaces the credential owner's selected identity.
The official `chatgpt_cca` fixture is a thread originator, not a mandatory
constant for the search endpoint. The shared client provides UA/originator,
the provider may provide Version, and authentication supplies bearer/account
headers; the search tool's extra headers must be reviewed with those layers.

| Account/protocol | Upstream path | Identity declarations |
| --- | --- | --- |
| Normal OAuth | ChatGPT `/backend-api/codex/alpha/search` | Selected UA, Originator and Version |
| Native Codex API key | Configured base URL + `/alpha/search` | Selected UA, Originator and Version |
| API key with a compatible preset | Configured base URL + `/alpha/search` | Selected preset's own protocol header mapping |
| OAuth using PAT | Existing Plus adapter to ChatGPT `/backend-api/codex/responses` with `web_search` | Selected UA, Originator and Version; Responses transport headers |

Direct search uses JSON and strips `OpenAI-Beta`, `Session_ID`,
`Conversation_ID`, `X-Codex-Beta-Features`, `X-Codex-Turn-State` and the Responses
Lite header, including noncanonical duplicates from legacy custom headers.
`X-Codex-Turn-Metadata` is forwarded for both OAuth and API-key search, including
its opaque `mcp_request_meta` and nested `openai/search_context` search context.
OAuth turn/session IDs retain existing credential isolation; other metadata
fields remain intact. The PAT adapter also retains this metadata. These
protocol differences do not create a new identity source or a passthrough
exception. PAT's Responses adapter is existing Plus behavior; it is not a
claim that official Codex always implements PAT search this way. Whether a
custom API-key upstream implements standalone search remains that provider's
capability; choosing an identity preset does not add the endpoint.

## Maintaining the profile registry

The Codex identity controls now live in **System Settings → Outbound identity**.
The default identity and source-priority matrix above are unchanged. Explicit
non-Codex identity selections for compatible API-key/upstream accounts use the
separate [outbound identity](../OUTBOUND_IDENTITY.md) preset mechanism. This does
not change native Codex account selection, client classification or manual version updates.

Do not add a profile based only on a UI/product name or a community report.
For every registry addition or change:

1. Record a reviewed official OpenAI upstream source or release reference that
   shows the client wire identity.
2. Add regression fixtures for the transport `User-Agent`, independently
   reviewed `originator`, version, and known request-header evidence.
3. Verify every ingress path above, including WebSocket, Count Tokens, and
   Alpha Search ineligible-candidate cases.
4. Update this document and the Chinese/English admin descriptions if the
   security boundary changes.

The running gateway intentionally does not download profile rules dynamically;
an upstream change must be reviewed and released with the gateway.

## Operational interpretation

Use session identifiers, usage timing, rate/concurrency patterns, API-key
scope, and account controls for sharing investigations. Treat the client
profile decision as one signal that narrows supported access patterns, not as
conclusive evidence about the person or binary behind a request.

### Shared model discovery

Public API-key model discovery and administrator discovery apply the credential
owner’s final outbound identity. Supported OAuth shadows resolve to their parent;
API-key discovery uses the selected account’s own credentials.
Generic account header overrides run before this final identity step and cannot
replace its source. With the Codex preset, API-key `/v1/models` sends the selected UA, Originator
and Version; Codex/OAuth manifest Version and client_version follow the selected
identity. An OpenAI API-key account's custom base URL retains this native Codex
contract. Shared raw catalog cache keys include the resolved URL,
request headers and credentials, so an identity/credential change cannot reuse
a different identity's cached response. Detached manifest cache refreshes retain
the identity snapshot used to build their request and cache key.
The compiled default identity and source-priority matrix above remain the
normative declarations for every discovery and forwarding path.

### Official source parity and thread references

Reviewed against openai/codex `770ed6dcea40fedb83bb831f6d5e3760a933cc9a`
(`login/src/auth/default_client.rs`, `model-provider-info/src/lib.rs`,
`core/src/client.rs`, `core/src/responses_metadata.rs`, and
`codex-api/src/endpoint/responses.rs`). The exact compiled identity and
source-priority matrix above stay unchanged. OpenAI OAuth/setup-token/API-key
accounts retain the Codex client family. Migration 290 removes the retired
`openai:apikey` mapping and whole foreign account candidates while preserving
credential-owner `user_agent`, valid native selections, explicit version pins,
and compatible supplier/upstream settings. Runtime rejects foreign selections
independently of migration completion.

The retired administrator action
`POST /api/v1/admin/settings/openai-codex-version/sync` is no longer registered.
Settings responses and updates no longer expose the automatic-sync toggle,
synced version, check timestamp or synchronization error. The manual version,
effective version and `override` / `compiled` source fields remain available.
The local full profile and CI repository-policy lane run the identity guard and
its regression tests. They reject OAuth-only inference finalization and any
restoration of the retired automatic-version service or storage keys.

HTTP and WS both finish thread projection after credential-owner isolation and
fingerprint handling: `x-client-request-id` equals the final `thread-id`.
Account isolation uses the same thread namespace for request IDs and parent
references (`x-codex-parent-thread-id` and canonical/flat `parent_thread_id`),
so parent references preserve their relationship to an isolated parent thread.
Generic account overrides cannot replace the live parent-thread header.
Existing explicit session/full convergence policies retain their thread
convergence behavior. Protocol metadata does not change either audit engine's
canonical user-content extraction or side-effect ordering.


Exact provider `model_not_found` errors are terminal for the selected
credential/model, including HTTP 400/403/404 and statusless SSE/WS events.
They preserve their structured code after bounded cross-owner failover and do
not count as authentication, account health or quota failures. A generation-
scoped negative model cache prevents a late old-grant response from blocking
a replacement grant. Other models on that account remain schedulable.
