# Official agent outbound baselines

Both **OAuth and API Key** accounts for each platform use its official agent
below. This is the source index for future version and outbound-protocol reviews;
it contains no developer checkout paths. The seven-provider review was on 2026-10-24;
Codex was reviewed again on 2026-10-28 against the source and stable release below,
including request headers, typed IDs, OAuth validation, quota ownership,
model catalogs, terminal errors and retry/failover lifetimes.

| Platform | Official source | Reviewed commit | Selected agent version |
| --- | --- | --- | --- |
| OpenAI | [openai/codex](https://github.com/openai/codex.git) | `154e8736a51224e12b2d990753622a2cd8118f54` | stable `rust-v0.165.0` |
| xAI | [xai-org/grok-build](https://github.com/xai-org/grok-build.git) | `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8` | `1.0.45` |
| DeepSeek | [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness.git) | `22ddee3792402c3e70ec5f86a63c3e3e895a2f60` | `0.2.1-rc.3` (prerelease) |
| Kimi | [MoonshotAI/kimi-code](https://github.com/MoonshotAI/kimi-code.git) | `7ccac2b45b8a3481c76f771bb99c043b562de2c5` | stable `2.3.1` |
| MiniMax | [MiniMax-AI/minimax-code](https://github.com/MiniMax-AI/minimax-code) | `da22f11c55d4748155d7162c1030414228a00565` | product `0.6.5`; managed UA versionless; BYOK SDK `0.91.1` |
| StepFun | [stepfun-ai/Step-Code](https://github.com/stepfun-ai/Step-Code.git) | `81e79bc4b699148d6823fedde9b5e70d9a80255f` | product `0.84.4`; inference UA versionless; OpenAI JS `6.40.0` |
| Zhipu | [zai-org/ZCode](https://github.com/zai-org/ZCode.git) | `3c5b227649371507ddc4eb46ac5133afe6db1cab` | product source `3.16.2`; separate CLI line `0.17.0` |

ZCode's selected product version comes from current source and is paired with
the project-access-token implementation at that commit. It has no matching
published Release at review time (latest Release: `3.14.3`). DeepSeek also
explicitly follows a prerelease. Neither is represented as a stable Release.

| Agent | Official code entrypoints to review | Project owners |
| --- | --- | --- |
| Codex | `codex-rs/login/src/auth/default_client.rs`, `codex-rs/model-provider`, `codex-rs/codex-api`, auth and WebSocket clients, `codex-rs/protocol/src/{session_id,thread_id}.rs`, `codex-rs/core/src/{installation_id,responses_metadata,client,realtime_conversation}.rs`, `codex-rs/core/src/state/auto_compact_window.rs`, `codex-rs/core/src/session/mod.rs` | `service/openai_codex_identity.go`, `openai_gateway_service.go`, `openai_ws_client.go`, `pkg/openai`, complete Codex transport/identity suites |
| grok-build | `crates/codegen/xai-grok-sampler`, `xai-grok-voice`, `xai-grok-imagine`, OAuth/OIDC nonce and JWT verification, billing clients and sampler request/conversation IDs | `pkg/xai`, `service/grok_*`, `account_test_service_grok_test.go` |
| deepseek-harness | `packages/llm/llm/src/attribution.ts`, `packages/llm/llm-deepseek/src/{config,adapter,messages-api,files-api,file-id,file-store}.ts`, `llm-deepseek/src/{listing,listed-catalog}.ts`, `llm-deepseek-api-key/src/{index,config}.ts`, account-platform and anonymous-user-id packages | `pkg/deepseek`, `pkg/cnoauth/client.go`, `service/{deepseek_request_state,domestic_agent_models,cn_oauth_service,provider_files}.go`, `handler/provider_files_handler.go`, `repository/provider_files_repo.go` |
| kimi-code | `packages/oauth/src/{identity,oauth,managed-kimi-code,managed-usage}.ts`, `packages/agent-core-v2/src/llm-adapter` | `pkg/kimi`, `pkg/cnoauth/client.go`, `service/{domestic_agent_models,upstream_models,cn_oauth_service}.go` |
| minimax-code | `packages/oauth-core/src`, `packages/local-runtime-v2/src/service/model-system/resolution/{model-resolver-helpers,model-resolver-byok}.ts`, [`packages/tui/src/account/matrix-account-client.ts`](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/tui/src/account/matrix-account-client.ts) personal workspace/region quota, `packages/shared/src/llm-error-classifier.ts` plan/Credits errors, `packages/local-runtime-v2/src/service/turn-system/agent-host/assembly/{local-turn-payload-transform,messages-file-api-patcher}.ts`, Anthropic provider and lockfile | `pkg/minimax`, `pkg/cnoauth/client.go`, `service/{minimax_request_state,minimax_oauth_quota,cn_provider_quota_service,provider_files}.go`, domestic wire/quota and Files tests |
| Step-Code | `packages/providers/src/step-provider/{index,login-client}.ts`, `api/openai-completions.ts`, `utils/pi-user-agent.ts`, coding-agent `step/{defaults,environment,search-web-tool,mcp-client}.ts` | `pkg/stepfun`, `pkg/cnoauth/stepfun.go`, `service/{stepfun_*,openai_chat_roles}.go`, login panel/composable tests; remote MCP [contract audit only](providers/STEPFUN.md#remote-web-search-mcp-contract-audit) |
| ZCode | `packages/shared/src/{project-access-token-client,project-access-token,zcodeEndpoint,official-glm-model-id}.ts`, `packages/services/src/model-provider/{zaiStartPlanBilling,codingPlanProviderAvailability}.ts`, CLI `auth/coding-plan-api-key.ts`, UUID/request-trace attribution and device-mid helpers, `model/off-peak-request-auth.ts`, bootstrap `model-config.ts`, runtime-platform headers and lockfile | `pkg/zcode`, `service/{zhipu_oauth_service,zhipu_project_tokens,zhipu_offpeak_ticket,zhipu_start_plan_entitlement}.go` |

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
as `0.165.0` (stable tag commit `360319d8dee62d87e2d59fc0c21805d79519cfed`,
[published October 27](https://github.com/openai/codex/releases/tag/rust-v0.165.0));
reviewing protocol changes never enables automatic identity-version updates.

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
