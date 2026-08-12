## Context

`codex_cli_only` is stored with the OpenAI OAuth account and is intended to
limit access to Codex clients. Existing code runs a detector for two HTTP
forwarding methods only. It treats official UA, official Originator, configured
whitelist, and generic App Server mode as alternatives, then accepts any
`x-codex-*` header as the default fingerprint. `gateway.force_codex_cli`
short-circuits this access control.

OpenAI's public Codex implementation uses a coherent Originator and User-Agent
identity, but the same harness can power CLI, app, and IDE product surfaces.
No header presented to this gateway proves that the client binary is official;
the boundary is therefore a strict request-profile policy, not attestation.

## Design

### Built-in client profiles

The OpenAI package owns a closed, versioned built-in profile registry. Each
profile declares one or more canonical Originators and derives the matching UA
from the same exact client identifier. The classifier:

1. requires both headers;
2. extracts the leading UA client identifier and version;
3. requires the raw leading identifier to equal the raw Originator;
4. matches the pair to a built-in profile using case-sensitive canonical
   identity rules; and
5. requires a valid full semantic Codex version.

The special upstream `Codex ` client family remains an explicit case-sensitive
profile family, with the exact same raw name required on both headers. It is
not normalized to lowercase and no `contains` or trailing-UA recovery is used
for authorization.

Built-in profiles intentionally describe the wire identity (for example the
shared CLI/App Server identity), not a guessed desktop or IDE UI name. Existing
explicit non-official entries remain an administrator-managed compatibility
path. Compatibility entries never receive an "official profile" classification
and cannot skip core policy checks.

### Historical profile compatibility

The global `codex_legacy_client_profile_compatibility_enabled` setting defaults
to `false`. When true, the classifier may additionally return one distinct
`codex-legacy-compatible` profile for exactly `codex_app`, `codex_exec`,
`codex_sdk_ts`, or `codex_vscode_copilot`. The registry is exact and
case-sensitive; it is not a prefix rule. A legacy profile requires the same
raw leading UA/Originator equality, complete semantic version, known evidence,
and version bounds as a built-in profile.

The outgoing account/global UA validator and resolver read this switch in the
same cached settings snapshot. When it is false, a legacy account candidate
falls through to global/default and a legacy global candidate falls through to
the compiled default. OAuth adapters receive an already-resolved identity
tuple; a legacy tuple is accepted only when its supplied exact Originator
matches, preventing legacy acceptance through the historical user-agent-only
adapter methods.

Inbound profile versions and configured profile bounds use strict SemVer:
`MAJOR.MINOR.PATCH` is required, with no `v` prefix or leading zeroes. Normal
prerelease ordering applies (`0.147.0-alpha.4 < 0.147.0`) and build metadata
does not affect precedence. This parser is intentionally separate from the
more permissive outbound version normalization used only to preserve a chosen
outbound identity while synchronizing its version declaration.

### Evidence and policy

Built-in and explicit compatibility profiles must have known non-empty Codex
evidence. The accepted names are a closed set maintained from the public Codex
source, rather than an arbitrary header prefix. Empty, missing, or unknown
header names fail this evidence check. The classifier is still documented as
spoofable request evidence, not an authenticity proof.

The global generic App Server allow switch, per-account App Server override,
and per-entry fingerprint bypass are removed. `ForceCodexCLI` is not consulted
by the inbound detector. It may still control existing outbound formatting and
feature behaviour, but cannot change an account access decision.

### Enforcement topology

`OpenAIGatewayService` remains the single policy owner. It exposes a
side-effect-free account policy evaluation for the handler and enforces it
defensively in all service forwarding methods.

HTTP Responses, Chat Completions, Messages, Messages Count Tokens, and Alpha
Search apply the detector before upstream work. Messages and Count Tokens use
the same policy even though their protocol is Anthropic-compatible. Count
Tokens and Alpha Search release and exclude a policy-ineligible selected
candidate, then continue selection without recording an account-health
failure. Responses WebSocket evaluates after its first `response.create` frame
is received and before a selected account's credentials are acquired. During
WebSocket selection, a policy-ineligible account is released/excluded and
selection continues; a policy rejection never affects account health. The
WebSocket proxy repeats the check before an upstream connection so direct
callers cannot bypass the handler.

OAuth session-sharing policy is a separate account boundary and applies only
to `PlatformOpenAI` OAuth accounts. API-key accounts retain their established
API-key-scoped session isolation and local `previous_response_id` binding even
if historic data contains the OAuth policy field; management writes reject that
field for API-key accounts. A group-local `previous_response_id` binding for an
account without enabled OAuth sharing is authoritative before the OAuth shared
namespace is considered, so a shared-namespace collision cannot deny,
redirect, or otherwise affect an API-key continuation.

### Configuration and UI

The old `codex_cli_only_allow_app_server_clients`,
`codex_cli_only_allow_app_server`, and generic fingerprint signal controls are
removed from the admin DTO, settings service, account editor, and audit list.
The existing explicit whitelist is retained as the narrow compatibility path,
with its two-factor match. The global legacy-profile switch is displayed beside
the OpenAI Codex outbound UA field and is audited when changed. The label and explanatory text say "Codex official
client profiles" and state its non-attestation limitation. English and Chinese
locale keys remain aligned.

## Non-goals

- This change does not implement device enrollment, mTLS, signed request
  proofs, or remote binary attestation.
- This change does not claim to detect account sharing.
- This change does not dynamically download upstream profile rules or identify
  a product UI solely from a product name.
