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
precedence (`0.147.0-alpha.4` is lower than `0.147.0`, and build metadata does
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

## Outbound fingerprint convergence

Every credential-owning OpenAI OAuth account stores an explicit
`extra.codex_fingerprint_mode`. New accounts default to `device`; missing,
empty, null, or malformed legacy values are normalized to `device`. API-key,
setup-token, and credential-shadow accounts do not own this setting.

| Mode | Upstream-visible identity behavior |
| --- | --- |
| `off` | Do not mutate fingerprint-owned body or header carriers. Plus cache, security, session-sharing, and compact policy still apply. |
| `device` (default) | Converge only the installation identifier to an account-level stable value; preserve each client's session and thread boundaries. |
| `session` | Converge installation and session identifiers; derive a stable thread from the client-original session. |
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
| Ordinary Responses create turns and Chat/Messages Responses bridges | Configured mode | Plus prompt-cache/session identity |
| Native remote Compact v2 (`/responses` with `compaction_trigger`) | Configured mode | Plus prompt-cache/session identity |
| ChatGPT Codex OAuth legacy compact compatibility path | `off`, or installation-only for every other mode | Legacy compact session/cache/thread namespace |
| HTTP-to-WebSocket and direct Responses WebSocket `response.create` turns | Configured mode per turn | Plus WebSocket session/cache identity |
| Count-tokens, alpha-search, response retrieve/cancel subpaths, and other non-session endpoints | No fingerprint mutation | Endpoint policy |

Personal access token and Agent Identity accounts are OpenAI OAuth credential
owners and follow this endpoint matrix. API-key and setup-token accounts are
excluded. Credential shadows read the mode and stable installation source from
their credential-owning parent; the shadow never creates an independent
fingerprint identity.

Fingerprint body/header staging happens before the final cache and outbound
identity stages. The finalized Plus cache key owns both `session-id` aliases;
fingerprinting owns installation and thread/turn carriers. Malformed, null,
array, or scalar embedded `x-codex-turn-metadata` values are rebuilt as JSON
objects when that carrier is present, while valid unrelated fields are kept.
Missing carriers are not synthesized solely for embedded metadata.
WebSocket pool reuse compares every final stable handshake carrier in all four
modes, including client-owned values preserved by `off` and `device`.

Outbound User-Agent identity has one immutable source order: a valid
credential-owner `credentials.user_agent`, then a valid global
`openai_codex_user_agent`, then the compiled default. Originator and Version
are derived coherently from that selected client family. Version synchronization
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

### Mandatory source-priority matrix and exact default

| Credential-owner account candidate | Global candidate | Selected source |
| --- | --- | --- |
| Valid | Any | `account`; retain that account's client family and fingerprint |
| Empty or invalid | Valid | `global`; retain the configured global family and fingerprint |
| Empty or invalid | Empty or invalid | `compiled_default` |
| Credential shadow | Any | Resolve the credential-owning parent and apply the same matrix; a shadow does not supply a UA |
| Legacy candidate with compatibility disabled | Any | Treat that candidate as invalid and proceed to the next source |

Account and global candidates share the same configured-UA validation, including
the 512-character limit. Create, single update and bulk update reject invalid
Codex UA candidates. Bulk updates validate all OpenAI targets before any write;
omitting `credentials.user_agent` preserves its value, while null or a blank
string clears it through an explicit null in the JSONB update.

The exact compiled identity is:

```text
User-Agent: codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color
Originator: codex-tui
Version: 0.147.0
```

The version resolver runs after source selection: a valid administrator version
override, then an eligible synchronized stable version, then the compiled
version. This may change only the selected version declarations. The configured
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
One Live observer retains its scope across connection retries; a newly started
controller or observer resolves a fresh scope.

Final header application removes every case variant and duplicate of managed
identity headers, including foreign SDK declarations, before rendering the
selected identity. Native Codex OAuth/ChatGPT protocol requests send Originator
and Version with the selected UA. Native Codex Platform API-key requests omit
both Originator and Version, including `responses/compact`. Header presence is
determined by the endpoint protocol, never by an inbound or generic override
value. Explicit compatible presets retain their own protocol header mappings.
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
Shared HTTP/TLS transports cannot select a Grok identity based on a base URL or
discard the chosen identity on Grok's access-denied fallback.

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
| Native Codex API key | Configured base URL + `/alpha/search` | Selected UA; Originator and Version omitted under the Platform API-key contract |
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
not change native Codex account selection, client classification or version sync.

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
replace its source. With the Codex preset, API-key `/v1/models` omits OAuth-only
Originator; Codex/OAuth manifest Version and client_version follow the selected
identity. A compatible API-key account explicitly selecting another preset uses
that preset's wire headers and version for the Codex-style manifest's
client_version query. Shared raw catalog cache keys include the resolved URL,
request headers and credentials, so an identity/credential change cannot reuse
a different identity's cached response. Detached manifest cache refreshes retain
the identity snapshot used to build their request and cache key.
The compiled default identity and source-priority matrix above remain the
normative declarations for every discovery and forwarding path.
