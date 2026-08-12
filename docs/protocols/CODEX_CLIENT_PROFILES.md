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
