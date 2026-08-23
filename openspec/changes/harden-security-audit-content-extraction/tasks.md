## 1. Repository contract and specification

- [x] 1.1 Add the immutable security-audit coverage rule to root `AGENTS.md` at the same priority as Codex identity invariants.
- [x] 1.2 Define shared extraction, protocol coverage, fail-closed, metrics, and verification requirements in OpenSpec.
- [x] 1.3 Add the normative protocol/source coverage matrix and link protocol documentation to it.

## 2. Shared extraction

- [x] 2.1 Add the dependency-free canonical audit-content parser and real-payload unit tests.
- [x] 2.2 Move Prompt Audit protocol text extraction onto canonical segments without changing redaction, hashing, persistence, or scanner behavior.
- [x] 2.3 Move Content Moderation text extraction onto canonical current segments while preserving image handling and risk side effects.

## 3. Security behavior and observability

- [x] 3.1 Cover Responses HTTP/WS tool outputs, nested frames, Alpha Search, Live, Embeddings, and Chat/Anthropic/Gemini tool results.
- [x] 3.2 Fail closed for classified content-bearing extraction failures in blocking modes while preserving control-only frames.
- [x] 3.3 Expose extraction attempted/succeeded/empty/failed metrics for both engines and add structured logs without raw content.
- [x] 3.4 Cover reusable prompt variables and current official Responses shell, computer, MCP, programmatic-tool-calling, and dynamic tool-search item shapes.
- [x] 3.5 Audit every Responses passthrough client data frame before forwarding, sanitize media from persisted text, and classify moderation unavailability separately from policy blocks.
- [x] 3.6 Use the Live adapter for initial HTTP sessions, cover legacy/current transcription context, reject unknown request/session siblings, and fail closed on structured serialization errors.

## 4. Verification

- [x] 4.1 Add dual-engine semantic payload-contract tests and update obsolete tests that codified tool-loop skips.
- [x] 4.2 Run gofmt, focused audit/service/handler tests, OpenSpec strict validation, AGENTS format validation, and `git diff --check`.
