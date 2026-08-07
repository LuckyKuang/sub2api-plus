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

## 7. Post-review fixes and usage-list TPS

- [x] 7.1 Preserve first-output timing fields in partial stream usage results and add regression coverage.
- [x] 7.2 Lock Antigravity non-streaming aggregate semantics: no first Token, final aggregate first output only.
- [x] 7.3 Add modality-specific labels, mixed-modality dual timing, neutral missing/media styles and estimated TPS to the shared usage table.
- [x] 7.4 Synchronize English and Chinese locale keys and add focused Vitest coverage for TPS eligibility and invalid inputs.
- [x] 7.4a Harden usage-list TPS with reliability gates (`generation_ms >= 300`, `text_output_tokens >= 8`, `TPS <= 500`), hide out-of-range estimates, update locale hints, and extend Vitest coverage for short window / few tokens / unrealistically high / boundary cases.
- [x] 7.4b Collapse latency primary column to first-token/total/TPS; move first-output modality details into hover; merge modality label helpers; update OpenSpec UI wording and Vitest.
- [x] 7.5 Run full frontend Vitest, lint, typecheck and strict OpenSpec validation.
- [x] 7.6 Run focused Go tests for timing, usage parsing, persistence and migration paths with Go 1.26.5.
- [x] 7.7 Persist `audio_output_tokens` and nullable `is_complete`; hide TPS for historical and incomplete records and cover mixed audio output.

## 8. Stream completion lifecycle hardening

- [x] 8.1 Infer `is_complete` from forwarding results when handlers do not explicitly set it; preserve `NULL` only for unknown historical/direct repository writes.
- [x] 8.2 Return partial OpenAI and Gemini stream results for missing protocol terminal, non-success terminal, client disconnect and upstream read failure, and persist those records as incomplete.
- [x] 8.3 Require WebSocket terminal-frame delivery before completing a turn; cover drain and terminal write failure.
- [x] 8.4 Propagate Gemini/Antigravity audio output token details and add focused regression coverage.
- [x] 8.5 Apply the explicit incomplete-record contract to native Gemini v1beta and Antigravity Gemini stream read failures, with handler and stream regression coverage.
- [x] 8.6 Require a real Gemini terminal for Antigravity Claude/Chat/Responses conversion and internal aggregation; never synthesize a successful client terminal or JSON response from EOF-only partial data.
