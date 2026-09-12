# Outbound client identity

Manage client declarations in **System Settings → Outbound identity**, next to
Gateway. Gateway retains routing, timeouts, concurrency and protocol behavior.
Identity settings do not change account authentication, model access or routing.

The gateway resolves a trusted triple: User-Agent, client identifier and client
version. A preset renders only its defined wire declarations. Gemini and
Antigravity encode the identifier/version in User-Agent; they do not acquire
invented OpenAI `Originator` or `Version` headers. SDK versions and protocol
versions are distinct from the CLI version.

## Presets and default mappings

| Preset | Default accounts | Wire identity |
| --- | --- | --- |
| Codex | OpenAI OAuth/setup-token, OpenAI-compatible API keys, Chinese compatible providers | Existing Codex UA/Originator/Version rules, including endpoint-specific omissions |
| Claude Code | Anthropic OAuth/setup-token/API key, Claude on Bedrock or Vertex | `claude-cli` UA, `X-App: cli`, project-owned `X-Stainless-*` SDK/runtime declarations |
| Gemini CLI | Gemini OAuth/API key, Gemini on Vertex | `GeminiCLI` UA |
| Grok | Grok OAuth/API key | `xai-grok-workspace` UA, `x-grok-client-identifier`, `x-grok-client-version` |
| Antigravity | Antigravity OAuth/upstream | `antigravity` UA |

Native OAuth and setup-token accounts retain their native client family.
API-key, upstream, Bedrock and service-account accounts can explicitly select
any preset. OpenAI API-key/upstream accounts inherit existing Codex behavior
unless an administrator explicitly selects another preset through an account
selection or a type default. Selecting a preset changes declarations only;
it does not make the destination accept another authentication protocol.

Built-in declarations reuse existing pins in `internal/pkg/claude`,
`internal/pkg/geminicli`, `internal/pkg/xai`, `internal/pkg/antigravity` and
`internal/service/openai_codex_identity.go`. This feature does not upgrade
those pins. The settings page displays the exact current effective identity.

## Selection and persistence

For non-Codex identities, selection is:

1. A valid credential-owning account `credentials.outbound_identity` candidate.
2. The selected account-type default and its configured global preset.
3. The platform default preset, using an existing valid environment override
   when available, otherwise the compiled declaration.

An account selection containing only `preset` inherits that preset's current
global identity. Explicit `user_agent`/`version` fields form an account candidate;
omitted fields in that candidate use the preset's built-in declarations. An
invalid candidate falls through as a whole. Invalid input through the management
API is rejected before saving. Empty or null account selection means inherit.

Account creation, single-account updates and bulk updates use the same identity
validation. Bulk updates validate the selection against every target account's
platform and authentication type before writing any account. An incompatible
native family or invalid version rejects the whole batch. Omitting
`credentials.outbound_identity` from a bulk update preserves existing selections;
an explicit null or empty object clears them and restores inheritance. The bulk
credentials merge retains an explicit null to replace a previously stored value.
The existing Codex `credentials.user_agent` field is also validated in bulk;
blank/null values explicitly clear it and omitted values are preserved.

Codex continues to use its established source chain: valid credential-owner
`credentials.user_agent` → valid `openai_codex_user_agent` → compiled default.
Its existing version selection and automatic synchronization remain in place.
The existing Codex account editor is retained. The new global `profiles` map
cannot replace Codex configuration. See the exact default and source matrix in
[Codex client profiles](protocols/CODEX_CLIENT_PROFILES.md).

Generic `header_overrides` cannot select or modify a client identity. Saves reject
managed identity names with `INVALID_HEADER_OVERRIDE` (HTTP 400), including empty
values and disabled override configurations. The shared managed-header registry
covers User-Agent, client identifiers/versions and SDK declarations such as
`X-Stainless-Package-Version`. Matching is case-insensitive. Previously stored
identity overrides are ignored at runtime; ordinary overrides remain effective.
Move intended identity customization to the account/global identity controls and
remove identity entries from the generic override editor before saving it.

Other global settings live in the existing settings store under
`outbound_identity`; account selections use the existing credentials JSON.
No database schema migration or new YAML/environment binding is required.
Defaults are empty `profiles` and `defaults` maps. Existing Antigravity
`antigravity_user_agent_version` is imported into the editable Antigravity
profile before the unified configuration is first saved. After that save,
clearing the profile restores the default without reviving the old setting. Existing environment
defaults (`SUB2API_CLAUDE_CLI_VERSION`, `XAI_GROK_CLI_VERSION`,
`ANTIGRAVITY_USER_AGENT_VERSION`) remain below explicit identity configuration.

```json
{
  "profiles": { "claude": { "preset": "claude", "version": "2.9.1" } },
  "defaults": { "gemini:service_account": "gemini", "openai:apikey": "codex" }
}
```

The version above is an illustrative administrator selection, not a recommended
or automatically discovered upstream release. Account type names are `oauth`,
`setup-token`, `apikey`, `upstream`, `bedrock`, and `service_account`.

Management API (administrator authentication required):

