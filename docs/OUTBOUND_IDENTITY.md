# Outbound client identity

Manage client declarations in **System Settings → Outbound identity**, next to
Gateway. Gateway retains routing, timeouts, concurrency and protocol behavior.
Identity settings do not change account authentication, model access or routing.

The gateway resolves a trusted triple: User-Agent, client identifier and client
version. A preset renders only its defined wire declarations. Gemini and
Antigravity encode the identifier/version in User-Agent; they do not acquire
invented OpenAI `Originator` or `Version` headers. SDK versions and protocol
versions are distinct from the CLI version. MiniMax is an enumerated versionless
client family: its official client publishes the bare product token with no
version segment, so `Version` is intentionally empty for that preset only. The
exception is registered per preset and never makes the client version optional
for any other family.

Every declaration a preset renders has a class that decides who may supply its
value. A **derived** declaration is computed from the resolved triple (the
User-Agent, and any version companion such as Codex's `Version`, Grok's
`x-grok-client-version` or Kimi Code's `X-Msh-Version`). A **pinned**
declaration is a fixed provider token (Claude's `X-App` and `X-Stainless-*`,
Kimi Code's `X-Msh-Platform`, Antigravity's `X-Goog-Api-Client`). Neither class
accepts a configured value: a candidate that names one is rejected before
saving, so no configuration tier can desynchronize a companion declaration or
rewrite a client-family token. A **runtime** declaration describes the host the
official client runs on; the settings and an account selection may supply it.
Kimi Code is the only preset with runtime declarations today.

## Presets and default mappings

| Preset | Default accounts | Wire identity |
| --- | --- | --- |
| Codex | OpenAI OAuth/setup-token, OpenAI-compatible API keys, TypeSafe API keys | Existing Codex UA/Originator/Version rules, including endpoint-specific omissions |
| Claude Code | Anthropic OAuth/setup-token/API key, Claude on Bedrock or Vertex | `claude-cli` UA, `X-App: cli`, project-owned `X-Stainless-*` SDK/runtime declarations |
| Gemini CLI | Gemini OAuth/API key, Gemini on Vertex | `GeminiCLI` UA |
| Grok | Grok OAuth/API key | `grok-shell` UA, `x-grok-client-identifier`, `x-grok-client-version`, `x-grok-client-mode: headless` |
| Antigravity | Antigravity OAuth/upstream | `antigravity` UA; the two privacy endpoints also declare the pinned `X-Goog-Api-Client` SDK |
| DeepSeek | DeepSeek API-key accounts | `deepseek-harness/<version> (+https://github.com/deepseek-ai/deepseek-harness)` UA; identifier and version are encoded in the UA, so no `Originator`/`Version` headers |
| MiniMax | MiniMax API-key accounts | `MiniMaxAgent` UA; the official client declares no version segment, so there is no `Originator`/`Version` header and no client version |
| Kimi Code | Kimi / Moonshot API-key accounts | `kimi-code-cli/<version>` UA, `X-Msh-Platform: kimi_code_cli`, `X-Msh-Version: <version>` and the four `X-Msh-Device-*` runtime declarations; the official client declares no `Originator` and no standalone `Version` header |
| ZCode | Zhipu / GLM API-key and account-link OAuth accounts | `ZCode/<version>` UA; the official client declares no `Originator` and no standalone `Version` header, so only the User-Agent reaches the wire |

Native OAuth and setup-token accounts retain their native client family.
API-key, upstream, Bedrock and service-account accounts can explicitly select
any preset. OpenAI API-key/upstream accounts inherit existing Codex behavior
unless an administrator explicitly selects another preset through an account
selection or a type default. Selecting a preset changes declarations only;
it does not make the destination accept another authentication protocol.

TypeSafe API-key accounts are an API-key compatible supplier with no
provider-defined client family, version or identity header. They register the
same configurable preset mapping as the other API-key compatible platforms
(Codex by default) through `typesafe:apikey`, so preview, save, forwarding,
account connection tests, proxy use, retries and failover all resolve the
ordinary account/global/default chain. No TypeSafe CLI name, version or header
is invented, and the native System One client renders only the selected
snapshot's declarations.

Built-in declarations reuse existing pins in `internal/pkg/claude`,
`internal/pkg/geminicli`, `internal/pkg/xai`, `internal/pkg/antigravity`,
`internal/pkg/deepseek`, `internal/pkg/minimax`, `internal/pkg/kimi`,
`internal/pkg/zcode` and
`internal/service/openai_codex_identity.go`. This
feature does not upgrade those pins. The settings page displays the exact
current effective identity.

The exact compiled DeepSeek identity is
`deepseek-harness/0.2.0-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)`,
with identifier `deepseek-harness` and client version `0.2.0-rc.2`. The
parenthesized product comment belongs to the same User-Agent value the
published harness renders from its own package manifest; it is not a second
declaration. Only the User-Agent reaches the wire, so the preset adds no
`Originator`, `Version` or harness request-state header; per-request
`x-deepseek-harness-user-id`, `-session-id` and `-compact` values stay owned by
the protocol layer. `SUB2API_DEEPSEEK_HARNESS_VERSION` may select a supported
version (`0.2.0-rc.2` remains the accepted floor) while the product token, the
`(+url)` comment and the identifier stay fixed. DeepSeek API-key accounts
resolve this preset by default through `nativeOutboundPreset`, so the
`deepseek:apikey` type default is an explicit, operator-visible equivalent
rather than a prerequisite. Enabling it replaces Codex's
`Originator`/`Version` declarations on those accounts; an account selection or
type default can still opt a DeepSeek account back into Codex or any other
compatible preset.

The exact compiled ZCode identity is `ZCode/3.14.3`, with identifier `ZCode`
and client version `3.14.3`. The official ZCode client builds exactly one client
declaration, the versioned product token, in a single place
(`apps/zcode-cli/packages/bootstrap/src/model-config.ts`), and declares neither
an `Originator` nor a standalone version header, so only the User-Agent reaches
the wire. The same upstream helper also attaches ZCode platform attribution and
telemetry declarations (`HTTP-Referer`, `X-Title`, `X-Release-Channel`,
`X-Client-Language`,
`X-Client-Timezone`, `X-Platform`, `X-Os-Category`, `X-Os-Version`) and the
Vercel AI SDK appends its own runtime fingerprint (`ai-sdk/<pkg>/<version>`,
`runtime/node.js/<version>`) to the User-Agent. None of those are rendered here:
the platform headers describe the ZCode product rather than the selected preset,
and the SDK suffix would claim a JavaScript runtime this gateway does not run.
`SUB2API_ZCODE_VERSION` may select another version. The official client ships two
parallel version lines (the desktop/server product version and the standalone CLI
package version), so this preset deliberately enforces only the shared client
version shape and declares no monotonic floor: a floor drawn on one line would
reject the other line's legitimate official value. Zhipu API-key and
account-link OAuth accounts resolve this preset by default through
`nativeOutboundPreset`, so the `zhipu:apikey` type default is an explicit,
operator-visible equivalent rather than a prerequisite. OAuth accounts are
additionally pinned to their native family, so they cannot opt back into Codex;
an API-key account selection or type default still can.

The exact compiled MiniMax identity is the bare product token `MiniMaxAgent`,
with identifier `MiniMaxAgent` and no client version. The official MiniMax Code
client renders that single declaration for managed provider requests and never
puts its package version on the wire, so this preset is a registered versionless
family: `Version` stays empty, no `Originator` or `Version` header is rendered,
and only the User-Agent reaches the wire. The versionless exemption is an
explicit per-preset enumeration; an unlisted preset still requires a client
version, and a MiniMax candidate carrying a version — or any User-Agent other
than the exact token — is rejected before saving. MiniMax API-key accounts
resolve this preset by default through `nativeOutboundPreset`, so the
`minimax:apikey` type default is an explicit, operator-visible equivalent rather
than a prerequisite. Enabling it replaces Codex's `Originator`/`Version`
declarations on those accounts; an account selection or type default can still
opt a MiniMax account back into Codex or any other compatible preset. The
managed MiniMax session headers (`X-Mavis-Session-Id`, `-Agent-Id`,
`-Timezone-Offset`) are request state owned by the protocol layer, not identity
declarations.

The exact compiled Kimi Code identity is `kimi-code-cli/2.1.1`, with identifier
`kimi-code-cli`, client version `2.1.1` and platform declaration
`kimi_code_cli`. The official client renders one product token with dashes and
one platform token with underscores; the two are not interchangeable. Its engine
attaches the complete block — the User-Agent plus `X-Msh-Platform`,
`X-Msh-Version` and the four device declarations — only to the first-party
`kimi` provider, which is registered as a full-host-header provider for the
Chat Completions, Anthropic and Responses protocols; every other provider
receives only a User-Agent whose product token is rewritten to the declared
agent slug. The official client declares no `Originator` and no standalone
`Version` header, so neither reaches the wire.
`SUB2API_KIMI_CODE_VERSION` may select a supported version; `2.1.1` is the
accepted floor, because the CLI, the `kimi web` server and the native binaries
share one released version line. The VS Code extension is a separate product
(`kimi-code-vscode`, platform `kimi_code_vscode`) and can never select this
preset, so the floor cannot reject a legitimate value from a second official
line — the opposite of the ZCode situation above.

The device declarations are this deployment's own **runtime** values, not
compile-time pins: the official client resolves them from the machine it runs on
and persists the device id. Sub2API Plus resolves them once from the host this
deployment runs on, persists them under `runtime.kimi` in the outbound identity
settings, and lets an operator or an account override each one. The derivations
mirror the official client: the host name, the kernel release, a
`<os type> <os version> <arch>` model string using Node's architecture token
(`amd64` renders as `x64`, `386` as `ia32`, `mipsle` as `mipsel`, exactly as
`os.arch()` reports), and a uuid v4 device id. A fact the host does not expose
falls back to the official `unknown` substitution rather than an invented value.
Two consequences are deliberate and operator-visible:

- One deployment presents **one** device identity to upstream, while the
  official client presents one per end-user install. The declaration set is
  identical; the cardinality is not. Every runtime declaration is overridable
  per account for exactly this reason, so an operator can split deployments or
  accounts when an upstream rate-limits or risk-scores by device.
- Opening the settings page materializes any declaration this deployment has not
  generated yet, and startup does the same before the first request. The
  forwarding path only reads, and a persisted value never churns on its own.
  Materializing writes the same end state an administrator save would produce,
  so concurrent first starts converge on the last persisted value and every
  instance reads it once its settings cache expires.

The request-state companion (`X-Msh-Tool-Call-Id`) is raised by the official
tool-call path per request and is not an identity declaration, so it is neither
rendered by this preset nor blocked from account header overrides. The
identity-header allowlist is extended by the six declarations above; an inbound
caller or a generic header override can never select one.

The exact compiled Grok identity is `grok-shell/1.0.45 (<os>; <arch>)`, with
identifier `grok-shell`, client version `1.0.45`, and mode `headless`. Runtime
OS and architecture use the official spellings (`darwin` renders as `macos`
and Go's `amd64`, `386`, and `arm64` render as `x86_64`, `x86`, and `aarch64`).
`XAI_GROK_CLI_VERSION` may select a supported version while retaining that
family, platform fingerprint, identifier and mode; `1.0.41` remains the accepted
version floor and an existing valid account, global or environment pin keeps
selecting its own version instead of being forced to the compiled default. The
compiled default follows the frozen local grok-build source and does not track a
build-time network scrape or an ambient `GROK_VERSION`.

The exact compiled Antigravity identity is
`antigravity/2.9.1 windows/amd64`, with identifier `antigravity` and client
version `2.9.1` encoded in the UA. Only `setUserSettings` and `fetchUserInfo`
also send `X-Goog-Api-Client: gl-node/22.21.1`. This SDK declaration belongs to
those endpoints and remains fixed during client version updates. The base
preset preview describes the shared UA; these endpoint declarations are added
to a copy of the trusted request snapshot, without changing the parent snapshot.

| Antigravity operation | Identity source priority | Privacy SDK declaration |
| --- | --- | --- |
| Existing credential-owning account | Valid account candidate → configured global preset → valid environment / compiled default | Fixed `gl-node/22.21.1` for the two privacy endpoints |
| Pre-account OAuth exchange / refresh-token validation | Global native Antigravity preset → valid environment / compiled default | Same fixed declaration; no API-key type-default mapping |
| Privacy client without a settings resolver or account snapshot | Valid environment / compiled default | Same fixed declaration |

Invalid candidates fall through atomically. An explicit account version retains
its selected source and OS/architecture, and does not update the privacy SDK.

Claude reset-credit status queries and manual redemption use the same identity resolver and snapshot
as the owning Anthropic OAuth account. The snapshot is captured before token
acquisition, so refresh, OAuth profile, pre-claim usage, redemption POST and
fresh post-claim usage cannot select different versions. Retries and account
revalidation within one operation reuse that owner snapshot even if settings
change. These requests render only the Claude preset declarations, preserving
the fixed Stainless SDK/runtime fingerprint and `X-App: cli`.

| Claude reset-credit status / redemption | Identity source priority |
| --- | --- |
| Valid explicit account candidate | Account candidate → configured global Claude preset → valid environment / compiled default |
| Empty or invalid account candidate | Configured global Claude preset → valid environment / compiled default |
| Empty or invalid global candidate | Valid environment / compiled default |

Its compiled UA remains `claude-cli/2.1.258 (external, cli)`, with client
identifier `claude-cli` and version `2.1.258`. The identifier/version are
encoded in the UA; this path does not add Codex `Originator`/`Version` headers.
The reset-credit status and full redemption transport regressions check source priority, exact defaults,
companion headers, the token-acquisition snapshot and final-send replacement
of foreign SDK headers. This integration does not upgrade identity pins.

For Sonnet 5.5, gateway request construction filters
`fine-grained-tool-streaming-2025-05-14` from `anthropic-beta` when the final
request contains a stable computer/browser toolset. Filtering runs after trusted
identity application and the existing beta/header-override decision. It cannot
restore a beta dropped by policy or change the selected identity. The model
family check also handles Vertex model suffixes; other models retain their
existing beta behavior.

## Selection and persistence

For non-Codex identities, selection is:

1. A valid credential-owning account `credentials.outbound_identity` candidate.
2. The selected account-type default and its configured global preset.
3. The platform default preset, using an existing valid environment override
   when available, otherwise the compiled declaration.

An account selection containing only `preset` inherits that preset's current
global identity. Explicit `user_agent`/`version` fields form an account candidate;
omitted fields in that candidate use the preset's built-in declarations. Runtime
declarations are deployment state rather than account identity, so even a
complete candidate inherits them from the `runtime` map before its own `headers`
entries are laid on top. An
invalid candidate falls through as a whole. Invalid input through the management
API is rejected before saving. Empty or null account selection means inherit.
A versionless family rejects a candidate that supplies a client version, and it
accepts only its exact compiled User-Agent token, so no other candidate can claim
that family. The settings page exposes no version or User-Agent control for such
a family; a profile the management API persisted for it is preserved by unrelated
saves instead of being silently dropped.
Non-Codex User-Agent candidates containing the project brand token (case
insensitive) are invalid. Rejecting them during selection ensures the final
brand filter cannot remove an accepted UA while leaving companion declarations
behind. Previously stored invalid account/global candidates follow the same
atomic fallback chain; the transport never substitutes an SDK default for them.

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
covers User-Agent, client identifiers/versions, the Kimi Code `X-Msh-*`
declaration block and SDK declarations such as `X-Stainless-Package-Version`.
Matching is case-insensitive. Previously stored
identity overrides are ignored at runtime; ordinary overrides remain effective.
Move intended identity customization to the account/global identity controls and
remove identity entries from the generic override editor before saving it.
Channel-monitor and request-template `extra_headers` enforce the same managed
header registry at save time and ignore previously stored identity overrides at
runtime. Ordinary custom headers retain their existing behavior. Authentication,
request/session fields, and destination-owned Grok protocol declarations are
also reserved where their owning adapter must derive them from credentials,
request state, or the final target.

Other global settings live in the existing settings store under
`outbound_identity`; account selections use the existing credentials JSON.
No database schema migration or new YAML/environment binding is required.
Defaults are empty `profiles`, `defaults` and `runtime` maps. Existing Antigravity
`antigravity_user_agent_version` is imported into the editable Antigravity
profile before the unified configuration is first saved — either by an
administrator save or by the runtime-declaration materialization described
above, whichever happens first. After that save,
clearing the profile restores the default without reviving the old setting. Existing environment
defaults (`SUB2API_CLAUDE_CLI_VERSION`, `XAI_GROK_CLI_VERSION`,
`ANTIGRAVITY_USER_AGENT_VERSION`) remain below explicit identity configuration.

```json
{
  "profiles": { "claude": { "preset": "claude", "version": "2.9.1" } },
  "defaults": { "gemini:service_account": "gemini", "openai:apikey": "codex" },
  "runtime": { "kimi": { "X-Msh-Device-Name": "kimi-gateway-1" } }
}
```

`runtime` carries the persisted runtime declarations per preset and is the only
place a runtime value can be written globally. A preset name or header name that
does not declare a runtime value is rejected, an absent or cleared entry falls
back to the resolved built-in value, and the map is materialized automatically
for any declaration this deployment has not generated yet.

The version above is an illustrative administrator selection, not a recommended
or automatically discovered upstream release. Account type names are `oauth`,
`setup-token`, `apikey`, `upstream`, `bedrock`, and `service_account`.

Management API (administrator authentication required):

| Method and path | Behavior |
| --- | --- |
| `GET /api/v1/admin/settings/outbound-identity` | Saved profiles/defaults/runtime declarations, built-in presets, effective global identities and sources, and the declared header block of every preset with its class, editable flag, built-in and effective value |
| `PUT /api/v1/admin/settings/outbound-identity` | Validate and replace profiles/defaults/runtime declarations, then return the effective view |
| `POST /api/v1/admin/settings/outbound-identity/preview` | Resolve `{platform, type, selection, user_agent?}` without tokens, secrets or an upstream request; optional `user_agent` is the existing Codex account declaration |

An account candidate carries runtime values in the same `headers` map
(`credentials.outbound_identity.headers`), validated against the same declared
names. Account values take precedence over the `runtime` map, which takes
precedence over the resolved built-in value; an invalid stored account candidate
falls through to the global tier as a unit rather than failing the request.

The preview describes managed identity headers. Authentication, request IDs,
session fields, capabilities and endpoint protocol headers remain owned by
their existing adapters and are not included in this preview.

The settings page tracks unsaved identity edits across tabs. Saving from another
settings tab also submits those edits; visiting the identity tab without editing
does not rewrite its configuration. A failed identity save retains the edits and
reports an error. Edits made while a save or effective-identity refresh is in
flight remain pending for the next save.

## Outbound paths and invariants

For forwarding, resolve after ingress authentication, basic validation, audit
and account selection. Carry a snapshot for forwarding and retries; failover resolves the
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
and its account/subscription enrich requests retain one identity as well.
Authorization-code exchange and device-code start/poll omit identity headers.
Refresh and revoke send User-Agent and Originator only. The chatgpt.com
backend-api auxiliary surface (login enrich `/backend-api/wham/accounts/check`,
Plus-only subscription enrich, the settings PATCH behind `set-privacy`, WHAM
usage and credit APIs) uses the regular HTTP client without browser TLS
impersonation and sends the selected User-Agent while omitting
Originator/Version. Login no longer PATCHes ChatGPT training settings.

The OpenAI OAuth credential plane aligns with the official client's HTTP
behavior: the shared refresh/revoke/enrich/WHAM client retains a filtering
cookie jar that stores only the allowlisted ChatGPT Cloudflare infrastructure
cookies (`__cf_bm`, `__cflb`, `__cfruid`, `__cfseq`, `__cfwaitingroom`,
`_cfuvid`, `cf_clearance`, `cf_ob_info`, `cf_use_ob`, `cf_chl_*`) plus the
`__oailb` routing cookie, and only for chatgpt.com hosts; account, session and
auth cookies are never stored. The jar is pooled per proxy configuration, so
cookies never cross egress boundaries. Authorization-code exchange and
device-code start/poll keep the raw client without a jar, matching the
official raw client. Device-code sessions expire 15 minutes after creation,
matching the official device-code lifetime, and do not pre-bind an account.
Browser authorization sessions keep the longer session TTL and may still bind
an account for re-authorization. The custom-CA policy matches the official
`CODEX_CA_CERTIFICATE` / `SSL_CERT_FILE` pair: `CODEX_CA_CERTIFICATE` takes
precedence, empty values are treated as unset, a configured PEM bundle is
appended to the system roots (including OpenSSL-style `TRUSTED CERTIFICATE`
labels), and a misconfigured bundle fails client creation early with a precise
error instead of silently using system roots. Both names are Codex-specific, so
only OpenAI outbound clients consult them and a bad bundle cannot take down
other providers. This covers the credential plane (shared pool) and the official
auth surface (personal access token validation and agent task registration).
`CODEX_REFRESH_TOKEN_URL_OVERRIDE`
and `CODEX_REVOKE_TOKEN_URL_OVERRIDE` override the token/revoke endpoints at
startup; empty or invalid values fall back to the defaults with a warning log.
When no revoke override is set but a refresh override is, the revoke endpoint is
derived from it by rewriting the path to `/oauth/revoke`, matching the official
`derive_revoke_token_endpoint`. Revoke is bounded by the official 10s request
timeout instead of the 120s credential-plane timeout, so a stuck revoke cannot
block a logout or account deletion.

The official authentication surface (personal access token validation, agent
identity task registration, token refresh, revoke) sends the selected User-Agent
and Originator only. Plus does not add an independent `version` header there,
matching the official `create_default_auth_client` default headers; `version`
remains an inference-plane declaration only. The ChatGPT accounts check sends
`ChatGPT-Account-Id` when the poid is known, matching the official
backend-client header surface.

`x-openai-internal-codex-residency` is not an identity source. Its only source
is the global setting `codex_residency` (`off` default, `us` sends the value
`us`). Account credentials, inbound headers, and generic header overrides
cannot select it. When the setting is `us`, Codex-protocol inference HTTP/WS,
refresh, revoke, and chatgpt.com backend-api auxiliary calls send the header.
Authorization-code exchange and device-code start/poll do not. A configured
Codex User-Agent may also carry the official trailing ` ({suffix})` group.
That group is preserved through pairing and version synchronization and is
never generated by the gateway.

Pre-account Antigravity code exchange / refresh-token validation and Gemini
code exchange start an independent native OAuth scope before the first provider
request. Token exchange, user/project discovery, privacy set/verify and Google
One Drive tier discovery reuse that operation's base snapshot, even when global
settings change between calls. The next operation sees the new settings.
Inherited account identities and API-key type defaults do not select the native
OAuth family. Existing-account refreshes continue to use the credential owner's
account identity. Antigravity `loadCodeAssist` body `metadata.ideVersion` follows
the same selected Antigravity version as its UA.

Privacy SDK declarations are carried in the two requests' own snapshots so final
identity reapplication preserves them. `X-Goog-Api-Client` remains a managed
header: inbound headers and generic overrides cannot supply it. Other Antigravity
endpoints, Gemini/Drive requests and compatible non-Antigravity presets do not
acquire this privacy SDK declaration.

The integration covers inference/streaming, token counting, model discovery,
account tests, quota/usage probes, OAuth exchange and refresh, Grok Realtime
handshakes and probes, OpenAI-compatible WebSocket handshakes, and Gemini/Vertex
batch requests and result retrieval. Before an account exists, authorization
requests use the global native preset. Bedrock applies the selected identity
before SigV4 signing and reuses the same declarations at send time.
This includes the non-streaming Bedrock account connection test, for both IAM
credentials and bearer API keys. IAM signatures include the selected companion
declarations; the send-time finalizer must not introduce a new signed header.

Google One tier refresh resolves the credential-owning Gemini account before
calling Drive `about?fields=storageQuota`. Drive sends that snapshot on every
retry. A pre-account Google One OAuth exchange uses the global Gemini identity,
with the compiled Gemini CLI UA available when no settings resolver is wired.

Auxiliary services with independently configured credentials resolve a separate
operation snapshot. They never inherit a forwarding account's identity or its
nil-account Codex cache:

| Credential owner / path | Source and snapshot lifetime |
| --- | --- |
| Channel-monitor endpoint API key | `<provider>:apikey` type default, configured global preset, then existing environment/compiled fallback; one snapshot for the origin HEAD and all concurrent model POSTs in one check |
| Prompt Audit endpoint token | `openai:apikey` type default and its global/default chain; one snapshot per endpoint credential across an evaluation/job's chunks and failover returns; a `/models` probe and its inference fallback share a snapshot |
| Content Moderation endpoint API key | `openai:apikey` type default and its global/default chain; same-key retries share a snapshot, key rotation or endpoint failover resolves the new owner; administrative key tests start independent operations |

These credentials have no account-level identity field. Their type default can
select another compatible preset. Native OpenAI API-key Codex requests preserve
the existing Originator/Version header omissions; other presets render their
defined companions. Fresh monitor checks, audit evaluations/jobs, probes and
key tests observe current settings. Supplier identity resolution does not select
a forwarding account or move inference ahead of the ingress audit boundary.

Model discovery includes both standard model lists and the Codex-style manifest
requested from a compatible API-key upstream. Explicit account or type-default
preset selections govern the manifest's final headers and `client_version`
query together. A versionless family declares no client version, so the manifest
omits the `client_version` query instead of sending an empty declaration; the
selected identity still owns the headers. Its cache key includes the final URL
and headers, and detached cache refreshes carry the same resolved identity
snapshot. Native Codex source precedence and endpoint-specific header omissions
remain unchanged.

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
existing Codex feature. A versionless family has no version-only update: it
rejects a client-version candidate and can only be changed through its preset
selection.

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
legacy source cases. It also covers the seven compatible non-Codex presets.
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
Every final send, including redirects, adds `X-XAI-Token-Auth: xai-grok-cli`
only for `cli-chat-proxy.grok.com`; sampling and media-mutation paths on that
host also add `x-authenticateresponse: authenticate-response`. Both
declarations are removed from other destinations. The narrowly matched Grok
access-denied compatibility
fallback retains the selected UA and companion headers when changing hosts and
removes proxy authentication declarations. Repository tests capture both actual
transport methods and the fallback with inherited/explicit Codex, Grok and
Claude identities. Every account-owned Grok HTTP path also receives the Grok
transport profile at the shared final preparation boundary unless the owning
operation explicitly selected a more specialized profile.

Grok inference and media-mutation builders own the sampler declarations
separately from the identity triple. They issue a fresh `x-grok-req-id`, retain
one random process-level `x-grok-agent-id`, declare the final model, attach the
OAuth credential owner's `sub` when available, and reuse the tenant-isolated
conversation snapshot for conversation/session headers and the official UUIDv5
conversation-group derivation. Generic overrides cannot set these fields. The
gateway omits sampler and response-authentication declarations on model,
billing and media-status lookups, and omits optional turn, retry, deployment,
and tracing declarations when it does not possess the corresponding
authoritative value. One constructed sampler request keeps its request
association snapshot across transport retries, redirects and the compatibility
fallback; a newly constructed logical sampler call or a resubmit renders a new
association instead.

Grok sampler `Accept` is a request-owned operation declaration rather than an
identity field: the JSON operation declares `application/json`, and a request
whose final body streams upstream declares `text/event-stream` even when the
downstream client is aggregated. Account header overrides and inbound headers
cannot change it.

Negotiated request encoding is owned by the gateway's compression layer and
never by an identity candidate. When it is enabled
(`gateway.grok.grok_request_compression_enabled`, environment
`GATEWAY_GROK_REQUEST_COMPRESSION_ENABLED`, default true) the gateway may send
level-3 zstd only after the exact `cli-chat-proxy.grok.com` target's
`/v1/settings` advertises `zstd` for the normalized scheme/host/effective
port/base path, credential owner and proxy configuration. The capability probe
uses the same-owner snapshot, proxy, URL validation and audit ordering as the
sampler send and never acquires sampler declarations. `Content-Encoding: zstd`
appears only with the bytes it describes; a destination change (the `api.x.ai`
compatibility fallback, a cross-origin redirect) rebuilds a plain body from the
final JSON and drops the declaration. Audit, payload hashing, inflight
estimation, cache/session keys and billing keep the uncompressed semantics.
Generic header overrides cannot supply `Content-Encoding`.

Agent-only native differences stay explicitly scoped rather than being
approximated with surface declarations: the native media-tool user agent, the
Rustls/HTTP2 transport parameters, doom-loop recovery headers, enterprise
deployment authorization, and turn/resubmit/tracing declarations. The gateway
keeps the selected account snapshot and its existing transport fingerprint, and
emits no header without the matching authoritative state or recovery behavior.
See [Grok / xAI](providers/GROK.md#agent-only-differences-and-follow-up-scope).

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
