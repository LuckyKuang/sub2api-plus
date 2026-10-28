# Outbound client identity

Manage client declarations in **System Settings → Outbound identity**, next to
Gateway. Gateway retains routing, timeouts, concurrency and protocol behavior.
Identity settings do not change account authentication, model access or routing.

The gateway resolves a trusted triple: User-Agent, client identifier and client
version. A preset renders only its defined wire declarations. Gemini and
Antigravity encode the identifier/version in User-Agent; they do not acquire
invented OpenAI `Originator` or `Version` headers. SDK versions and protocol
versions are distinct from the CLI version. MiniMax and Step-Code are enumerated versionless
client families: their official inference clients publish no product version
segment, so `Version` is intentionally empty for those presets only. The
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
Kimi Code and ZCode persist runtime declarations.

Official v0.2.18 synchronization preserves this source and fingerprint contract.
Its TUI-to-CLI normalization is intentionally not applied to valid configured
Codex identities. Queued/metadata overload retries keep the resolved credential
owner's snapshot; account failover resolves the new owner. No client-family,
versionless-family or compiled fingerprint change accompanies the Go/security
dependency upgrade.

## Default client environment and privacy

Native OpenAI API-key records own separate installation and execution
projections. Installation/device identity uses the credential-owning account
record and remains stable when its API key is replaced. Execution session,
thread, turn, parent and window mappings also include an irreversible digest of
the actual API key, so replacement on the same record cannot inherit the old
key's execution state. Unverified organization/project values never select
either namespace. Different records remain isolated, and credential shadows
use their actual credential-owning parent. HTTP and WS share these projections and align
`x-client-request-id` with the final `thread-id`. This does not enable OAuth
fingerprint convergence modes for API keys. Ordinary logical cache keys use
the same tenant derivation across OAuth and API keys, independently of the
credential-owner metadata. Existing API-key explicit keys may see one cache
miss when upgrading to this shared derivation; subsequent owner changes keep
the key stable. Explicit OAuth sharing retains its authorized scope.
The Codex source-priority matrix below is unchanged. Its manually reviewed
compiled version is `0.165.0`; the client family and environment stay fixed.
The manual October 28 upgrade retains the account/global/default source matrix
and administrator version pins. Government endpoint selection for recognized
native OpenAI API-key issuance prefixes and `X-OpenAI-Fedramp` comes from the
acquired credential owner, never from the inbound client or a generic header
override. Explicit base URLs remain authoritative. See
[Codex protocol compatibility](protocols/CODEX_CLIENT_PROFILES.md#october-28-protocol-compatibility).

All built-in OS declarations describe one fixed **Ubuntu 24.04 x86_64** client,
independent of the gateway/container host. Device name is `ubuntu`. The baseline
is Ubuntu Noble GA `linux 6.8.0-31.31` (amd64): `os.release()` is
`6.8.0-31-generic`; `os.version()` is
`#31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024`.
These two strings describe the same Ubuntu kernel build. This is an advertised
client environment, not a claim that the server runs that kernel. Product and
SDK versions do not update this baseline.

| Preset | Exact default OS/architecture declarations |
| --- | --- |
| Codex | `codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color` |
| Claude | `X-Stainless-OS: Linux`, `X-Stainless-Arch: x64` |
| Gemini | `GeminiCLI/0.1.5 (Linux; x64)` |
| Grok | `grok-shell/1.0.45 (linux; x86_64)` |
| Antigravity | `antigravity/hub/2.23.0 linux/amd64` |
| DeepSeek | Product UA has no OS; OAuth exchange body uses `device_model: linux-x64`, `os_version: linux 6.8.0-31-generic` |
| Kimi | `X-Msh-Device-Name: ubuntu`, `X-Msh-Device-Model: Linux 6.8.0-31-generic x64`, `X-Msh-Os-Version: 6.8.0-31-generic`; inference SDK uses `Linux` / `x64` |
| MiniMax OAuth / API Key | SDK uses `Linux` / `x64`; UA has no OS segment |
| ZCode | `X-Platform: linux-x64`, `X-Os-Category: linux`, inference kernel release and control-plane kernel version as above |

Only provider-defined fields are rendered. Fixed defaults deliberately replace
the official clients' ambient host detection, preventing container hostnames and
kernel details from leaking. Kimi retains its persisted UUID. Client names,
version companions, authentication and protocol-specific SDK formats stay intact.

Source priority remains valid account → global preset/type → environment/compiled
default; Codex retains its separate immutable source contract. Kimi and ZCode
OS, kernel and architecture fields are pinned and reject overrides at every
configuration tier. Settings and account editors show this environment read-only.
Migration `276_pin_domestic_identity_environment.sql` removes the superseded OS
keys from global runtime/profile values and account selections; no runtime
compatibility or repair path remains. Language, timezone, UUID and other settings
are preserved. Migration `275_neutral_kimi_device_name.sql` replaces known Kimi
gateway names with `ubuntu`; new branded names are rejected. Kimi device name
and UUID remain configurable globally and per account.

ZCode language and timezone use searchable dropdowns in settings and account
editors. Defaults are `en-US` and `UTC`; explicit canonical BCP 47 language tags
and loadable IANA timezones are accepted. `Local`, unknown zones and malformed
language tags are rejected before saving; invalid stored candidates fall through
atomically. Previously saved valid choices outside the common browser list remain
visible. Blank account values inherit global settings. SDK `X-Stainless-Lang: js`
is a programming language and is never a locale selector.

MiniMax has a non-header `timezone` setting on each global/account preset
selection (`minimax` and `minimax_apikey`), default `UTC`. It is serialized into
the identity snapshot and OAuth account selection. The protocol layer computes
`X-Mavis-Timezone-Offset` in seconds at request creation, respecting DST; the same
owner's retries keep the captured offset and failover resolves the new owner.
No synthetic timezone header is added to OAuth or other providers.

DeepSeek stores `language` (`zh-CN` or `en-US`, default `zh-CN`) and `timezone`
(default `UTC`) in global/account selections. Its official platform API supports
only wire locales `zh_CN` and `en_US`; both the `auth_init` body and
`X-Client-Locale` use the selected language. `X-Client-Timezone-Offset` is the
selected zone's UTC offset in **seconds east of UTC**, including DST. The login
session captures it once before authorization and serializes it with the identity;
callback exchange reuses the snapshot even after global settings or DST change.
`X-Client-Bundle-Id` is intentionally an empty header, shown as an official empty
value. `X-Client-Platform: web` identifies the web authorization flow, independently
of the pinned Ubuntu environment. These control headers never leak into inference.

MiniMax OAuth and API Key appear in one settings group with two named modes.
They keep separate profiles/timezones and their official distinct wire identities;
combining their UA values would violate the official managed/BYOK behavior.

## Presets and default mappings

| Preset | Default accounts | Wire identity |
| --- | --- | --- |
| Codex | OpenAI OAuth/setup-token, OpenAI-compatible API keys, TypeSafe API keys | Existing Codex UA/Originator/Version rules, including endpoint-specific omissions |
| Claude Code | Anthropic OAuth/setup-token/API key, Claude on Bedrock or Vertex | `claude-cli` UA, `X-App: cli`, project-owned `X-Stainless-*` SDK/runtime declarations |
| Gemini CLI | Gemini OAuth/API key, Gemini on Vertex | `GeminiCLI` UA |
| Grok | Grok OAuth/API key | `grok-shell` UA, `x-grok-client-identifier`, `x-grok-client-version`, `x-grok-client-mode: headless` only on CLI proxy/control destinations |
| Antigravity | Antigravity OAuth/upstream | `antigravity` UA; the two privacy endpoints also declare the pinned `X-Goog-Api-Client` SDK |
| DeepSeek / Harness | DeepSeek OAuth/API-key accounts | `deepseek-harness/<version> (+https://github.com/deepseek-ai/deepseek-harness)` UA; identifier and version are encoded in the UA, so no `Originator`/`Version` headers |
| MiniMax Code | MiniMax OAuth accounts | `MiniMaxAgent` plus pinned Anthropic SDK declarations; no product version or `Originator`/`Version` headers |
| MiniMax Code API Key | MiniMax API-key accounts | `Anthropic/JS 0.91.1` plus matching pinned SDK declarations; version is mandatory |
| Kimi Code | Kimi / Moonshot OAuth/API-key accounts | `kimi-code-cli/<version>` UA, `X-Msh-Platform: kimi_code_cli`, `X-Msh-Version: <version>` and four device declarations (two runtime, two pinned); the official client declares no `Originator` and no standalone `Version` header |
| GLM / ZCode | Zhipu / GLM API-key and account-link OAuth accounts | `ZCode/<version>`, paired `X-ZCode-App-Version`, product attribution, two runtime and three pinned environment declarations listed below |

Native OAuth/setup-token accounts retain their native client family. DeepSeek,
Kimi, MiniMax, GLM and StepFun API-key accounts also retain their official family;
MiniMax API keys use `minimax_apikey`, whereas OAuth uses `minimax`. Bedrock and
Vertex Claude retain `claude`; Vertex Gemini retains `gemini`. Valid account
parameters can override global declarations within that family. A custom base
URL or proxy does not change this rule.

Cline and Command Code use the existing compatible-supplier Codex preset by
default. Their API-key forwarding, model discovery, model protocol catalogs,
credential probes, balances and quota batches resolve the credential-owning
account once per operation. Background catalog refresh carries that snapshot
after caller cancellation. Catalogs and failed-refresh backoff are isolated by
account, URL, proxy and a hash of the captured credential, tenant and identity
headers; rotation starts a new cache entry. Generic headers and incoming client
declarations cannot replace the trusted identity. No new native client family or
versionless exception is introduced.

Kimi/Zhipu/MiniMax quota probes and monitor caches separate credential/proxy
generations. One-time Kimi 401 recovery preserves the trusted identity snapshot
while recording the refreshed credential owner that actually answered the query.
Quota windows persist with atomic exact-owner/proxy comparison and scheduler
invalidation; threshold/reset decisions reject old or unbound generations. The
stored generation is a digest of ownership inputs, not another outbound identity
declaration. See [quota ownership and legacy data](providers/DOMESTIC_OAUTH.md).

Credential quarantine, cooldowns, OAuth rotation and quota/billing persistence
carry the acquired full owner snapshot into their conditional repository ports.
A configured proxy requires its observed protocol, host, port, username,
password and status; an ID without its hydrated proxy cannot authorize a write.
The mutation locks the matching live proxy row with `FOR SHARE` until the account
update and scheduler outbox commit. Editing that same proxy row therefore either
follows the mutation or causes its stale comparison to fail, including when the
mutation waits on a separate account update. Runtime generation digests include
proxy status so repairing status alone also isolates previous local blocks.

Every proxy edit or deletion commits scheduler invalidation for all undeleted
accounts bound to that proxy in the same transaction, including accounts without
quota observations and accounts paused by an administrator. Outbox replay
refreshes both full account payloads and scheduler metadata. Edits to egress
timezone, country and fallback settings also refresh the embedded proxy; only
probe-identity changes clear the existing quota bindings. Expiration without
fallback refreshes the bound accounts' proxy status as well. Soft deletion keeps
the configured proxy ID, so a missing proxy cannot silently become direct egress.

An account's saved proxy binding is mandatory on every physical send, including
inference, HTTP/SSE/WS, retries, OAuth recovery, discovery, connection tests,
usage, quota and balance queries. Missing, mismatched or invalid loaded proxy
configuration stops the operation before sending. A proxy timeout, refused
connection, authentication failure or other transport failure returns the
failure; it never retries through the machine's direct network, `NO_PROXY`, an
environment proxy or a different proxy on the same account. An acquired attempt
retains its complete credential/identity/proxy snapshot. A new operation uses
the committed binding, and recovery revalidates it before replay. Explicit
administrator unbinding and the separately saved expiry policy can change the
binding; a network error cannot invoke those mutations. Tests must count actual
destination connections as well as configured-proxy requests so a failed TLS
handshake cannot conceal unauthorized local egress.

The same binding applies to credential refresh/revocation, PAT validation,
AgentIdentity task registration, manual ChatGPT privacy changes, Grok observed
models/compression discovery, and detached adaptive protocol-catalog refreshes.
These operations acquire the complete owner before reading a token, route or
warm cache; a second proxy lookup cannot pair old credentials with a new route.
Account metadata produced by a response uses the captured-owner conditional
write, with no unconditional fallback. URL-only asynchronous image results
also carry their final inference owner's route into CDN downloads; object
storage uploads retain their independently configured administrative route.

Kimi and DeepSeek PAYG balance probes freeze the complete credential owner before
HTTP preparation and isolate concurrent probes by credential/proxy generation.
Balance snapshots and threshold decisions use that same owner in conditional
state writes; a late balance response cannot change a replaced grant's extra or
scheduling. Balance metadata, pause and recovery additionally compare the exact
cooldown deadline and reason observed before the probe, including an absent
deadline, so a late healthy response cannot clear a newer reactive balance marker
or replace a newer failure from the same grant. Missing conditional storage
does not fall back to row-ID writes. Owner evidence remains internal to the
result and is never included in its admin JSON response.

Provider Files upload/delete use the same trusted credential-owner identity
resolver and strict proxy as inference. The selected account is frozen before
multipart forwarding; no inbound SDK/auth header or generic override selects its
client family, credential or route. Mapping publication compares that complete
account/proxy snapshot under database locks. Local references resolve after the
common security audit and constrain every selection/retry to their persistent
owner. Send preparation rechecks the final acquired owner; it cannot attach a
different owner's credential to an already rewritten remote file ID. Background
cleanup dispatches only known remote IDs whose recorded request snapshot still
matches, with no direct fallback or substitute account. File operations do not
create inference-token billing records.

DeepSeek first-batch Files use the reviewed Harness Anthropic family: default
`/anthropic/v1/files`, `anthropic-beta: files-api-2025-04-14`, raw account
`X-Dsh-Auth-Token` or API-key `X-Api-Key`. OAuth and API keys retain `deepseek`
identity precedence and the complete versioned Harness triple. The default
endpoint follows `config.ts` → `adapter.ts` → `messages-api.ts`/`files-api.ts`,
as linked in [the baseline index](OFFICIAL_AGENT_BASELINES.md). MiniMax first-batch
Files support only global API-key Chat `video_understanding` at
`https://api.minimax.io/v1/files/upload`, Bearer API-key auth and the existing
trusted `minimax_apikey` triple. No new identity preset or versionless exception
is introduced. The managed agent's separate upload gateway does not authorize
direct OAuth, CN or BYOK Anthropic Files. Limits, scope epochs, unsupported
bridges/lifecycle operations and native-reference compatibility are defined in
[FILES.md](providers/FILES.md).

MiniMax OAuth quota resolves the personal workspace group and calls the fixed
regional account/quota origins with the same grant, proxy and trusted `minimax`
snapshot. Its recognized five-hour/weekly windows feed existing scheduling
thresholds and the native OAuth finite-exhaustion gate until reset (including
when early-pause thresholds are disabled); unknown query failures do not
quarantine a grant. Credits remain independent of token-plan windows.

Only these eleven keys permit another client family, through a global default
or account selection:

| Platform | Configurable account types | Automatic preset |
| --- | --- | --- |
| OpenAI compatible upstream | `upstream` | `codex` |
| Anthropic | `apikey`, `upstream` | `claude` |
| Gemini | `apikey`, `upstream` | `gemini` |
| Grok | `upstream` | `grok` |
| Antigravity | `upstream` | `antigravity` |
| TypeSafe / Jev | `apikey` | `codex` |
| OpenCode Go | `apikey` | `codex` |
| Cline | `apikey` | `codex` |
| Command Code | `apikey` | `codex` |

Anthropic/Gemini API-key entries also serve compatible endpoints, so they
retain advanced overrides without guessing endpoint type from its hostname.
System Settings presents these mappings in a collapsed advanced section.
The API's `account_policies` declares the native preset, allowed presets and
whether default mapping is permitted. Both editors consume this backend policy.
Management save, preview, account creation/import/update/bulk update and runtime
resolution enforce the same family rule. Retired mapping keys are rejected even
when their value is the native preset. Selecting a compatible identity changes
only declarations, never authentication, protocol or model routing.

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
baseline upgrades follow [the official agent source index](OFFICIAL_AGENT_BASELINES.md). The settings page displays the exact
current effective identity.

The exact compiled DeepSeek identity is
`deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)`,
with identifier `deepseek-harness` and client version `0.2.1-rc.3`. The
parenthesized product comment belongs to the same User-Agent value the
published harness renders from its own package manifest; it is not a second
declaration. Only the User-Agent reaches the wire, so the preset adds no
`Originator`, `Version` or harness request-state header; per-request
`x-deepseek-harness-user-id`, `-session-id` and `-compact` values stay owned by
the protocol layer. The official inbound Harness session header is a routing
signal within DeepSeek groups. Its outbound session is `session-<UUIDv4>`, randomly allocated and persisted for
the authenticated tenant, logical conversation and credential owner. Subsequent
turns and same-owner retries retain it; failover or reauthorization changes the
owner namespace.
Caller session values never become trusted client declarations.
`SUB2API_DEEPSEEK_HARNESS_VERSION` may select a supported
version (`0.2.0-rc.2` remains the accepted floor) while the product token, the
`(+url)` comment and the identifier stay fixed. DeepSeek API-key accounts
resolve this preset by default through `nativeOutboundPreset`, so the
`deepseek:apikey` mapping is no longer configurable. The global DeepSeek profile
and valid same-family account overrides remain available; a foreign persisted
candidate falls through atomically to the native profile/default.

The exact compiled ZCode identity is `ZCode/3.16.2`, with identifier `ZCode`
and client version `3.16.2`. The desktop host's declaration block comes from
`apps/zcode-cli/packages/bootstrap/src/model-config.ts` and
`runtime-platform-headers.ts`:

| Declaration | Default | Class |
| --- | --- | --- |
| `User-Agent` | `ZCode/3.16.2` | Derived |
| `X-ZCode-App-Version` | `3.16.2`, always paired with UA | Derived |
| `HTTP-Referer` | `https://zcode.z.ai` | Pinned |
| `X-Title` | `Z Code@electron` | Pinned |
| `X-Release-Channel` | `production` | Pinned |
| `X-ZCode-Agent` | `glm` | Pinned |
| `X-Client-Language` | `en-US` | Runtime |
| `X-Client-Timezone` | `UTC` | Runtime |
| `X-Platform` | `linux-x64` | Pinned |
| `X-Os-Category` | `linux` | Pinned |
| `X-Os-Version` | `6.8.0-31-generic` | Pinned |

Only language and timezone are configurable, persisted under `runtime.zcode`
and overridable globally or per account. The OS declarations use the fixed
environment above. No declaration reads host locale, timezone or kernel files.
Version-only changes retain these facts and all pinned
product declarations. No standalone `Originator` or `Version` is rendered.
`SUB2API_ZCODE_VERSION` selects a version without changing the desktop family;
both upstream version lines remain accepted. The bootstrap declarations above
are supplemented by the pinned per-protocol SDK wire profiles below. Product
version changes preserve those profiles.

Account-link, project-token issuance and off-peak ticket clients derive their
control-plane headers from the same snapshot, including standalone clients.
Following `packages/services/src/providers/sourceHeaders.ts`, control calls omit
`X-ZCode-Agent` and inference SDK suffixes; their `X-Os-Version` is the pinned
Ubuntu Node `os.version()` equivalent, not inference's `os.release()`. It is a read-only baseline declaration carried
in `control_headers` and serialized with login sessions. An absent configured telemetry
`X-Device-Mid` resolves to a persisted random UUIDv4 before the first request.
The control header and corresponding body declaration share that device ID;
inference requests retain their own protocol declarations. Start, poll, code exchange and token
issuance retain one identity even across Redis instances/settings changes;
account creation pins its product and configurable runtime declarations. Deferred off-peak settlement retains the acquiring credential
owner's identity alongside its authentication, even after settings change. Gateway transports reapply the trusted declarations at send
time; stale SDK/inbound/generic overrides cannot rewrite them. Both API-key and OAuth accounts retain the ZCode family.

Linked Start entitlement uses an independent
`GET https://zcode.z.ai/api/v1/zcode-plan/billing/balance?app_version=<trusted-version>`.
The query version, UA and `X-ZCode-App-Version` come from one trusted ZCode
snapshot; authentication is the linked `zcode_jwt_token` as Bearer. It uses
control-plane declarations, disables redirects and follows the same frozen
owner's strict proxy. Inference URL/auth overrides cannot change its target or
replace its JWT with a manual API key, project PAT or another account's token.
Start observations remain separate from personal/team project model discovery
and off-peak quota/tickets. The reviewed
`services/src/model-provider/{zaiStartPlanBilling,codingPlanProviderAvailability}.ts`
and `shared/src/{zcodeEndpoint,official-glm-model-id}.ts` paths are linked at
[the fixed baseline](OFFICIAL_AGENT_BASELINES.md); business-login failure needs
reauthorization rather than project-PAT renewal.

Linked personal/team model discovery uses the fixed estate's coding `/models`
endpoint and the final owner-scoped PAT resolver. Linked inference, probes and
discovery disable redirects before credentials are attached. Every explicit 401
invalidates only its attempted PAT, including final/unreplayable attempts; a
transient concurrent refresh cannot return material invalidated by that 401.
Linked team model and ticket requests derive organization/project declarations
from that PAT's resolved scope; personal plans omit team headers. Generic header
override eligibility cannot remove or replace these protocol-owned declarations.

MiniMax has two distinct defaults:

| Account type | Preset | UA / identifier / version |
| --- | --- | --- |
| OAuth | `minimax` | `MiniMaxAgent` / `MiniMaxAgent` / empty |
| API Key | `minimax_apikey` | `Anthropic/JS 0.91.1` / `Anthropic` / `0.91.1` |

`minimax` is an enumerated versionless exception (Step-Code is the other; see
its section below). The official managed MiniMax resolver overrides the SDK UA; its BYOK resolver does not. The latter uses the
Anthropic SDK locked at `0.91.1` in `third_party/pi-mono/packages/ai` and
`pnpm-lock.yaml`. Both inference profiles preserve these exact declarations:
`X-Stainless-Lang: js`, `X-Stainless-Package-Version: 0.91.1`,
`X-Stainless-OS: Linux`, `X-Stainless-Arch: x64`,
`X-Stainless-Runtime: node`, `X-Stainless-Runtime-Version: v22.19.0`.
This is an explicitly pinned supported official Node host fingerprint, independent
of the gateway's Go runtime. The API-key SDK UA/version are read-only; updating
the SDK requires a source/wire regression review, not a product-version edit.
MiniMax API keys retain `minimax_apikey`; OAuth retains `minimax`. Neither type
can switch families. Valid account parameter overrides retain precedence.

Messages requests for both native presets carry protocol-owned
`X-Mavis-Session-Id` (gateway-generated), `X-Mavis-Agent-Id: main` (the official
default agent) and `X-Mavis-Timezone-Offset` (selected timezone UTC offset in seconds). A
request scope retains these across retries. The inbound MiniMax session header
is a routing signal within MiniMax groups. Its outbound value is `mvs_<32 lowercase UUIDv4 hex digits>`, randomly allocated
and persisted for the authenticated tenant, conversation and credential owner.
Failover or reauthorization resolves a different owner namespace.
They are not configurable identity declarations. Official OAuth uses fetch
without a product UA. The gateway deliberately keeps its trusted `MiniMaxAgent`
UA on authorization/refresh, without the inference SDK block, because every
provider-bound call requires a trusted client declaration. This intentional
policy difference is displayed under OAuth/account-service headers.

The official
[`matrix-account-client.ts`](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/tui/src/account/matrix-account-client.ts)
uses `MiniMaxCode` for Matrix identity/workspace/membership requests, including
`/v1/api/user/info`. The gateway renders this exact operation-specific profile
without the inference SDK block. Token-plan remains uses the trusted managed
profile because the official fetch has no explicit product UA; this is an
explicit identity-policy extension, not a selectable client family. OAuth quota selects the personal workspace's
`op_group_id` for `X-Group-Id`; its fixed quota origins are
`https://www.minimaxi.com` (China) and `https://platform.minimax.io` (global).
Identity/account lookup uses the corresponding `https://agent.minimaxi.com` or
`https://agent.minimax.io` origin. Matrix query declarations retain the official
`app_id=3001`, `biz_id=3`, `version_code=22201` (a Matrix application code,
independent of the versionless product UA), and the pinned Linux fingerprint.
Timezone comes from the same trusted account snapshot. Attribution is calculated
over the validated URL and actual body before dispatch; send-time identity
rendering preserves it. Generic headers cannot select the workspace,
change these destinations, supply authentication or overwrite identity.

The exact compiled Kimi Code identity is `kimi-code-cli/2.3.1`, with identifier
`kimi-code-cli`, client version `2.3.1` and platform declaration
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

The official client derives device declarations from its host. The gateway uses
the fixed Ubuntu environment above and a persistent UUID instead. Device name
and UUID are stored under `runtime.kimi` and support global/account overrides. The default
hostname, kernel and architecture never depend on the deployment host.
Two consequences are deliberate and operator-visible:

- One deployment presents **one** device identity to upstream, while the
  official client presents one per end-user install. The declaration set is
  identical; the cardinality is not. Device name and UUID are overridable
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

### Official-agent source evidence and source priority

All seven platforms and both credential types are owned by their corresponding
[official agent](OFFICIAL_AGENT_BASELINES.md). That index records public GitHub
URLs, reviewed commits/versions and code entrypoints. DeepSeek follows
**deepseek-harness** directly. OAuth (`x-dsh-auth-token`) and API Key
(`x-api-key`) share its Messages adapter and attribution. Kimi shares its
first-party host declarations. MiniMax retains its official managed/BYOK wire
difference. Grok OAuth and API Key are both native grok-build families; only
Grok `upstream` accounts can select a compatible family.

The 2026-10-25 protocol review also inspected the same current official source
revisions for native OAuth control operations, Step-Code Passport sessions and
ZCode Project Access Token authentication. The reviewed revisions, selected
agent versions and any explicitly versionless inference families are recorded
in the baseline index. A protocol review never changes identity defaults or
SDK pins implicitly; selected updates require their own source and wire evidence.
Session IDs, authentication and per-request metadata remain owned by the protocol.

| Operation / candidate | Resolution |
| --- | --- |
| OAuth or API-key credential owner | Valid account candidate → global preset/type default → valid environment / compiled default |
| Empty or invalid candidate, including invalid companion/runtime headers | Fall through atomically to the next tier |
| Native client family | Domestic OAuth/API Key and cloud accounts retain their native family; only the eleven compatible keys above can select another family |
| Pre-account native authorization | Global native preset → valid environment / compiled default; ignore inherited owners and API-key mappings |
| Retry or nested same-owner request | Reuse the selected snapshot, including all runtime values |
| Failover to another credential owner | Resolve that owner's candidate and current defaults |

The managed `minimax` and `stepfun` presets are the versionless exceptions;
`minimax_apikey` is versioned. Settings and account header maps
use each declaration's registered spelling, reject case-insensitive duplicates,
and return deep copies; malformed stored header candidates cannot partially win.
Global profile runtime headers override the deployment runtime map; explicit
account runtime values override that resolved global state. Settings header
blocks show the actual resolved declarations for all four families. This identity
contract does not create a new login protocol or change authentication headers.

Grok snapshots also contain the official `grok_media` endpoint declaration,
`xai-grok-build/<selected version>`, sharing the selected source, owner, version
and `grok-shell` identifier. It is immutable request rendering, not a new identity
selection. Sampler/control and Imagine declarations are independently preserved
during version updates. See the [official endpoint contract](providers/GROK.md#endpoint-identity).

The exact compiled Grok sampler identity is `grok-shell/1.0.45 (linux; x86_64)`, with
identifier `grok-shell`, client version `1.0.45`, and CLI/control mode `headless`. Official public API
requests omit the CLI mode declaration.
The mode is rendered only for the exact native proxy/control hosts
`cli-chat-proxy.grok.com`, `auth.x.ai` and `accounts.x.ai`. Custom suppliers,
public/regional API hosts and cross-host redirects omit it while retaining the
same selected User-Agent, identifier and version. Rendering for a destination
never mutates the owner's snapshot. Token exchange and refresh freeze the
snapshot once per operation and use a dedicated Grok control transport pool to
render every permitted HTTP attempt, including explicitly overridden token
destinations. Token redirects retain the initial endpoint's scheme, host and
port and the standard ten-hop bound; a redirect cannot delegate trust to
another origin or downgrade HTTPS before the grant is sent. Same-origin
redirects retain the frozen snapshot. Other providers and signed transports
retain their preparation and signing order.
Compatible Responses WebSocket handshakes use a dedicated Grok-only transport
to render the same owner snapshot for every destination and redirect. Delayed
pool prewarming retains the selected snapshot after the request context ends;
it does not resolve a newer account or global identity. Other WebSocket presets
retain their prepared headers, including signed User-Agent declarations.
OS and architecture use the fixed Ubuntu baseline, in the official Linux/x86_64
spellings, independently of Go's runtime platform.
`XAI_GROK_CLI_VERSION` may select a supported version while retaining that
family, platform fingerprint, identifier and mode; `1.0.41` remains the accepted
version floor and an existing valid account, global or environment pin keeps
selecting its own version instead of being forced to the compiled default. The
compiled default follows the frozen local grok-build source and does not track a
build-time network scrape or an ambient `GROK_VERSION`.

The exact compiled Antigravity identity is
`antigravity/hub/2.23.0 linux/amd64`, with identifier `antigravity`, client
family `antigravity/hub`, and client version `2.23.0` encoded in the UA.
OS and architecture remain `linux/amd64`. Only `setUserSettings` and `fetchUserInfo`
also send `X-Goog-Api-Client: gl-node/22.21.1`. This SDK declaration belongs to
those endpoints and remains fixed during client version updates. The base
preset preview describes the shared UA; these endpoint declarations are added
to a copy of the trusted request snapshot, without changing the parent snapshot.

Official `v0.2.17` renamed the Antigravity client family from
`antigravity/<version>` to `antigravity/hub/<version>`; Plus keeps its pinned
Ubuntu system fingerprint, so the compiled identity stays
`antigravity/hub/2.23.0 linux/amd64`. Preset validation follows the new family:
a stored custom Antigravity User-Agent that still uses the retired
`antigravity/<version>` prefix is an invalid candidate and falls through
atomically to the next source, and the management API rejects it until the
operator saves an `antigravity/hub/...` value or clears the override.

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

## Outbound header privacy

Routing Host (including HTTP/2 authority, URL host and TLS SNI) is exempt and
may contain `sub2api`; it is never removed, substituted or rewritten. Existing
URL/allowlist/SSRF validation remains mandatory. Every other outgoing HTTP
header name or value must exclude `sub2api`, case insensitive.
This applies to every value of a multi-value header, custom headers and
trailers, HTTP/WS handshakes, redirects, probes, OAuth, SDK and auxiliary clients.
Body text and URL paths are not rewritten; a Referer containing such a path is
removed before the redirected request is sent.

Management APIs reject branded identity candidates and custom-header overrides.
Existing invalid identity candidates fall through atomically in the documented
source order; version overrides and environment versions cannot reintroduce the
token. The compiled identities below are unchanged. Ordinary optional headers
are filtered at the final transport boundary, after cookies and SDK defaults.
Credentials (Authorization, proxy authorization, cookies and API keys),
or User-Agent containing the token stop the request before
network dispatch with a static error that does not echo their values. Earlier
header cleanup preserves credentials and UA until this check; it must never
silently remove authentication or let an SDK substitute its own identity.

Unsigned Bedrock requests are filtered before signing. An already signed request
with prohibited declarations is rejected without modifying its signed headers;
clean signatures are preserved byte for byte. `X-Grok-Client-Tool-Cache` has no gateway control semantics or special filter;
explicit extras follow general policy, including signature preservation. Shared clients and independent HTTP/req clients apply the
same check to redirected attempts; plugin forwarding checks before opening its
forward stream. GLM off-peak task IDs use the official `offpeak-<UUID>` shape.

## Selection and persistence

For non-Codex identities, selection is:

1. A valid credential-owning account `credentials.outbound_identity` candidate.
2. The configured global native preset, or a permitted compatible account-type
   mapping and its configured global preset.
3. The platform default preset, using an existing valid environment override
   when available, otherwise the compiled declaration.

Migration 278 removes retired/unknown type-default keys and discards whole
foreign account candidates on fixed-family accounts. Correct-family account
candidates, global profiles/runtime declarations, compatible mappings, tokens,
model settings and billing data remain intact. This is a forward-only cleanup,
not a compatibility path; management writes reject the retired selections and
runtime validation prevents stale data from selecting a foreign family.

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
All User-Agent, version and configurable identity-header candidates containing
`sub2api` (case insensitive, anywhere in the value) are invalid, including Codex
account/global User-Agents and version overrides. Rejecting them during selection ensures the final
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
Both write validation and runtime resolution check each raw candidate before
trimming ordinary surrounding spaces. Leading/trailing CR/LF and other invalid
header bytes reject management writes; persisted invalid candidates fall through
atomically to the next source. The global cache retains the raw value so a
cached read cannot repair an invalid candidate into a higher-priority identity.
The settings cache retains an absent global candidate as empty, including on
read failure. The shared resolver supplies the compiled identity and reports
`compiled_default`; only an explicitly configured valid global UA reports
`global`, even when that configured fingerprint equals the default.
Codex version selection is explicit administrator override → compiled release. Automatic
release fetching and persisted synchronization results are retired; migration 290
removes their settings. OpenAI API-key accounts retain the native Codex family,
including stale stored foreign candidates; upstream supplier accounts retain
their explicit compatible-family mapping.
The existing Codex account editor is retained. The new global `profiles` map
cannot replace Codex configuration. See the exact default and source matrix in
[Codex client profiles](protocols/CODEX_CLIENT_PROFILES.md).

The plugin account directory resolves each bearer export in an independent
credential scope; caller forwarding evidence cannot supply its owner. After
token acquisition or refresh, the exported token, ChatGPT/FedRAMP headers,
Codex identity triple and proxy all use the acquired immutable account snapshot.
The Codex account/global/default precedence above applies to this export, with
the same owner's fallback settings retained across acquisition. The acquired
owner must still satisfy the plugin's capability scope and supported account
type. Missing proxy details or ambiguous acquisition evidence prevent credential
export. AgentIdentity accounts remain readable in directory metadata but have
no bearer export; resolving one returns no identity and performs no task
registration or credential write.

Generic `header_overrides` cannot select or modify a client identity. Saves reject
managed identity names with `INVALID_HEADER_OVERRIDE` (HTTP 400), including empty
values and disabled override configurations. The shared managed-header registry
covers User-Agent, client identifiers/versions, the Kimi Code `X-Msh-*`
and ZCode product/runtime declaration blocks and SDK declarations such as `X-Stainless-Package-Version`.
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
The hostname and pinned-environment data migrations require no schema or YAML/environment changes.
Defaults are empty `profiles`, `defaults` and `runtime` maps. Existing Antigravity
`antigravity_user_agent_version` is imported into the editable Antigravity
profile before the unified configuration is first saved — either by an
administrator save or by the runtime-declaration materialization described
above, whichever happens first. After that save,
clearing the profile restores the default without reviving the old setting. Existing environment
defaults (`SUB2API_CLAUDE_CLI_VERSION`, `XAI_GROK_CLI_VERSION`,
`ANTIGRAVITY_USER_AGENT_VERSION`) remain below explicit identity configuration.
The Antigravity profile imports only the version: a custom User-Agent saved
under the retired `antigravity/<version>` family is not migrated, so it becomes
an invalid candidate and the resolver falls back to the next source until the
administrator re-saves it as `antigravity/hub/<version> …` or clears it.

```json
{
  "profiles": { "claude": { "preset": "claude", "version": "2.9.1" } },
  "defaults": { "gemini:apikey": "gemini", "openai:upstream": "codex" },
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
ChatGPT and native Codex Platform API-key provider requests emit the coherent
UA, Originator and Version, including `responses/compact`, models discovery and
HTTP/WS probes. This matches official Codex `1b6e0d8145` default client headers
and the built-in OpenAI provider, independently of credential type. Inbound
thread originators and generic overrides cannot replace any of the triple. Explicit compatible presets render their own header mapping.
The native Codex finalizer removes foreign SDK identity headers as well as stale
or duplicate core declarations before rendering the selected identity.

Codex Responses, compact and WS handshakes also advertise the official
`remote_compaction_v2` capability for OAuth, setup-token and API-key credentials.
The capability uses the same trusted account snapshot as the client identity;
other declared feature tokens remain intact, and v2 is appended only if absent.
Compatible suppliers using a non-Codex preset do not receive session-level
synthesis, while an explicitly native v2 compaction request still declares its
required capability. This does not add the header to direct search, discovery,
OAuth credential operations or usage requests. See the
[Codex protocol boundary](protocols/CODEX_CLIENT_PROFILES.md#codex-source-compatibility-boundaries).

The request scope retains each credential owner's first selected identity, even
when a handler re-enters forwarding for a retry. This includes the choice to use
the native Codex resolver: changing a type default cannot switch an in-flight
request between Codex and another preset. Failover uses the other credential
owner's snapshot; a fresh request observes new settings. OAuth exchange/refresh
and its account/subscription enrich requests retain one identity as well.
Authorization-code exchange and device-code start/poll omit identity headers.

Their configured proxy binding is validated before any login request. Missing
or mismatched proxy rows cannot become direct egress. Account-owned token
refresh and revocation retain the hydrated credential owner's proxy snapshot
and reject an unavailable or invalid bound route before provider IO.
Browser and device sessions retain their initial proxy ID and URL; a callback
may acknowledge the same ID but cannot re-resolve an edited row or choose a
different binding. A different route requires a new login.

WHAM usage, reset-credit details and credit consumption validate and capture
the credential owner's proxy before token lookup or refresh. Shadow usage uses
the parent's captured binding; Agent Identity has the same routing requirement.
A missing or malformed binding cannot trigger a mutable proxy lookup, direct
request or credential refresh. Explicitly unbound owners ignore stale hydrated
proxy edges. These checks do not enable credit resets through shadow accounts.
Usage, reset-credit details, consumption and their retries also retain the same
captured authentication mode, account ID and selected identity. Token/cache
lookups cannot pair a replacement login's credential with that earlier call's
headers or route. Agent task recovery permits a newer task for the same owner;
it rejects a relink before registering or replaying with another owner.

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
Codex models discovery (OAuth and API Key using the Codex preset), refresh,
revoke, and chatgpt.com backend-api auxiliary calls send the header.
Authorization-code exchange and device-code start/poll do not. A configured
Codex User-Agent may also carry the official trailing ` ({suffix})` group.
That group is preserved through pairing and manual version updates and is
never generated by the gateway.

Codex source compatibility fixes preserve the source matrix and exact compiled
triple above. Guardian role context, non-root session/cache separation, Lite
transport metadata and request encoding cannot select or overwrite identity.
OAuth HTTP Responses compression runs after JSON adaptation at the transport
boundary; it excludes signed Agent Identity requests and preserves the selected
headers. Non-root sessions retain independent tenant/credential-scoped seeds;
this does not remove isolation or enable an inbound identity override. See
[Codex compatibility boundaries](protocols/CODEX_CLIENT_PROFILES.md#codex-source-compatibility-boundaries)
for supported contexts and explicit differences from the official client.

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
requests use the global native preset. Bedrock applies the native Claude identity
before SigV4 signing and reuses the same declarations at send time.
This includes the non-streaming Bedrock account connection test, for both IAM
credentials and bearer API keys. IAM signatures include the selected companion
declarations; the send-time finalizer must not introduce a new signed header.

Gemini native model list/detail (including every Vertex publisher-model page)
and explicit-cache create/patch/delete resolve the credential owner's snapshot
before acquiring an OAuth/service-account token and apply it at the HTTP send
boundary. Failed-cache cleanup keeps that same snapshot when detached from a
cancelled client. Mutation recovery resolves the bound account, and failover
resolves the new owner; neither path inherits another account's declarations.
The source matrix remains valid account > configured global preset/type default
> environment/compiled default, with invalid candidates falling through atomically.
The exact native Gemini default remains `GeminiCLI/0.1.5 (Linux; x64)`, with no
`Originator`, `Version` or `X-Goog-Api-Client` wire headers. These paths are covered
by `gemini_control_identity_test.go` and the cached-content handler regressions.

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
| Prompt Audit endpoint token | native Codex global/default chain; one snapshot per endpoint credential across an evaluation/job's chunks and failover returns; a `/models` probe and its inference fallback share a snapshot |
| Content Moderation endpoint API key | native Codex global/default chain; same-key retries share a snapshot, key rotation or endpoint failover resolves the new owner; administrative key tests start independent operations |

These credentials have no account-level identity field. Only permitted compatible
API-key type defaults can select another preset; native domestic monitors use
their own platform profile, including MiniMax's API-key SDK identity. Native OpenAI API-key Codex requests send
the selected UA/Originator/Version triple; other presets render their
defined companions. Fresh monitor checks, audit evaluations/jobs, probes and
key tests observe current settings. Supplier identity resolution does not select
a forwarding account or move inference ahead of the ingress audit boundary.

V1 active checks use this same credential-owner snapshot whether V1 runs alone
or is enabled as an extra source for V3 status. Scheduled and manual checks,
origin HEAD, model POSTs and quota requests retain the established trusted
platform identity; monitor mode, inbound headers and `extra_headers` cannot
replace its managed declarations. Enabling the V3 probe source changes status
aggregation and viewer visibility, without selecting another outbound identity.

Model discovery includes both standard model lists and the Codex-style manifest
requested from an OpenAI API-key account's configured upstream. Native Codex
identity governs that manifest's final headers and required `client_version`
query together, including custom base URLs. Historical foreign-family candidates
cannot remove its version, residency or native catalog-cache identity; the
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
architecture, terminal and SDK fingerprint. Codex release upgrades are reviewed
and maintained in code; no background or immediate Codex version synchronization
exists. Explicit manual version settings remain available. A versionless family has no version-only update: it
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
declarations are removed from other destinations. CLI 403 responses are returned without an automatic cross-host public API replay. Repository tests capture both actual
transport methods with inherited/explicit Codex, Grok and
Claude identities. Every account-owned Grok HTTP path also receives the Grok
transport profile at the shared final preparation boundary unless the owning
operation explicitly selected a more specialized profile.

Grok inference builders own the sampler declarations
separately from the identity triple. They retain one random UUIDv4 `x-grok-req-id` across retries of a logical prompt
and allocate another for the next prompt. `x-grok-agent-id` is the official
UUIDv5 derivation from a persisted random UUIDv4 device seed. They declare the
final model, attach the
OAuth credential owner's `sub` when available, and reuse the tenant-isolated
conversation snapshot for conversation/session headers and the official UUIDv5
conversation-group derivation. Generic overrides cannot set these fields. The
gateway omits sampler and response-authentication declarations on model,
billing and media-status lookups, and omits optional turn, retry, deployment,
and tracing declarations when it does not possess the corresponding
authoritative value. One constructed sampler request keeps its request
association snapshot across transport retries and redirects; a newly constructed
logical prompt renders a new association; a retry/resubmit of the same prompt
retains it. Synthetic conversation/session IDs are UUIDv7. Imagine start/poll
operations carry their isolated session association without sampler fields.

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
appears only with the bytes it describes; a destination change (a cross-origin redirect) rebuilds a plain body from the
final JSON and drops the declaration. Audit, payload hashing, inflight
estimation, cache/session keys and billing keep the uncompressed semantics.
Generic header overrides cannot supply `Content-Encoding`.

Agent-only native differences stay explicitly scoped rather than being
approximated with surface declarations: the native Rustls ClientHello, doom-loop recovery headers, enterprise
deployment authorization, and turn/resubmit/tracing declarations. The gateway
keeps the selected account snapshot and its Go TLS fingerprint, and
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

## Domestic SDK wire profiles

The settings response includes `wire_profiles` (protocol-specific resolved
inference headers) and `control_plane` (OAuth/account-service headers), alongside
the common product declarations. These are read-only projections of the selected
identity; URL protocol rendering never chooses another source or client family.
Snapshots deep-copy the protocol maps, survive OAuth session serialization, and
are reapplied at final transport send, including retries. Arbitrary header
maps cannot supply SDK, device or control-plane declarations.

Kimi's OAuth and API-key inference use the same `kimi-code-cli/2.3.1` UA and
`X-Msh-*` declarations. Anthropic uses `X-Stainless-Package-Version: 0.95.2`;
Chat Completions/Responses use `6.34.0` (the reviewed Anthropic/OpenAI JS lockfile
versions). Both explicitly pin `X-Stainless-Lang: js`, `X-Stainless-OS: Linux`,
`X-Stainless-Arch: x64`, `X-Stainless-Runtime: node`, and
`X-Stainless-Runtime-Version: v22.19.0`, a supported official Node host profile.
OAuth/device/token requests carry the product/device declarations without SDK
headers. Product-version edits preserve this SDK fingerprint.

ZCode's exact default inference UAs (both account types) are:

| Protocol | User-Agent |
| --- | --- |
| Anthropic | `ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22` |
| Chat Completions | `ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22` |
| Responses | `ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22` |

`runner-options.ts` supplies the bootstrap headers to `ai`, so its `ai/6.0.193`
suffix replaces the provider factory's suffix; provider-utils appends its own
version and runtime. Node 22's `navigator.userAgent` produces `runtime/node.js/22`
(the desktop bundles Node v22.16.0). A container wire experiment using the exact
locked SDKs, the same provider/options header layering, and a mock fetch verified
these strings, plus both MiniMax UAs and their six Stainless declarations.
The fixture uses no live provider credentials or requests. SDK pins are owned by
these presets and cannot be changed by inbound headers or generic overrides.

ZCode Project Access Token issuance is also an account-service operation. Its
`clientType:"zcode"` and selected `clientVersion` body declarations accompany the
same trusted control identity. PAT rotation changes authentication, not the
stable owner, identity source, client family, runtime facts or inference SDK
suffix. No copied key or cached token fingerprint selects an identity. See
[Zhipu / GLM](providers/ZHIPU.md) for token lifetime and project scope.

ZCode monitor quota calls project that same owner's control identity, including
the paired app version and control runtime facts, and omit `X-ZCode-Agent`.
Their raw PAT authorization is independent of identity selection. Live personal
and team model discovery stays on the linked estate's coding `/models` endpoint
and authenticates with its current PAT; a public Key ID, earlier copied secret
or generic URL/header override cannot become an authentication fallback.

The gateway applies trusted ZCode product declarations to all account-service
calls, including external business origins where official NodeApiClient only
attaches the source block to its configured ZCode origin. This is an intentional
extension required by the provider-bound identity contract. Optional telemetry
IDs are not copied from another installation. DeepSeek uses the official Harness web login branch
(`platformClientHeaders(null, client)`); its selected client version is
`0.2.1-rc.3`, independent of any desktop shell.

Regression evidence: `domestic_identity_review_test.go`,
`domestic_outbound_identity_test.go`, the complete Codex identity regressions,
`pkg/outboundidentity`, `pkg/zcode`, OAuth session tests, settings/admin tests and
frontend outbound-identity settings/editor tests.

## Native domestic OAuth login paths

DeepSeek, Kimi and MiniMax browser/device login, callback exchange, token refresh,
account probes and Messages forwarding share the trusted identity resolution
contract and exact defaults documented above. New-login sessions resolve the
native preset; relink sessions resolve the credential-owning account. Sessions
retain that immutable snapshot across polling and publish it with the account.
Kimi retains all device declarations. MiniMax OAuth uses the explicitly enumerated
versionless family, while its API-key profile is versioned. DeepSeek's authorization-only `x-client-version` matches the
selected identity; its web platform, selected locale and captured timezone offset
are scoped to platform requests. No Codex source, family or default changes are introduced.

Native OAuth destinations and authentication are fixed by provider/region before
sending. Refresh and inference reuse the same owner snapshot, and failover
resolves the next owner. DeepSeek's `x-dsh-auth-token` is reserved against generic
header overrides. The native paths disable redirects. Native Kimi model/usage
queries and DeepSeek model/account-wallet queries reuse that same snapshot and
fixed endpoint authentication; generic API-key defaults cannot select their
credentials. New endpoints add no identity-family exception. Tests are in
`internal/pkg/cnoauth`, `cn_oauth_service_test.go`, existing native Anthropic
adapter/identity suites and frontend `useCNOAuth`/`CNOAuthPanel` suites. See
[domestic OAuth](providers/DOMESTIC_OAUTH.md) for official source evidence,
request-only declarations and session behavior.

## Requirement-based regression coverage

Expected wire declarations are literal official-client fixtures, with source
paths recorded next to them. Tests must not use the identity builder under test
to calculate their own expected default UA, SDK or OS headers. Snapshot tests
may compare a captured first request with retries, but independent default
fixtures remain mandatory. In-process HTTP/WS servers inspect what is received,
including SDK-added cookies and redirect Referer values; helper-only assertions
are insufficient for the final outbound privacy boundary.

| Requirement | Regression evidence |
| --- | --- |
| Native providers, both account types, official default UA/SDK/environment, probes | `service/domestic_identity_review_test.go` literal fixtures; `repository/outbound_identity_test.go` HTTP captures |
| Account/global/environment precedence, invalid candidates, version updates, retry/failover snapshots | `service/outbound_identity_test.go`, `outbound_environment_test.go`, `domestic_outbound_identity_test.go`, complete Codex identity suites |
| No branded names/values; multi-value headers, redirects, cookies, rejected auth, signed requests | `pkg/brandidentity/brandidentity_test.go`, `service/bedrock_signer_test.go`, `openai_ws_client_test.go`, `plugin_security_regression_test.go` |
| OAuth/account-service versus inference declarations; locale, timezone, DST and kernel build/release | `pkg/cnoauth/client_test.go`, `pkg/deepseek/identity_test.go`, `pkg/zcode/identity_test.go`, settings UI tests |
| ZCode personal/team PAT quota and model discovery, fixed estate endpoints, source/companion coherence, login relinking and manual-key isolation | `service/zhipu_project_quota_test.go`, `service/zhipu_project_models_test.go` |
| Cancellation/expiry and delayed responses, single consumption across replicas, unavailable/changed proxies | `useCNOAuth.spec.ts`, `useZhipuOAuth.spec.ts`, both login panel suites, `pkg/zcode/session_replica_test.go`, `service/zhipu_oauth_service_test.go`, admin handler tests |
| Off-peak official task IDs, repeat retirement, original owner identity/auth, concurrent settlement, expired admission, rejection before forward/probe and bounded waits | `service/zhipu_offpeak_ticket_test.go` |
| Active ticket leases through send/EOF/Close/cancel/error, overlapping streams and old-owner draining on rotation | `service/zhipu_offpeak_lease_test.go` |
| Ticket bare/envelope/empty settlement responses, business reset/retry hints and distinct physical request IDs | `pkg/zcode/offpeak_wire_test.go` |
| Exact model_not_found over HTTP/SSE/WS, preserved terminal code and no whole-account auth/quota punishment | `service/openai_model_not_found_contract_test.go`, `handler/openai_gateway_credential_failover_test.go` |
| Grok acquired-owner destination/ordinary headers/egress and generation-isolated capability probes | `service/grok_acquired_owner_request_test.go`, `grok_auxiliary_owner_test.go` |
| DeepSeek native OAuth/API-key catalog auth, version/capability gates and conditional metadata writes | `service/deepseek_models_test.go` |
| ZCode linked discovery reaches final PAT auth on both estates; every model/ticket 401 invalidates its PAT; concurrent transient refresh cannot resurrect rejected material | `service/zhipu_project_tokens_test.go` |
| Linked ZCode forwarding/probes set redirect prohibition; both HTTP transport methods prevent redirected credential dispatch | `service/zhipu_project_tokens_test.go`, `repository/http_upstream_test.go` |
| Kimi reasoning survives optional effort errors; Coding absent/null/disabled reasoning clears stale efforts without changing public/PAYG metadata defaults | `service/official_agent_update_test.go` |
| Codex turn-state provenance retains string principal evidence across Redis replicas, hashes private session seeds, expires and rejects legacy numeric owners; Live snapshots survive Redis persistence and controller changes | `repository/gateway_cache_codex_identity_integration_test.go` against real Redis; existing turn-state and Live service suites |

All paths above run without live provider credentials. Database-dependent
migration and Channel Monitor V3 behavior is additionally exercised against
isolated PostgreSQL/Redis; its unit tests verify pause/resume, fresh confirmation
evidence, visibility and incident recovery. Passing a coverage percentage alone
does not demonstrate these requirements.

## StepFun / Step-Code

StepFun OAuth (Step Plan) and API Key accounts are pinned to the same `stepfun`
preset. Exact identity: `stepcode (linux 6.8.0-31-generic; x64)`, identifier `stepcode`,
empty product version. Step-Code's actual OpenAI adapter imports
`packages/providers/src/utils/pi-user-agent.ts`; the coding-agent helper that
accepts a version is not its inference UA. `step/environment.ts` defaults
`STEP_CLIENT` to `stepcode`. The inference adapter pins OpenAI JS 6.40.0.

Chat Completions renders `X-Step-Client: stepcode`, `X-Stainless-Lang: js`,
`X-Stainless-Package-Version: 6.40.0`, `X-Stainless-OS: Linux`,
`X-Stainless-Arch: x64`, `X-Stainless-Runtime: node`, and
`X-Stainless-Runtime-Version: v22.19.0`. This supported Node host and the
Ubuntu Noble kernel are fixed independent of the server host. Discovery uses
fetch upstream; this gateway adds the same trusted product UA as an explicit
identity-policy difference, without inference attribution or SDK headers.
Browser navigation uses the browser's own headers; the server does not claim
it can override them. Cloud login initialization/polling and optional Passport
account-session refresh use the same trusted product UA without inference SDK
headers. The init body separately declares client `stepcode`, version `0.84.4`,
platform `linux-x64`; that provider-defined login audit metadata does not add a
version segment to the product UA. Passport app/device headers are account-session
protocol state and cannot select the identity source. No model-key refresh
endpoint is inferred from the account-session refresh operation.
No synthetic Originator, Version, device-name, locale or timezone header is
sent. Optional high-sensitivity `x-step-*` trace fields are not generated.

`stepfun` is explicitly versionless. Its exact UA, SDK and environment are
read-only. Product-version updates cannot change it; SDK upgrades require new
source and wire evidence. Other presets still require a complete version.
Account > global native profile > compiled default applies atomically.
Both OAuth and API Key retain Step-Code; foreign client-family selections are rejected.
Retries, probes, discovery and native Plan usage retain the credential owner's
snapshot; failover resolves the new owner. Final header privacy remains mandatory
for every path. Cloud login is based on Step-Code commit
`81e79bc4b699148d6823fedde9b5e70d9a80255f` (`login-client.ts`,
`login-refresh.ts`, `pi-user-agent.ts`); the product-token migration from `step`
to `stepcode` is an explicit source-backed identity change, not a version-only sync.
Settings preview exposes the shared profile and the Chat Completions wire block.

Pre-account OAuth catalog discovery validates the ready session's owner,
platform and lifetime, then uses its captured identity and Step Plan region
through the shared model-discovery transport. It exposes only model IDs and
does not consume the grant. `stepfun_model_restrictions_test.go` covers its
literal discovery headers, both regions, proxy, and rejected session states.

Independent StepFun tests cover exact literal wire declarations, four
region/auth combinations, actual forwarding and probes/discovery, cloud-login
polling and static model-key lifecycle, bounded flow IDs, invalid/missing state,
secret/public flow separation, cancellation, optional account-session refresh,
single consumption, region pinning,
versionless validation and source priority. See [StepFun](providers/STEPFUN.md).


V1 streaming probes use the same `postMonitorRequest` identity/SSRF path and
same-operation snapshot as JSON probes and HEAD Ping. `stream_probe` controls only
the request body stream flag and Accept header; custom headers cannot replace
managed identity. The monitor stream and identity regressions cover this path.

## Official request-state contract

[OFFICIAL_AGENT_REQUEST_STATE.md](OFFICIAL_AGENT_REQUEST_STATE.md) defines UUID
versions, relationship preservation, request/conversation/device lifetimes,
credential ownership, persisted random mappings and failure behavior. These
fields supplement the trusted identity triple; they never select its source.
