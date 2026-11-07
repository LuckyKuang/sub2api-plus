# Official agent outbound baselines

Both **OAuth and API Key** accounts for each platform use its official agent
below. This is the source index for future version and outbound-protocol reviews;
it contains no developer checkout paths. The seven-provider review was refreshed
on 2026-11-07 against each official default branch. It covers OAuth and API-key
request headers/bodies, SDK and runtime declarations, auxiliary authentication,
model discovery, request ownership and terminal errors. Historical review notes
below retain the source/version they originally evaluated.

| Platform | Official source | Reviewed commit | Selected agent version |
| --- | --- | --- | --- |
| OpenAI | [openai/codex](https://github.com/openai/codex.git) | `efdacaf46ff54660a239189a6d62434c5db33d2c` | stable `rust-v0.167.0` |
| xAI | [xai-org/grok-build](https://github.com/xai-org/grok-build.git) | `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8` | `1.0.45` |
| DeepSeek | [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness.git) | `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159` | `0.2.2-rc.1` (prerelease) |
| Kimi | [MoonshotAI/kimi-code](https://github.com/MoonshotAI/kimi-code.git) | `eb2035f3cad57cc12d4d5082ec626f7f0ea5f651` | stable `2.5.2` |
| MiniMax | [MiniMax-AI/minimax-code](https://github.com/MiniMax-AI/minimax-code) | `3c21163c85a9684030cb147bfef48c6284fa3c46` | product `0.7.2`; managed UA versionless; BYOK SDK `0.91.1` |
| StepFun | [stepfun-ai/Step-Code](https://github.com/stepfun-ai/Step-Code.git) | `a4141afa49f686296cb04f5aac5bf8940679c0a3` | product `0.84.4`; inference UA versionless; OpenAI JS `6.40.0` |
| Zhipu | [zai-org/ZCode](https://github.com/zai-org/ZCode.git) | `a9fe401eede6e6e5e01be9048679c206fe6f02d9` | stable product `3.17.2`; separate CLI line `0.17.1` |

The managed Codex bridge/task runtime additionally pins the official
[`rust-v0.167.0` release](https://github.com/openai/codex/releases/tag/rust-v0.167.0)
and its source commit
[`086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a`](https://github.com/openai/codex/tree/086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a).
Its execution review includes static API-key file authentication, custom model
providers, Responses Lite `additional_tools`, `exec` JSONL events, noninteractive
approval enforcement, plugin startup, cloud skills, configuration discovery and
system/MDM precedence. Relevant entries include `codex-rs/exec/src/lib.rs`,
`codex-rs/config/src/loader/{mod.rs,macos.rs}` and
`codex-rs/core/src/client.rs`. Transparent bridge review additionally covers
[`codex-rs/responses-api-proxy/src/lib.rs`](https://github.com/openai/codex/blob/086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a/codex-rs/responses-api-proxy/src/lib.rs)
and [`read_api_key.rs`](https://github.com/openai/codex/blob/086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a/codex-rs/responses-api-proxy/src/read_api_key.rs): HTTP-only request forwarding, stdin API-key constraints,
startup status, upstream errors and streaming bytes. The gateway uses a random
loopback nonce for that stdin key, keeping the actual OAuth JWT in its existing
credential-owner sender. Embedded archive and native-executable digests are owned by
`backend/internal/codexcli/manifest.go`; the real-binary contract harness is
`tools/test_codex_cli_wire.py`. Its official proxy cases and
`codexcli/TestCodexBridgeOfficialBinaryWire` capture real wire traffic through
the Go nonce relay and synthetic upstream, including API Key model GETs,
compatible Responses and JSON/binary multipart image requests. They do not
imply real-account or live-model acceptance. Project bridge owners are
`backend/internal/codexcli/bridge.go`,
`backend/internal/service/codex_cli_bridge_transport.go`,
`openai_plugin_transport.go` and `openai_codex_transport.go`.
The downstream display review also covers the release source's
[`codex-rs/tui/src/chatwidget/safety_buffering.rs`](https://github.com/openai/codex/blob/086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a/codex-rs/tui/src/chatwidget/safety_buffering.rs).
`service/TestCodexSafetyBufferingOfficialCLIInteractive` runs the unmodified
pinned binary with `--no-daemon` in a private PTY through the gateway. It observes
the notice/actions off, their absence on, normal completion and one generation
for both OAuth/API Key owners and native/official CLI outbound. The PTY owner is
`tools/test_codex_safety_buffering_tui.py`; desktop App rendering is not executed.
The auxiliary title request follows
[`codex-rs/tui/src/app/thread_title.rs`](https://github.com/openai/codex/blob/086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a/codex-rs/tui/src/app/thread_title.rs)
and is counted separately from the user's original generation.

ZCode's selected product version matches the published
[`v3.17.2` release](https://github.com/zai-org/ZCode/releases/tag/v3.17.2) and
the project-access-token implementation at the reviewed commit. DeepSeek
explicitly follows the `dsh-v0.2.2-rc.1` prerelease; it is not a stable Release.

| Agent | Official code entrypoints to review | Project owners |
| --- | --- | --- |
| Codex | `codex-rs/login/src/auth/default_client.rs`, `codex-rs/model-provider`, `codex-rs/codex-api`, auth and WebSocket clients, `codex-rs/protocol/src/{session_id,thread_id}.rs`, `codex-rs/core/src/{installation_id,responses_metadata,client,realtime_conversation}.rs`, `codex-rs/core/src/state/auto_compact_window.rs`, `codex-rs/core/src/session/mod.rs`, `codex-rs/exec/src/exec_events.rs`, `codex-rs/responses-api-proxy/src/{lib,read_api_key}.rs` | `service/openai_codex_identity.go`, `openai_gateway_service.go`, `openai_ws_client.go`, `pkg/openai`, `internal/codexcli`, `service/codex_cli_{account,forward,request,bridge_setting,bridge_transport}.go`, `service/openai_plugin_transport.go`, `service/openai_codex_transport.go`, `tools/test_codex_cli_wire.py`, complete Codex transport/identity suites |
| grok-build | `crates/codegen/xai-grok-sampler`, `xai-grok-voice`, `xai-grok-imagine`, OAuth/OIDC nonce and JWT verification, billing clients and sampler request/conversation IDs | `pkg/xai`, `service/grok_*`, `account_test_service_grok_test.go` |
| deepseek-harness | `packages/llm/llm/src/attribution.ts`, `packages/llm/llm-deepseek/src/{config,adapter,messages-api,files-api,file-id,file-store}.ts`, `llm-deepseek/src/{listing,listed-catalog}.ts`, `llm-deepseek-api-key/src/{index,config}.ts`, account-platform and anonymous-user-id packages | `pkg/deepseek`, `pkg/cnoauth/client.go`, `service/{deepseek_request_state,domestic_agent_models,cn_oauth_service,provider_files}.go`, `handler/provider_files_handler.go`, `repository/provider_files_repo.go` |
| kimi-code | `packages/oauth/src/{identity,oauth,managed-kimi-code,managed-usage}.ts`, `packages/agent-core-v2/src/llm-adapter`, `packages/agent-core-v2/src/human/llm/builtin/provider/kimi/{index,protocol}.ts` | `pkg/kimi`, `pkg/cnoauth/client.go`, `service/{domestic_agent_models,upstream_models,cn_oauth_service}.go` |
| minimax-code | `packages/oauth-core/src`, `packages/local-runtime-v2/src/service/model-system/resolution/{model-resolver-helpers,model-resolver-byok}.ts`, [`packages/tui/src/account/matrix-account-client.ts`](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/tui/src/account/matrix-account-client.ts) personal workspace/region quota, `packages/shared/src/llm-error-classifier.ts` plan/Credits errors, `packages/local-runtime-v2/src/service/turn-system/agent-host/assembly/{local-turn-payload-transform,messages-file-api-patcher}.ts`, Anthropic provider and lockfile | `pkg/minimax`, `pkg/cnoauth/client.go`, `service/{minimax_request_state,minimax_oauth_quota,cn_provider_quota_service,provider_files}.go`, domestic wire/quota and Files tests |
| Step-Code | `packages/providers/src/step-provider/{index,login-client}.ts`, `api/openai-completions.ts`, `utils/pi-user-agent.ts`, coding-agent `step/{defaults,environment,search-web-tool,mcp-client}.ts` | `pkg/stepfun`, `pkg/cnoauth/stepfun.go`, `service/{stepfun_*,openai_chat_roles}.go`, login panel/composable tests; remote MCP [contract audit only](providers/STEPFUN.md#remote-web-search-mcp-contract-audit) |
| ZCode | `packages/shared/src/{project-access-token-client,project-access-token,zcodeEndpoint,official-glm-model-id}.ts`, `packages/services/src/model-provider/{zaiStartPlanBilling,codingPlanProviderAvailability}.ts`, CLI `auth/coding-plan-api-key.ts`, `model/{runner-attribution,model-request-headers}.ts`, UUID/request-trace attribution and device-mid helpers, `model/off-peak-request-auth.ts`, bootstrap `model-config.ts`, runtime-platform headers and lockfile | `pkg/zcode`, `service/{zhipu_oauth_service,zhipu_project_tokens,zhipu_offpeak_ticket,zhipu_start_plan_entitlement}.go` |

Project paths in the last column are relative to `backend/internal/` unless
stated otherwise. Source layout may move; locate the named symbol in the pinned
commit rather than substituting a different agent or desktop bundle.

These commits identify reviewed source, not completion of a deployment's
acceptance checks. Behavior changes require the owning regression suites and
container validation before submission.

The defaults and exact source-priority matrix belong to
[OUTBOUND_IDENTITY.md](OUTBOUND_IDENTITY.md). Valid administrator account/global
pins keep their priority; manual baseline upgrades change defaults, not existing
pins. Codex automatic version synchronization remains prohibited.

Every update reviews both credential types independently: endpoint, auth,
request/body and companion headers, SDK/runtime fingerprint, streaming/WS,
retries, probes, discovery, usage and OAuth auxiliary calls. A product version
must not be inserted into a versionless official inference UA. MiniMax's managed
`MiniMaxAgent` and BYOK `Anthropic/JS 0.91.1` are deliberately different profiles
from the same official MiniMax agent.

Gateway extensions remain explicit: API-key Chat/Responses adapters, configured
compatible endpoints and model discovery can exceed an agent's native UI/API
surface, while retaining that platform's trusted identity and account-owned
authentication. DeepSeek OAuth and official-origin native Messages API-key routes use the
shared Harness `/models` catalog and version/Messages gates, with their own
`x-dsh-auth-token` and `x-api-key` authentication. API-key Chat/Responses and
custom compatible catalogs remain explicit gateway extensions. ZCode project tokens are only for
linked accounts; manually entered API keys are sent unchanged.

Migration 291 removes Grok API-key foreign-family candidates/mapping; 292 gives
DeepSeek credential owners a persistent anonymous UUID; 293 removes obsolete
ZCode linked-account project secrets. They preserve manual API keys and valid
native identity pins. Older Z.ai links lacking their original OAuth token can
use a still-valid stored business token, then require reauthorization; no copied
project-key fallback is retained.

Migration 294 stores random outbound IDs by namespace and irreversible scope
digest, without a scheduling-cache TTL. See [OFFICIAL_AGENT_REQUEST_STATE.md](OFFICIAL_AGENT_REQUEST_STATE.md)
for ID versions, lifetime, grant ownership and failover contracts. Codex main
uses a development manifest version, so its stable identity is manually selected
as `0.167.0` (stable tag commit `086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a`,
[published November 6](https://github.com/openai/codex/releases/tag/rust-v0.167.0));
reviewing protocol changes never enables automatic identity-version updates.

## November 7 protocol alignment

- Codex's stable identity is manually advanced to `0.167.0`; source selection,
  Originator, Ubuntu/architecture/terminal fingerprint and explicit administrator
  version pins keep their existing contracts. The separately embedded, digest
  verified CLI executable is also upgraded to `0.167.0` with reviewed archive
  and native executable digests for all four supported platforms. Settings
  installs or updates only this reviewed target on an explicit click. Runtime,
  browser catalog and compiled default identity version equality is enforced;
  explicit administrator identity pins retain priority.
- Main's [per-turn cache options](https://github.com/openai/codex/commit/41cbd5b9b2d5369ba35957328504093ed324333c)
  serialize `prompt_cache_options.ttl` as `30m` or `12h` on Responses HTTP and WS.
  OAuth/setup-token transforms now preserve that field, including compaction and
  the HTTP bridge; an omitted option on the next turn stays omitted. Existing
  platform API-key model gates retain their documented behavior. This protocol
  change is reviewed from main independently of the selected stable identity.
- The exact `budget_reached` and six audio rejection codes in
  [Codex's error parser](https://github.com/openai/codex/blob/efdacaf46ff54660a239189a6d62434c5db33d2c/codex-rs/codex-api/src/error.rs)
  are terminal with HTTP 400 or a statusless SSE/WS event. They preserve their
  codes without same-owner retries, failover, cooldown or account penalties;
  this includes WebSocket prewarm and scheduler/health observations across
  HTTP, SSE, direct WS, passthrough and the HTTP bridge.
  Unrelated HTTP authentication/server statuses retain their original policy.
- Codex's [animated Images controls](https://github.com/openai/codex/commit/f9e7b19df8aa75490fc1d90f98d3d95ad716d08e)
  flatten `output_format: "gif"`, optional `frame_count` (2–16), and optional
  `frame_duration_ms` (20–2000) into sibling request fields. Native OAuth/setup
  requests now preserve these options, API-key requests retain them, omission
  leaves API defaults intact, and generated GIF data URLs use `image/gif`.
  GIF requests cannot fall back to the Responses tool and lose their controls.
  Guardian-only parent-connection routing cookies remain client-owned optional
  hints; the gateway neither invents these cookies nor forwards inbound cookies.
- Harness `0.2.2-rc.1` retains raw account `x-dsh-auth-token`, native API-key
  `x-api-key`, Messages/Files bases and attribution. Listing badges are optional
  display data, not auth or capability gates. Request serialization, beta tokens,
  anonymous-user/session ownership and web OAuth declarations remain compatible.
- Kimi `2.5.2` retains the locked Anthropic `0.95.2` and OpenAI `6.34.0` SDKs and
  product/device declarations. Its [provider-specific thinking transform](https://github.com/MoonshotAI/kimi-code/blob/eb2035f3cad57cc12d4d5082ec626f7f0ea5f651/packages/agent-core-v2/src/human/llm/builtin/provider/kimi/protocol.ts)
  explicitly chooses `context-management-2025-06-27` for enabled or disabled
  thinking; the gateway now uses that beta instead of generic interleaved thinking.
  Caller prompt, tools and requested thinking fields remain intact.
- ZCode `3.17.2` adds [session-feature attribution](https://github.com/zai-org/ZCode/blob/a9fe401eede6e6e5e01be9048679c206fe6f02d9/apps/zcode-cli/packages/adapters/src/model/runner-attribution.ts).
  The optional `x-zcode-session-features` comes only from the client's own
  accumulated workflow context. The gateway has no such runtime facts, so it
  omits the field and rejects static overrides, including old stored/imported
  values. Inbound case variants cannot leak to inference or control calls.
  PAT, off-peak, source headers and locked inference SDK declarations are retained.
- MiniMax `0.7.2` still sends managed `MiniMaxAgent`, control `MiniMaxCode`, and
  BYOK `Anthropic/JS 0.91.1`. OAuth refresh-journal and prompt-suggestion changes
  are client execution concerns; product version never enters its versionless UA.
- Step-Code's reviewed tree retains product `0.84.4`, versionless inference UA,
  OpenAI JS `6.40.0` and login contract. New unfinished-tool/empty-turn detection
  is client execution behavior; the gateway does not execute those tools.
  Grok's default branch remains at the previous reviewed commit and `1.0.45`.

The request-content and transport regressions include
`official_agent_alignment_november_test.go`, `openai_input_terminal_error_test.go`,
the updated complete identity/default suites and both canonical audit engines.
These are local synthetic wire checks, not a claim of live-account acceptance.

## October 28 Codex alignment

The review compares `970111f6cb62cc02fe36a82ea7d71b37a2889c97` with the
Codex source above. The gateway changes cover both OAuth and API-key routes:

- Exact `policy_capacity_unavailable` errors follow the official
  [SSE error classification](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/codex-api/src/sse/responses_error.rs).
  They keep their code and bounded Retry-After evidence through HTTP/SSE/WS;
  Plus treats capacity failures as request-scoped, without account penalties.
- [FedRAMP API-key recognition](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/login/src/auth/api_key.rs)
  selects government default inference/catalog endpoints and the credential-owned
  routing header. Explicit configured base URLs remain authoritative.
- [Named additional context](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/context-fragments/src/additional_context.rs)
  uses a named `function_call_output` without a call ID. Its content remains
  subject to the shared audit contract, while it creates no paired-call dependency.
- [Workspace selection](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/login/src/auth/manager/workspace_switching.rs)
  retains the original ID/refresh token. Session import adopts the selected
  access token's account, plan and government routing only for a matching
  workspace and known login user; mismatches cannot write an account.
- The [official model catalog](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/models-manager/models.json)
  adds `gpt-6.1-terra`. Catalog metadata and reasoning options do not establish
  a credential's model entitlement or introduce a new price.

The official doctor WebSocket probe produces a client support report. Local
workspace switching UI, OpenTelemetry management, local tool execution and
that report are client responsibilities; this gateway exposes no new client
support-report API. The identity source matrix, environment fingerprint and
ingress audit ordering remain governed by their existing contracts.


## October 28 upstream patch review

Upstream gateway patches are candidates, not provider protocol authorities.
The October 28 merge review rechecked Terra against the pinned official
[Codex catalog](https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/models-manager/models.json):
`context_window=500000`, `max_context_window=872000`, default reasoning `low`,
and supported efforts `low`, `medium`, `high`, `xhigh`, `max`, `ultra`.
`default_reasoning_summary=none` is a summary setting, not an advertised `none`
effort. The catalog does not establish API-wide rejection behavior or prices.
At that review the candidate 1,050,000 context, 128,000 output and new Terra
price fallback were deferred pending independent authoritative evidence.
The candidate Terra-specific Chat tool prohibition is also deferred: a Codex
client's use of Responses does not establish a general API restriction on Chat
tool calls. Likewise the proposed OpenCode-hosted DeepSeek role rewrite is not
enabled by model spelling alone without evidence for that hosted endpoint;
existing native-provider role handling remains in force.

The v0.2.19 Plus release review separately verifies the current official
[Terra API model](https://developers.openai.com/api/docs/models/gpt-6.1-terra)
and [API pricing](https://developers.openai.com/api/docs/pricing). The API price
is now independently established: per million tokens, standard input/output/
cached input/30-minute cache writes are $1/$5/$0.05/$1.25. Fast is twice standard,
Flex and Batch are half standard, and total input above 272,000 applies twice
the input/cache prices and 1.5 times the output price to the entire request.
This evidence authorizes Terra's own price card; no Sol/Astra price is inherited.
Explicit operator prices and the request's frozen billing snapshot retain Plus
precedence. API limits and capabilities are a separate contract from the pinned
Codex client descriptor: this pricing change does not replace that descriptor
or alter the manually selected client identity.

A limited xAI catalog review also inspected
[`EndpointsConfig::resolve_models_list_url`](https://github.com/xai-org/grok-build/blob/f0e3be1100ef5252488e3be8bb0e91cf68d8c305/crates/codegen/xai-grok-shell/src/agent/config.rs)
and the official
[subscription-unblock catalog refresh test](https://github.com/xai-org/grok-build/blob/f0e3be1100ef5252488e3be8bb0e91cf68d8c305/crates/codegen/xai-grok-pager-pty-harness/tests/pty_e2e/subscription_watch_and_gate_verify_pty.rs).
They support destination-specific catalog resolution and refreshing the catalog
after an authenticated entitlement change. This limited inspection does not
upgrade the selected xAI client identity or claim review of that entire tree.
Gateway catalog diagnostics add an owner/proxy fence and bounded snapshot CAS;
those are gateway controls, not newly inferred provider wire requirements.

The candidate common CN frequency-limit rule based on free-form wording and a
90-percent quota threshold remains deferred. It must not replace the existing
provider-specific structured status, captured-owner and cooldown contracts.

## October 24 protocol alignment

Codex's exact `model_not_found` terminal classification follows main commit
`60adfa8513f77c27826f0c5b770e7a7d4eca538e`, after the selected stable tag.
HTTP 400/403/404 and statusless SSE/WS errors keep the exact code. A bounded
gateway failover may try another eligible owner, but never penalizes the whole
account or retries the same owner for a model rejection. Updating the identity
version alone does not supply this behavior.

Grok URL construction, ordinary relay overrides, voice/media/WS egress and
compression settings use the acquired credential snapshot. Compression
observations and single-flight work are isolated by target, credential
generation and proxy configuration, including negative observations.

DeepSeek rc.3 native API-key discovery uses the official auth/path and shared
version/capability parser; metadata writes are conditional on the captured
grant. ZCode ticket control accepts both official successful response shapes,
emits a fresh request ID per physical send and retains an active lease through
the complete model response lifetime. Old active grants drain independently
after credential rotation.

Kimi 2.3.1's login-region resolver does not change inference adapters or the
locked SDK; explicitly linked account regions remain authoritative. MiniMax's
reviewed tree is unchanged. Step-Code's five added commits change agent-local
tools/extension turns/cwd/feedback, without provider, SDK-lock or identity
changes. Neither versionless inference UA acquires a product version.

## Files and Start capability review

The October26 review uses the same pinned trees. DeepSeek's
[`config.ts`](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/config.ts)
selects `/anthropic` as the default base;
[`adapter.ts`](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/adapter.ts)
passes that connection to Files, and
[`messages-api.ts`](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/messages-api.ts)
and [`files-api.ts`](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/files-api.ts)
produce `/anthropic/v1/files` for both account and native Anthropic API-key
profiles. A header-only test's temporary unprefixed base is not the production
default. The gateway first batch uses Anthropic objects/beta, raw account
`X-Dsh-Auth-Token` or API-key `X-Api-Key`, and the conservative public64MiB limit.

MiniMax Code's
[`messages-file-api-patcher.ts`](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/local-runtime-v2/src/service/turn-system/agent-host/assembly/messages-file-api-patcher.ts)
is conditional on a separately advertised managed gateway. It does not establish
direct OAuth or BYOK Anthropic upload entitlement. The first gateway batch
therefore uses the [public upload contract](https://platform.minimax.io/docs/api-reference/file-management-upload)
only for global API-key Chat `video_understanding`, with actual `video_url`
references. Managed, CN, image-understanding, local references in protocol
bridges and unproved remote lifecycle operations remain unsupported. Both
providers use one trusted owner/strict-proxy snapshot, durable local ACLs and
hard inference-owner constraints; the full capability contract is
[FILES.md](providers/FILES.md).

ZCode Start follows
[`zaiStartPlanBilling.ts`](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/packages/services/src/model-provider/zaiStartPlanBilling.ts),
[`codingPlanProviderAvailability.ts`](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/packages/services/src/model-provider/codingPlanProviderAvailability.ts),
[`zcodeEndpoint.ts`](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/packages/shared/src/zcodeEndpoint.ts)
and [`official-glm-model-id.ts`](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/packages/shared/src/official-glm-model-id.ts).
Its independent balance GET carries the trusted `app_version` and linked JWT,
with ZCode control identity and the same owner's strict proxy. It never issues
or substitutes a coding project PAT. See [ZHIPU.md](providers/ZHIPU.md).
