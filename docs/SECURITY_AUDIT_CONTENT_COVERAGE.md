# Security Audit Content Coverage

This document is the normative content-extraction matrix for Content
Moderation and Prompt Audit. The shared implementation is
`backend/internal/auditcontent`; protocol handlers and account paths must not
maintain alternate text extractors.

## Boundary And Ordering

Every accepted HTTP request, WebSocket turn, and Live Sideband client frame
must cross the same security-audit boundary after authentication and basic
request validation, but before:

1. account selection or API-key/OAuth credential normalization;
2. billing, quota reservation, or concurrency acquisition;
3. routing, retry, probe, fingerprint, or protocol transformations; and
4. any upstream request or frame write.

Session affinity, account type, inbound role labels, envelope `type` values,
and protocol adapters cannot bypass the audit hook. Extraction remains
compatible with `v0.1.177+custom.003`: unsupported or unrecognized content may
produce no audit input and pass through, while every successfully extracted
sibling segment remains auditable.

## Canonical Result

The shared extractor returns a protocol-independent document containing text
segments and image inputs. Both carry `Role`, `Source`, `Current`, and
`ClientControlled`; the document also carries `ContentBearing` and `Incomplete`
classifications.

- `Current` identifies the new content in this request or turn. Consecutive
  trailing tool results are all current.
- `ClientControlled` is independent of the claimed role. A current
  `assistant` or `model` message remains client-controlled inbound content.
- Structured tool arguments and results are encoded as deterministic JSON.
- Recognized media blocks are explicit no-text content. Images remain in the
  canonical result with the same role, source, and current-turn attribution as
  their containing item. Prompt Audit does not scan URLs or encoded media as
  prompt text. Structured results are sanitized before text
  serialization so image/file URLs, data URLs, long base64 payloads, encrypted
  compaction data, screenshots, and image-generation results are not persisted;
  ordinary text beside those fields remains auditable.
- Any non-empty recognized content item that cannot be completely normalized
  may set `Incomplete` for metrics and structured logs. `Incomplete` does not
  discard successfully extracted sibling segments and does not itself block,
  return an unavailable decision, or change an empty selection into a policy
  violation.

## Protocol Matrix

| Protocol family | Canonical text sources | Current-content rule | Explicit no-text or control cases |
| --- | --- | --- | --- |
| OpenAI Chat Completions | `instructions`; `tools` and `functions`; `messages[].content`; `tool_calls[].function.arguments`; `function_call.arguments`; tool/function-role results, including structured content | Last message is current; if the tail contains tool/function results, every consecutive trailing result is current; system/developer context is current audit context | Recognized image/video content blocks |
| Anthropic Messages | `system`; `tools`; message text and thinking text; client/server tool-use input; tool-result content, including structured content | Last message is current; system and tool definitions remain current audit context | Recognized image blocks and encrypted `redacted_thinking` blocks |
| OpenAI Responses HTTP and WebSocket | Top-level, `response`-nested, or session-update `instructions`, `tools`, `input`, and reusable `prompt.variables`; message/reasoning/refusal text; function/custom/tool-search outputs; local/hosted shell, apply-patch, computer, MCP, code-interpreter, program/program-output, additional-tools, and accepted search call payloads | Last input item is current; every consecutive trailing recognized output is current; `tool_search_output.tools` and other dynamic definitions are current context; a claimed system/developer role remains context and all other current roles remain client-controlled | Recognized media/opaque items and control envelopes produce no text; unknown frames, item types, sibling fields, and valid-JSON unrecognized structures pass through without an audit-derived block |
| OpenAI Live | Initial session instructions, tools, input, legacy `input_audio_transcription.prompt`/`keywords`, current `audio.input.transcription.prompt`/`keywords`; `session.update`; `transcription_session.update`; `conversation.item.create`; Live-shaped `response.create` | Every initial HTTP session and accepted Sideband client frame enters the audit hook before its downstream side effect or upstream write | Known control events and unknown frames, session/config fields, item types, or valid-JSON structures may produce no audit input and pass through without an audit-derived close |
| Alpha Search | `commands.search_query[].q` and Responses-shaped top-level `input` | Every query is current; Responses input uses the Responses current-item rule | Empty query and input collections |
| OpenAI Embeddings | String or string-array `input` | Every string input is current | Empty input and unsupported token-ID arrays produce no audit text and pass through |
| Gemini | `systemInstruction`/`system_instruction`; tools; `contents`/`content`; batched `requests`; `instances[].prompt`; part text; `functionCall` arguments; `functionResponse.response` | Last content item is current; system and tools remain current audit context | `inlineData`/`fileData` media-only parts |
| Images and media | Deterministic prompt-like keys such as prompt, description, query, lyrics, negative prompt, and input | Every extracted prompt is current; duplicate text is emitted once | HTTP(S) URLs, `data:image`/`data:video` values, and large base64-like media payloads |

For unknown protocol labels, the fallback recognizes Chat-shaped `messages`,
Responses-shaped `input` or `instructions`, Gemini-shaped content, Alpha
Search commands, and finally the media prompt allowlist. Unrecognized values
pass through; a newly accepted content field needs an explicit adapter before
it becomes auditable by both engines.