| Method and path | Behavior |
| --- | --- |
| `GET /api/v1/admin/settings/outbound-identity` | Saved profiles/defaults, built-in presets, effective global identities and sources |
| `PUT /api/v1/admin/settings/outbound-identity` | Validate and replace profiles/defaults, then return the effective view |
| `POST /api/v1/admin/settings/outbound-identity/preview` | Resolve `{platform, type, selection, user_agent?}` without tokens, secrets or an upstream request; optional `user_agent` is the existing Codex account declaration |

The preview describes managed identity headers. Authentication, request IDs,
session fields, capabilities and endpoint protocol headers remain owned by
their existing adapters and are not included in this preview.

The settings page tracks unsaved identity edits across tabs. Saving from another
settings tab also submits those edits; visiting the identity tab without editing
does not rewrite its configuration. A failed identity save retains the edits and
reports an error. Edits made while a save or effective-identity refresh is in
flight remain pending for the next save.

## Outbound paths and invariants

Resolve after ingress authentication, basic validation, audit and account
selection. Carry a snapshot for forwarding and retries; failover resolves the
new credential owner. Apply reserved identity declarations after generic
header overrides and again at the final HTTP transport boundary. Header
matching is case-insensitive, including duplicate noncanonical Go map keys.

The OpenAI **HTTP passthrough** switch changes HTTP forwarding behavior only.
Both states retain gateway-owned authentication and the same identity source
chain, with required protocol handling, safety filtering, audit, billing and
concurrency controls. It does not select a WebSocket mode. Native Codex OAuth /
ChatGPT protocol requests emit the coherent UA, Originator and Version. Native
Codex Platform API-key requests emit UA and omit Originator and Version, including
`responses/compact`; old overrides or inbound Version headers cannot enable
those declarations. Explicit compatible presets render their own header mapping.
The native Codex finalizer removes foreign SDK identity headers as well as stale
or duplicate core declarations before rendering the selected identity.

The request scope retains each credential owner's first selected identity, even
when a handler re-enters forwarding for a retry. This includes the choice to use
the native Codex resolver: changing a type default cannot switch an in-flight
request between Codex and another preset. Failover uses the other credential
owner's snapshot; a fresh request observes new settings. OAuth exchange/refresh
and its account, subscription and privacy requests retain one identity as well.

The integration covers inference/streaming, token counting, model discovery,
account tests, quota/usage probes, OAuth exchange and refresh, Grok Realtime
handshakes and probes, OpenAI-compatible WebSocket handshakes, and Gemini/Vertex
batch requests and result retrieval. Before an account exists, authorization
requests use the global native preset. Bedrock applies the selected identity
before SigV4 signing and reuses the same declarations at send time.

Model discovery includes both standard model lists and the Codex-style manifest
requested from a compatible API-key upstream. Explicit account or type-default
preset selections govern the manifest's final headers and `client_version`
query together. Its cache key includes the final URL and headers, and detached
cache refreshes carry the same resolved identity snapshot. Native Codex source
precedence and endpoint-specific header omissions remain unchanged.

Claude fingerprint caching now preserves the account's device identifier while
refreshing client declarations from project configuration. Cached or inbound
UA/SDK headers no longer select an identity. Existing session masking behavior
is preserved. Claude billing-header `cc_version` follows the same selected UA.
Token counting snapshots the identity before token acquisition and request
construction, so signature retries keep the same UA and billing-header version
even if global settings change. The next independent request sees the update.
The security-audit extractor and its pass-through semantics are unchanged.

Version-only updates replace the selected client version declaration and its
paired version header. They preserve client family, Originator, OS,
architecture, terminal and SDK fingerprint. Other presets currently expose
manual version settings; automatic release synchronization remains the
existing Codex feature.

## Mandatory maintenance contract

The repository-wide `Outbound Identity` and `Codex Identity` rules in
[AGENTS.md](../AGENTS.md) are mandatory for new features, maintenance, dependency
and SDK upgrades, version synchronization, and upstream merges. Every existing
or new account type and every provider-bound request must participate. A
compatibility option, upstream implementation, or SDK upgrade cannot grant an
exception. Preserve the existing Codex resolver and source chain.

- **One trusted identity:** use the documented account/global/default source
  chain. Validate and select a complete candidate; do not mix caller, cache,
  account and global identity fields. Preset-only inheritance and missing
  candidate fields follow the selection rules above. Before an account exists,
  use the global native preset. New types require an explicit default mapping
  and supported preset policy; they must not silently inherit an SDK identity.
- **One snapshot per credential owner:** HTTP and WebSocket adapters, retries,
  probes, discovery, usage, OAuth and batch paths must use the resolved
  snapshot. Resolve again when failover changes the credential owner. Settings
  refreshes during a request must not create conflicting declarations. Apply
  identity before signing and preserve any signed declarations at send time.
- **No alternate identity source:** inbound headers, generic header overrides,
  cached fingerprints, request classification, SDK defaults and protocol
  adapters must not replace any managed declaration. Compare names
  case-insensitively, including duplicate map keys. Authorization, request/session
  IDs and protocol/capability headers keep their existing ownership; the triple
  must not bypass authentication or the ingress security-audit boundary.
