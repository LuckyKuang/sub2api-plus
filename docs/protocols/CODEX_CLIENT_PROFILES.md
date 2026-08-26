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
[`d06dc732`](https://github.com/openai/codex/blob/d06dc73290729d2bcb464b955a4cfd9992abc35d/codex-rs/login/src/auth/default_client.rs#L40-L165):
the default HTTP client sets both `originator` and `User-Agent`, and the
first-party predicate lists the fixed names plus the `Codex ` family. The
gateway mirrors that family rule instead of inventing a fixed
`codex_jetbrains` or `codex_app` alias that the upstream source does not list.

For a built-in profile, all of the following are required:

1. `User-Agent` and `originator` are both present.
2. The leading User-Agent client name exactly equals `originator`, including
   case.
3. The leading User-Agent version is valid semantic version text.
4. The originator is in the reviewed built-in registry, or is the exact
   case-sensitive upstream `Codex ` product family (for example, `Codex
   Desktop` or `Codex JetBrains`).
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
identity, a missing `originator`, or a case-rewritten `Codex ` family does not
pass this gate. The optional global minimum/maximum Codex version bounds apply
to built-in profiles. Policy versions use strict SemVer 2.0: they require a
complete `MAJOR.MINOR.PATCH` core without a `v` prefix or leading zeroes.
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

## Maintaining the profile registry

Do not add a profile based only on a UI/product name or a community report.
For every registry addition or change:

1. Record a reviewed official OpenAI upstream source or release reference that
   shows the client wire identity.
2. Add regression fixtures for the coherent `User-Agent`, `originator`,
   version, and known request-header evidence.
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