An envelope `type` value or empty top-level field never overrides content
present elsewhere in the payload. In particular, a non-`response.create` type
carrying `input`, `instructions`, or nested `response.input` is still
content-bearing, and top-level plus nested Responses fields are both inspected.
An unsupported envelope type is still counted and safely logged as an
extraction failure even when those sibling fields are extracted successfully.
Likewise, a media type label does not suppress recognized text in the same
content block. Unknown sibling keys are ignored, and unsupported item types,
unknown Responses/Live frames, valid-JSON unrecognized structures, and other
incomplete or unextractable content pass through. Successfully extracted
sibling content remains available to both engines. Responses passthrough and
Live Sideband still invoke the audit hook before every upstream frame write;
an extraction failure alone does not prevent that write.

A non-empty Responses or Live root, nested `response`, or session object
containing no recognized request, control, content, or metadata field is an observable
extraction failure rather than an ordinary empty request. Both engines count
and safely log that failure before allowing it. Unknown sibling keys remain
ignored when the containing object has at least one recognized field.

## Engine Selection

Both engines consume the same canonical document:

| Engine/mode | Segment selection |
| --- | --- |
| Content Moderation | Scans only current direct-user text and images. Chat and Anthropic require an explicit `user` role; Responses, Live, and Gemini also accept their protocol-defined roleless user forms. Direct Alpha Search queries, embedding strings, and media prompts remain eligible. Instructions, system/developer context, reusable prompt variables, assistant/model messages, reasoning, tool definitions/calls/results, approval responses, and tool-produced images are excluded so platform or external content is not attributed to the user. |
| Prompt Audit full/async | Scans conversation text from the canonical document: messages, instructions/system context, reusable prompt variables, reasoning, and search/embedding/media prompts. It excludes tool/function definitions, structured tool-call arguments, and tool/function outputs so platform schemas and tool results are not treated as user prompts. |
| Prompt Audit blocking latest-turn-only | Restores the `v0.1.177+custom.003` narrow scan: the latest user text plus the nearest preceding assistant/model output. Instructions, tool definitions, older turns, and structured tool calls are omitted from the blocking input. |

Sharing a canonical document does not mean that the engines select identical
segments. Content Moderation preserves the `v0.1.177+custom.003` attribution
rule: only a direct user submission may produce a user content-policy
violation. Prompt Audit keeps the same version's conversation-text policy:
ordinary `hi` plus a client tool schema must not become a jailbreak block, while
jailbreak text in user/system/assistant/tool message content remains auditable.
A turn containing only a tool result or tool schema is a valid empty Prompt
Audit selection. Incomplete canonical extraction is observable but does not
override either engine's selection policy: extracted content is still
evaluated, while an empty selection passes through.

## Failure Semantics

All enabled engine paths expose `extraction_attempted`,
`extraction_succeeded`, `extraction_empty`, and `extraction_failed` counters.
Every extraction, evaluation, or audit-dependency exception emits a structured
log containing request ID, endpoint, protocol, stage, a stable error
code/reason, available byte counts, and bounded incomplete reasons. Logs must
not contain raw content, credentials, or unsanitized user fields. Extraction
failure is an observability outcome, never a policy violation or an unavailable
decision.

Content Moderation applies the same log contract to asynchronous persistence,
hash-cache, account-side-effect, notification, worker, cleanup, runtime, and
post-upstream cyber-policy failures. These logs use stable error categories;
they do not include raw dependency errors, panic values, or recipient email
addresses. Prompt Audit applies it to enqueue, payload-store, job-claim,
lease-refresh, completion/retry/failure persistence, worker, reclaim, startup,
shutdown, and runtime health failures.

| Engine/mode | Content-bearing extraction failure |
| --- | --- |
| Content Moderation observe | Record failure; evaluate any selected extracted content; otherwise allow |
| Content Moderation pre-block | Record failure; evaluate any selected extracted content; otherwise allow without HTTP 503 |
| Prompt Audit async | Record failure; enqueue successfully extracted content or skip an empty snapshot; never affect request forwarding |
| Prompt Audit blocking | Record failure; evaluate successfully extracted content or allow an empty snapshot without HTTP 503 |

A confirmed policy match continues to use `content_policy_violation` or the
Prompt Audit block decision. Extraction failure alone must not become a policy
block, an unavailable decision, HTTP 503, or WebSocket close. Independent audit
dependency failures retain their documented availability behavior.

Deterministic structured serialization is part of extraction. Sanitization or
JSON serialization failure may set `Incomplete` and omit that value, but does
not discard successfully extracted siblings or block the request by itself.

Live Sideband and Responses passthrough are control connections: every client
text or binary frame enters the audit hook before `upstream.WriteFrame`.
Unsupported, binary, or otherwise unextractable content passes through unless
independent transport/basic validation rejects it. WebRTC media does not
traverse the Sideband control connection and is outside this text-extraction
boundary.

## Change Evidence

Any endpoint, accepted payload field, tool form, role rule, control event, or
protocol transform that can affect inbound content must update this matrix in
the same change and provide all of the following evidence:

- production-shaped shared-extractor tests;
- the dual-engine contract in
  `backend/internal/handler/security_audit_content_contract_test.go`;
- Content Moderation and Prompt Audit payload/selection tests;
- HTTP and WebSocket ordering tests proving extraction failures pass through,
  while confirmed policy blocks still produce zero account, billing,
  concurrency, or upstream side effects; and
- Live lifecycle tests when Sideband classification or forwarding changes.

Route-call presence or static source-order assertions alone do not prove
content coverage.
