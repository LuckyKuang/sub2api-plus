# Security Audit Content Coverage

This document is the normative content-extraction matrix for Content
Moderation and Prompt Audit. The shared implementation is
`backend/internal/auditcontent`; protocol handlers and account paths must not
maintain alternate text extractors.

## Boundary And Ordering

Every content-bearing HTTP request, WebSocket turn, and Live Sideband client
frame must cross the same security-audit boundary after authentication and
basic request validation, but before:

1. account selection or API-key/OAuth credential normalization;
2. billing, quota reservation, or concurrency acquisition;
3. routing, retry, probe, fingerprint, or protocol transformations; and
4. any upstream request or frame write.

Session affinity, account type, inbound role labels, envelope `type` values,
and protocol adapters are untrusted routing data. None of them may suppress
content that is present in an accepted payload.

## Canonical Result

The shared extractor returns a protocol-independent document containing
segments with `Text`, `Role`, `Source`, `Current`, and `ClientControlled`, plus
`ContentBearing` and `Incomplete` classifications.

- `Current` identifies the new content in this request or turn. Consecutive
  trailing tool results are all current.
- `ClientControlled` is independent of the claimed role. A current
  `assistant` or `model` message remains client-controlled inbound content.
- Structured tool arguments and results are encoded as deterministic JSON.
- Recognized media blocks are explicit no-text content. Content Moderation
  preserves its separate image inputs; Prompt Audit does not scan URLs or
  encoded media as prompt text. Structured results are sanitized before text
  serialization so image/file URLs, data URLs, long base64 payloads, encrypted
  compaction data, screenshots, and image-generation results are not persisted;
  ordinary text beside those fields remains auditable.
- Any non-empty recognized content item that cannot be completely normalized
  sets `Incomplete`, even when sibling items produced valid segments. Partial
  extraction is an extraction failure, not a successful or empty request.

## Protocol Matrix

| Protocol family | Canonical text sources | Current-content rule | Explicit no-text or control cases |
| --- | --- | --- | --- |
| OpenAI Chat Completions | `instructions`; `tools` and `functions`; `messages[].content`; `tool_calls[].function.arguments`; `function_call.arguments`; tool/function-role results, including structured content | Last message is current; if the tail contains tool/function results, every consecutive trailing result is current; system/developer context is current audit context | Recognized image/video content blocks |
| Anthropic Messages | `system`; `tools`; message text and thinking text; client/server tool-use input; tool-result content, including structured content | Last message is current; system and tool definitions remain current audit context | Recognized image blocks and encrypted `redacted_thinking` blocks |
| OpenAI Responses HTTP and WebSocket | Top-level, `response`-nested, or session-update `instructions`, `tools`, `input`, and reusable `prompt.variables`; message/reasoning/refusal text; function/custom/tool-search outputs; local/hosted shell, apply-patch, computer, MCP, code-interpreter, program/program-output, additional-tools, and accepted search call payloads | Last input item is current; every consecutive trailing recognized output is current; `tool_search_output.tools` and other dynamic definitions are current context; a claimed system/developer role remains context and all other current roles remain client-controlled | Recognized image/file/video items, computer screenshots, image-generation binary results, encrypted reasoning/compaction without summary text, item references, and an accepted control envelope only when it contains no canonical content fields |
| OpenAI Live | Initial session instructions, tools, input, legacy `input_audio_transcription.prompt`/`keywords`, current `audio.input.transcription.prompt`/`keywords`; `session.update`; `transcription_session.update`; `conversation.item.create`; Live-shaped `response.create` | Every initial HTTP session and accepted Sideband client frame uses the `openai_live` adapter and is evaluated independently before its downstream side effect or upstream write | Known audio-buffer, cancel, retrieve, truncate, delete, clear, and close events; unknown event or non-empty session/config fields are content-bearing until explicitly classified |
| Alpha Search | `commands.search_query[].q` and Responses-shaped top-level `input` | Every query is current; Responses input uses the Responses current-item rule | Empty query and input collections |
| OpenAI Embeddings | String or string-array `input` | Every string input is current | Empty input; token-ID arrays are not no-text and are extraction failures because this layer cannot decode them reliably |
| Gemini | `systemInstruction`/`system_instruction`; tools; `contents`/`content`; batched `requests`; `instances[].prompt`; part text; `functionCall` arguments; `functionResponse.response` | Last content item is current; system and tools remain current audit context | `inlineData`/`fileData` media-only parts |
| Images and media | Deterministic prompt-like keys such as prompt, description, query, lyrics, negative prompt, and input | Every extracted prompt is current; duplicate text is emitted once | HTTP(S) URLs, `data:image`/`data:video` values, and large base64-like media payloads |

