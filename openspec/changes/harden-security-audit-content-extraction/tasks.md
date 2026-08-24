## 1. Repository contract and specification

- [x] 1.1 Add the immutable security-audit coverage rule to root `AGENTS.md` at the same priority as Codex identity invariants.
- [x] 1.2 Define shared extraction, protocol coverage, compatibility pass-through, metrics, logging, and verification requirements in OpenSpec.
- [x] 1.3 Add the normative protocol/source coverage matrix and link protocol documentation to it.

## 2. Shared extraction

- [x] 2.1 Add the dependency-free canonical audit-content parser and real-payload unit tests.
- [x] 2.2 Move Prompt Audit protocol text extraction onto canonical segments without changing redaction, hashing, persistence, or scanner behavior.
- [x] 2.3 Move Content Moderation extraction onto a canonical-document selector while preserving risk side effects.
- [x] 2.4 Preserve canonical image attribution and restore Content Moderation to current direct-user selection without narrowing Prompt Audit coverage.

## 3. Security behavior and observability

- [x] 3.1 Cover Responses HTTP/WS tool outputs, nested frames, Alpha Search, Live, Embeddings, and Chat/Anthropic/Gemini tool results.
- [x] 3.2 Pass through unknown or failed extraction in every audit mode while continuing to audit successfully extracted sibling content.
- [x] 3.3 Expose extraction attempted/succeeded/empty/failed metrics and safe structured exception logs for both engines and all evaluation/dependency failures.
- [x] 3.4 Cover reusable prompt variables and current official Responses shell, computer, MCP, programmatic-tool-calling, and dynamic tool-search item shapes.
- [x] 3.5 Audit every Responses passthrough client data frame before forwarding, sanitize media from persisted text, and pass extraction failures through without converting them to moderation unavailability.
- [x] 3.6 Use the Live adapter for initial HTTP sessions, cover legacy/current transcription context, and pass unknown frames, siblings, and serialization failures through with safe logs.

## 4. Verification

- [x] 4.1 Add dual-engine `v0.1.177+custom.003` compatibility tests for unknown items, frames, siblings, valid-JSON structures, and extracted sibling content.
- [x] 4.2 Run gofmt, focused audit/service/handler tests, OpenSpec strict validation, AGENTS format validation, and `git diff --check` after implementation.
- [x] 4.3 Add HTTP/WS/API-key/OAuth regressions proving extraction failures pass through while confirmed policy blocks retain zero downstream side effects.