- **Coherent wire declarations:** render only headers defined by the selected
  preset's protocol mapping. Keep UA, client identifier, paired version headers
  and body version declarations coherent. Gemini and Antigravity must not gain
  invented Codex headers. A version-only update may change only the selected
  client version declarations, preserving the selected source, family,
  identifier, OS, architecture, terminal and SDK fingerprint. A deliberate SDK
  fingerprint change requires a separately described change and its regression
  evidence; it must not be bundled silently into client version synchronization.
- **Upgrade and merge acceptance:** review incoming provider/SDK changes for new
  outbound paths, default headers and signing behavior. Route new paths through
  trusted identity resolution before accepting the change. Update this source
  matrix, preset mappings, exact-default assertions and affected protocol
  documentation together with the implementation. Do not remove assertions,
  relax source precedence, or restore caller-derived identities to make an
  upstream merge pass.

Before merging any affected change, the following regression evidence is
required in the platform validation container described in
[CONTRIBUTING.md](../CONTRIBUTING.md):

| Contract | Required evidence |
| --- | --- |
| Sources and defaults | Account/global/environment/compiled selection, invalid and empty fallthrough, preset inheritance, every affected account type and exact built-in identity; unchanged Codex priority matrix |
| Header and body integrity | Caller/override/cache/SDK contamination, mixed-case and duplicate headers, coherent companion/body versions, and preserved auth/protocol/session fields |
| Outbound coverage | Captured final HTTP requests and WS handshakes for affected inference/adapters, retries, discovery, account tests, usage, OAuth/refresh and batch paths; no bypass for a new type or endpoint |
| Snapshot and signing | Same-owner retries and settings changes retain the snapshot, failover selects the new owner, and Bedrock signed declarations remain stable |
| Version maintenance | Selected version changes without changes to source, client family, identifier, OS, architecture, terminal or SDK fingerprint |
| Settings changes | Account/global save and effective preview agree with resolution, persistence/default behavior and aligned English/Chinese locales |

Existing coverage lives in `backend/internal/service/outbound_identity_test.go`,
`backend/internal/pkg/outboundidentity/identity_test.go`,
`backend/internal/repository/outbound_identity_test.go`, the existing Codex and
provider outbound tests, and the account/settings component tests. Extend the
owning tests for new paths rather than treating this list as a fixed coverage
limit. Backend identity changes require the complete existing Codex identity
regressions as well as the affected non-Codex cases. A failing identity
regression prevents merge; an exception must not be inferred from account type,
compatibility mode, or an upstream release.

`openai_outbound_contract_test.go` captures real HTTP sends, rejected-field
retries and WS handshake headers across OAuth/API-key accounts, both HTTP
passthrough states, forced Codex classification and the account/global/default/
legacy source cases. It also covers the four compatible non-Codex presets.
The header-override suites cover management rejection and filtering of legacy
stored declarations, including SDK headers and case variants. These guards are
required alongside the existing source-priority and exact-default assertions.

`openai_endpoint_identity_contract_test.go` additionally captures final sends
from `/v1/responses`, `/v1/messages`, `/v1/chat/completions`,
`/v1/images/generations` and `/v1/alpha/search`. Its matrix covers OAuth and
API-key accounts, both passthrough states, account/global/default selection,
compatible presets, same-owner retries during version updates, and fresh
requests. Chat Completions' automatic Responses-to-Chat fallback retains the
same snapshot. Alpha Search separately exercises normal OAuth, API keys and
the existing PAT Responses web-search adapter; see
[Codex client profiles](protocols/CODEX_CLIENT_PROFILES.md#standalone-search).

The shared HTTP and TLS transports may add Grok's destination-specific
authentication hint, but may not select an identity from the destination host.
The Grok access-denied fallback retains the selected UA and companion headers
when changing hosts. Repository tests capture both actual transport methods and
the fallback with inherited/explicit Codex, Grok and Claude identities.

The account editor uses the backend's passthrough precedence: a boolean
`extra.openai_passthrough` wins, including `false`; only when it is absent or
not boolean does `extra.openai_oauth_passthrough` provide the legacy fallback.
Saving removes the legacy field. Merely opening and saving an account must not
change its effective passthrough state.

The `compress-cli` validator protects the root identity clauses against removal
or weakening. Its tests already run in the repository-policy CI job and the
local `submit-pr` checks. Those policy checks protect the written contract;
the outbound behavior tests above remain required to verify implementation.

## Upstream references

- [AWS SigV4 canonical request rules](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html): signing and client declarations are separate concerns; signed fields must be stable.
- [Anthropic Bedrock SDK](https://github.com/anthropics/anthropic-sdk-typescript/blob/main/packages/bedrock-sdk/src/client.ts): provider authentication is implemented independently from SDK client configuration.
- [Gemini CLI content generator](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/core/contentGenerator.ts): client declarations are configured alongside the selected authentication path.

These references explain adapter boundaries. They do not imply that a generic
compatible supplier requires or recognizes every preset declaration.
