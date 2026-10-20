# Official agent outbound baselines

Both **OAuth and API Key** accounts for each platform use its official agent
below. This is the source index for future version and outbound-protocol reviews;
it contains no developer checkout paths. Reviewed on 2026-10-20.

| Platform | Official source | Reviewed commit | Selected agent version |
| --- | --- | --- | --- |
| OpenAI | [openai/codex](https://github.com/openai/codex.git) | `e1b9c9457ceca75a99b034a5df39ac48c3998d15` | stable `rust-v0.163.0` |
| xAI | [xai-org/grok-build](https://github.com/xai-org/grok-build.git) | `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8` | `1.0.45` |
| DeepSeek | [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness.git) | `687f282e8625c6b8d11ff58211a90524be2e4317` | `0.2.1-rc.2` (prerelease) |
| Kimi | [MoonshotAI/kimi-code](https://github.com/MoonshotAI/kimi-code.git) | `67325c1b25e46b2fc66043bceab059221b719fa9` | stable `2.2.0` |
| MiniMax | [MiniMax-AI/minimax-code](https://github.com/MiniMax-AI/minimax-code) | `5821f7c9b0363a9b97a57fff3deaa4cd2da165a4` | product `0.6.5`; managed UA versionless; BYOK SDK `0.91.1` |
| StepFun | [stepfun-ai/Step-Code](https://github.com/stepfun-ai/Step-Code.git) | `be88ee77071188975475bcb5ef4e1edd0913d8ad` | product `0.84.4`; inference UA versionless; OpenAI JS `6.40.0` |
| Zhipu | [zai-org/ZCode](https://github.com/zai-org/ZCode.git) | `3c5b227649371507ddc4eb46ac5133afe6db1cab` | product source `3.16.2`; separate CLI line `0.17.0` |

ZCode's selected product version comes from current source and is paired with
the project-access-token implementation at that commit. It has no matching
published Release at review time (latest Release: `3.14.3`). DeepSeek also
explicitly follows a prerelease. Neither is represented as a stable Release.

| Agent | Official code entrypoints to review | Project owners |
| --- | --- | --- |
| Codex | `codex-rs/login/src/auth/default_client.rs`, `codex-rs/model-provider`, `codex-rs/codex-api`, auth and WebSocket clients | `service/openai_codex_identity.go`, `openai_gateway_service.go`, `openai_ws_client.go`, `pkg/openai`, complete Codex transport/identity suites |
| grok-build | `crates/codegen/xai-grok-sampler`, `xai-grok-voice`, `xai-grok-imagine`, OAuth and billing clients | `pkg/xai`, `service/grok_*`, `account_test_service_grok_test.go` |
| deepseek-harness | `packages/llm/llm/src/attribution.ts`, `llm-deepseek/src/adapter.ts`, `llm-deepseek-account/src/{listing,resolve-catalog}.ts`, account-platform and anonymous-user-id packages | `pkg/deepseek`, `pkg/cnoauth/client.go`, `service/{deepseek_request_state,domestic_agent_models,cn_oauth_service}.go` |
| kimi-code | `packages/oauth/src/{identity,oauth,managed-kimi-code,managed-usage}.ts`, `packages/agent-core-v2/src/llm-adapter` | `pkg/kimi`, `pkg/cnoauth/client.go`, `service/{domestic_agent_models,upstream_models,cn_oauth_service}.go` |
| minimax-code | `packages/oauth-core/src`, `packages/local-runtime-v2/src/service/model-system/resolution/{model-resolver-helpers,model-resolver-byok}.ts`, Anthropic provider and lockfile | `pkg/minimax`, `pkg/cnoauth/client.go`, `service/minimax_request_state.go`, domestic wire tests |
| Step-Code | `packages/providers/src/step-provider/{index,login-client}.ts`, `api/openai-completions.ts`, `utils/pi-user-agent.ts`, coding-agent `step/{defaults,environment}.ts` | `pkg/stepfun`, `pkg/cnoauth/stepfun.go`, `service/{stepfun_*,openai_chat_roles}.go`, login panel/composable tests |
| ZCode | `packages/shared/src/{project-access-token-client,project-access-token}.ts`, CLI `auth/coding-plan-api-key.ts`, `model/off-peak-request-auth.ts`, bootstrap `model-config.ts`, runtime-platform headers and lockfile | `pkg/zcode`, `service/{zhipu_oauth_service,zhipu_project_tokens,zhipu_offpeak_ticket}.go` |

Project paths in the last column are relative to `backend/internal/` unless
stated otherwise. Source layout may move; locate the named symbol in the pinned
commit rather than substituting a different agent or desktop bundle.

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
authentication. DeepSeek OAuth uses the native account `/models` catalog and
version/Messages gates; API-key `/models` discovery is a gateway extension to
Harness's credential-gated built-in catalog. ZCode project tokens are only for
linked accounts; manually entered API keys are sent unchanged.

Migration 291 removes Grok API-key foreign-family candidates/mapping; 292 gives
DeepSeek credential owners a persistent anonymous UUID; 293 removes obsolete
ZCode linked-account project secrets. They preserve manual API keys and valid
native identity pins. Older Z.ai links lacking their original OAuth token can
use a still-valid stored business token, then require reauthorization; no copied
project-key fallback is retained.