For unknown protocol labels, the fallback recognizes Chat-shaped `messages`,
Responses-shaped `input` or `instructions`, Gemini-shaped content, Alpha
Search commands, and finally the media prompt allowlist. A newly accepted
protocol or content field must receive an explicit adapter when the fallback
cannot prove its classification.

An envelope `type` value or empty top-level field never overrides content
present elsewhere in the payload. In particular, a non-`response.create` type
carrying `input`, `instructions`, or nested `response.input` is still
content-bearing, and top-level plus nested Responses fields are both inspected.
Likewise, a media type label does not suppress text that is present in the same
content block. Known message, item, and control types have explicit allowed
fields; an unknown non-empty sibling makes the extraction incomplete even when
another field was extracted successfully. Responses passthrough audits every
client text or binary frame, including `conversation.item.create` and
`session.update`, before a non-`response.create` frame can be written upstream.
Responses HTTP roots, nested `response` objects, and Responses/Live session
objects apply the same sibling rule. Live transcription objects are inspected
recursively so a recognized prompt cannot hide an unsupported sibling.

## Engine Selection

Both engines consume the same canonical document:

| Engine/mode | Segment selection |
| --- | --- |
| Content Moderation | Scans current segments, with current messages and tool results placed before repeated context; image extraction follows the same current rule, including consecutive Chat/Responses tool outputs, nested Anthropic tool results, reusable prompt variables, and computer screenshots |
| Prompt Audit full/async | Scans all canonical segments subject to existing size, redaction, and persistence rules |
| Prompt Audit blocking latest-turn-only | Prioritizes current client-controlled segments, then current instructions/tool definitions and the nearest relevant prior assistant/model output; it never trusts the inbound role to select an older user turn |

## Failure Semantics

All enabled engine paths expose `extraction_attempted`,
`extraction_succeeded`, `extraction_empty`, and `extraction_failed` counters.
Extraction logs contain structured request metadata and byte counts, never raw
content.

| Engine/mode | Content-bearing extraction failure |
| --- | --- |
| Content Moderation observe | Record failure and allow the request to continue without treating partial text as complete coverage |
| Content Moderation pre-block | Reject before side effects with HTTP 503 and `content_moderation_unavailable` |
| Prompt Audit async | Record failure/drop and do not enqueue an empty audit task |
| Prompt Audit blocking | Reject before side effects using the existing invalid-response HTTP 503 envelope |

A policy match continues to use `content_policy_violation`; extraction
unavailability is a coordinator `unavailable` decision and must never be
represented as a policy block or policy violation. On WebSocket
paths, audit unavailability closes with 1013 Try Again Later, while a confirmed
policy block keeps the policy close behavior.

Deterministic structured serialization is part of extraction. Sanitization or
JSON serialization failure sets `Incomplete`; it must never silently discard a
recognized structured value.

Live Sideband and Responses passthrough are control connections: every client
text or binary frame enters the audit hook before `upstream.WriteFrame`. Binary
or otherwise non-JSON content fails extraction in blocking mode. WebRTC media
does not traverse the Sideband control connection and is outside this
text-extraction boundary.

## Change Evidence

Any endpoint, accepted payload field, tool form, role rule, control event, or
protocol transform that can affect inbound content must update this matrix in
the same change and provide all of the following evidence:

- production-shaped shared-extractor tests;
- the dual-engine contract in
  `backend/internal/handler/security_audit_content_contract_test.go`;
- Content Moderation and Prompt Audit payload/selection tests;
- HTTP and WebSocket ordering tests proving zero account, billing, concurrency,
  or upstream side effects after a blocking decision; and
- Live lifecycle tests when Sideband classification or forwarding changes.

Route-call presence or static source-order assertions alone do not prove
content coverage.
