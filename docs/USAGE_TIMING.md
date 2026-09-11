# Usage timing

The admin and user usage tables and exports use one timing contract: first
token, total duration, and estimated TPS. Credential type does not select a
different definition.

## Fields and display

- `timing_version=1` identifies the strict sampler, including requests that
  produced no token-like output. Existing rows remain `0` (unverified); neither
  a non-null first-output kind nor a stored first-event time proves strict timing.
- `first_token_ms` measures from the existing forwarding start (the turn start
  for WebSocket) to the first non-empty text, reasoning, or tool token-like
  output. Metadata, keepalive, usage, empty deltas, image/audio bytes, signatures,
  and terminal aggregate output do not start this clock.
- `last_token_ms` uses the same origin and the last token-like output. WebSocket
  response IDs retain separate windows, including interleaved turns.
- `duration_ms` retains its existing forwarding/turn duration. It is not a new
  measurement of the full client-perceived request, including scheduling.
- TPS is `(output_tokens - image_output_tokens - audio_output_tokens) * 1000 /
  (last_token_ms - first_token_ms)`. First-token and last-token times share the
  same forwarding/turn origin, so thinking wait is excluded from the denominator
  (it remains visible as first token) and post-token flush is not included. It is
  an estimate from billed upstream token usage, including any reasoning tokens
  folded into `output_tokens`; it is a decode-window rate, not a peak renderer
  rate and not an effective rate that re-averages thinking wait. Version-1 rows
  with billed text tokens and a positive decode window still display a number for
  incomplete, non-streaming, and short samples; those cases add a confidence note
  instead of `—`. A decode window is short when
  `last_token_ms - first_token_ms < 300` or text tokens are below 8. Missing
  first/last timestamps, a non-positive window, Live summaries, compaction-only
  results, invalid counts, and unverified history remain `—`.

Compaction-only results remain excluded even when upstream reports billed output
tokens and a total duration. A response that starts with compaction and later
produces observable text-like tokens is eligible. Very low positive TPS values
retain significant digits instead of rounding to zero in the table.

Historical first-event values remain stored but are not presented as first
token or used for TPS. Tooltips/export reasons distinguish unavailable history,
Live summaries, missing billed text tokens, missing first/last timestamps,
non-positive decode windows, invalid counts, and low-confidence
incomplete/non-stream/short samples. Total duration remains available for these
records.

Gemini frames may contain multiple parts or candidates. The observer preserves
the first meaningful output kind while scanning all parts for token-like output;
an image, audio part, signature, or execution result must not hide a subsequent
text/reasoning/tool delta in the same frame. TPS rejects negative/non-finite
token counts and modality totals greater than the total output token count.

Antigravity's Gemini-to-Claude stream samples native Gemini output before
conversion. Markdown synthesized from image bytes and text synthesized from
grounding metadata cannot start or extend the token clock. A signature followed
by text in the same converted SSE batch must retain both the first meaningful
output kind and the later token observation; the generic Anthropic SSE observer
scans the entire batch rather than stopping at its first meaningful event.

The Gemini-to-Messages bridge extends the token window for each non-empty tool
argument delta, including later chunks of an already open tool block. Repeated
arguments, empty input, terminal metadata, and synthesized tool names do not
start or extend that window.

Native Anthropic-to-Chat/Responses adapters distinguish a real upstream
`message_stop` from a completion synthesized by converter finalization. Missing
upstream completion or an upstream error marks usage incomplete, even when the
existing converter still closes the downstream stream normally. Partial usage,
observed first-token timing, and estimated TPS remain available, with a
confidence note on incomplete rows.
Chat-to-Messages/Responses fallback streams retain their existing requirement
for a real `[DONE]`. An upstream error followed by `[DONE]` is also marked as
incomplete usage; the final sentinel does not erase a preceding failure.

Remote Codex compaction is not an exception to this rule. An encrypted
`compaction`/`compaction_summary` result is meaningful output, recorded as
`first_output_kind=compaction`, but is not token-like output. A compact JSON
response cannot yield a measured token rate; it displays `—` for unavailable
first token/TPS. A response that actually emits text-like deltas can be sampled
normally. Compaction bytes are never counted as text tokens.

## Account and protocol coverage

| Platform | Account variants | Timing path |
| --- | --- | --- |
| Anthropic | OAuth, setup token, API key (including passthrough) | Anthropic output observer; Chat adapter uses the same token clock |
| Anthropic on Bedrock | SigV4 and Bedrock API key | Decoded Anthropic event observer |
| Anthropic on Vertex | Service account | Anthropic event observer |
| OpenAI/Codex | OAuth and API key | Responses HTTP normal/passthrough, Chat/Messages adapters, WS pooled/bridge/passthrough; compact aggregate handling |
| Gemini | OAuth variants, API key, Vertex service account | Gemini output observer and Chat/Messages adapters |
| Antigravity | OAuth and upstream | Claude/Gemini observers and compatibility adapters |
| Grok | API key | Raw Chat observer or unified Responses HTTP handler |
| Kimi, Zhipu, DeepSeek | API key, supported pay-as-you-go/coding configurations | Selected native Responses/Chat/Anthropic protocol and its adapters |

Composite groups route to an actual account and inherit that account's sampler.
The historical Kiro constant does not define a supported additional forwarding
implementation. Account variants cannot restore first-event timing through a
setting. Not every platform supports every credential/endpoint combination.

Codex Live records summarize a session, without an observed token window. Gemini
batch-image records summarize settlement, also without token timing. Both new
record paths identify their timing contract as version 1 with null token times;
Live duration remains session duration, and batch settlement does not invent a
generation duration. Image/audio/video-only operations cannot yield text TPS.

## Upgrade and API changes

Migration 257 adds `usage_logs.timing_version` with default 0 and extends the
first-output kind constraint. Gateway-created records are stamped 1; externally
supplied usage measurements without this provenance remain unverified.
The obsolete `openai_ttft_mode` setting is removed from storage, admin settings
API, and UI. No semantic/visible compatibility switch remains.

Operations and channel-monitor TTFT queries accept only strict records. Migration clears historical
derived TTFT aggregates (not request, token, cost, or latency totals); subsequent
aggregation uses verified records. Raw history is preserved. Scheduler algorithms
are unchanged and receive the corrected first-token observations for new calls.

Timing observation does not change authentication, account selection, payload
transformation, billing, or security-audit extraction/order. Meaningful compact
output participates in the existing first-output observation just like other
aggregate output, without inventing a token clock.

Regression fixtures cover protocol observations, HTTP normal/passthrough,
compaction, native Anthropic adapters, WS turn isolation, repository parameters,
and frontend/export calculations. These fixtures are not evidence of live calls
with every provider's real credentials.
