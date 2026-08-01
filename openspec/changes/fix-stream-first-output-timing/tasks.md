## 1. Contract and persistence

- [x] 1.1 Add the first-output observation contract and focused unit tests.
- [x] 1.2 Add forward-only usage log migration and Ent fields, then regenerate Ent and Wire.
- [x] 1.3 Propagate first output fields through result models, usage persistence, queries and DTOs.

## 2. OpenAI HTTP and conversion paths

- [x] 2.1 Fix raw Responses and guarded/failover stream timing without coupling metric state to commitment.
- [x] 2.2 Fix raw Chat and Chat→Responses fallback chunk classification.
- [x] 2.3 Measure Responses→Chat after conversion and reject unsupported image conversion instead of silently dropping it.
- [x] 2.4 Fix direct Images and OAuth Responses image bridge partial/final timing.

## 3. WebSocket paths

- [x] 3.1 Replace event-type-only timing in HTTP→WS, WS ingress and HTTP bridge paths.
- [x] 3.2 Release buffered image partial output immediately.
- [x] 3.3 Fix WS v2 terminal classification and per-turn request start binding.
- [x] 3.4 Cover first and subsequent turns, terminal-only output, disconnect and drain behavior.

## 4. Other providers

- [x] 4.1 Apply strict Anthropic/Bedrock output observation.
- [x] 4.2 Apply strict Gemini and Antigravity output observation.

## 5. Consumers and UI

- [x] 5.1 Keep scheduler and Ops TTFT samples token-only, excluding legacy semantics without discarding raw history.
- [x] 5.2 Add UsageLog API fields and modality-aware latency rendering/CSV and Excel export.
- [x] 5.3 Synchronize English and Chinese locales and frontend tests.

## 6. Verification

- [x] 6.1 Run focused Go tests for observers, images, conversion and WebSocket relay.
- [x] 6.2 Run migration/repository checks and generated-code checks.
- [x] 6.3 Run backend unit checks and frontend lint/typecheck/Vitest coverage for changed components.
- [x] 6.4 Validate this OpenSpec change strictly and record any environment limitations.
