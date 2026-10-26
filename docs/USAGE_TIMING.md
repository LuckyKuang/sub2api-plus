# Usage timing

The admin and user usage tables and exports use one timing contract: first
byte, first token, total duration, and request-average TPS. Credential type does not select a
different definition.

## Fields and display

WebSocket relay receipt timestamps are captured immediately after the upstream frame read,
before local transformations and quota/error callbacks. Strict token observations keep their
existing boundary; callback delays must not inflate first-byte timing.

- `first_byte_ms` measures from the existing forwarding origin to the first
  upstream body `Read(n > 0)`, before decompression, SSE/JSON parsing and adapters.
  Response headers, zero-byte reads and locally generated keepalives do not
  count. Positive data returned with EOF counts. Non-streaming responses use
  the same read boundary. This is body data latency, not browser header TTFB.
- WebSocket uses the first application frame attributed to that turn (including
  response.created); handshakes, ping/pong and other turns are excluded. This is
  frame precision, not TCP byte precision. Retries replace the discarded HTTP
  attempt observation; new account forwards and WS turns own separate timing.
- Historical, unobservable, Live-summary and asynchronous batch-settlement rows
  retain NULL. Zero is valid. First token/output/duration never fill missing
  first-byte data. The additive API field is independent from timing_version.

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
- Average TPS is `(output_tokens - image_output_tokens - audio_output_tokens) *
  1000 / duration_ms`. It uses raw millisecond precision and includes first-token
  waiting, pauses and stream cleanup within the existing forwarding/turn duration.
  It does not measure full client elapsed time or pure model decode speed.
  Billed output can include hidden reasoning/tool tokens; excluding media does
  not turn the remainder into a count of visible answer text.
- First/last-token timestamps and `timing_version` are not prerequisites for an
  ordinary text request average. Historical records with valid counts/duration
  are eligible while strict first-token display remains unavailable. Missing,
  equal or inconsistent token-event timestamps cannot shrink the denominator.
- Non-streaming and incomplete records with usable counts/duration remain
  eligible. Incomplete records carry an explicit partial-result note. Very short
  requests and small output counts are not suppressed by fixed duration/token
  gates; no rate cap or artificial minimum denominator is applied.
- Live summaries, compaction-only results, known image/audio-only results and
  zero non-media output display `-`. A compaction/media first output followed by
  a verified token-like delta can yield an average from the non-media count.
  Invalid/non-finite or negative counts, modality totals exceeding output, and
  non-positive/non-finite durations also display `-`. Absent legacy modality
  counts default to zero; other invalid data is not repaired.

The main latency cell retains **首字 / 总耗时 / TPS** (First Byte / Total / TPS
in English). First-byte text and the color bar use first_byte_ms. Details always retain the
independent First Token / Last Token labels (including missing values), even
for ordinary text. The rate hint explains that TPS is a request average. Hovering the
rate explains its formula and, where applicable, the unavailable
reason or incomplete-result note. Strict first-token unavailable reasons remain
independent: a missing historical token clock does not invalidate the average.
Very low positive values retain significant digits rather than rounding to zero.

User CSV headers are now `Average TPS` and `Average TPS note` instead of `TPS`
and `Unavailable reason`. Admin Excel uses their localized equivalents. Both
export the shared unrounded numeric average, an empty unavailable rate, and the
same reason/partial-result note as the table. Consumers identifying columns by
header must update their mappings. Counts, token timing and TPS retain their
existing meanings; first-byte timing adds an independent nullable API field.

## Operations output TPS distribution

`output_tps` applies the same request-average formula and eligibility as usage
tables: `r = (output_tokens - image_output_tokens - audio_output_tokens) * 1000 /
duration_ms`. Each eligible request has equal weight. `avg = sum(r) / N`; it is
not `sum(tokens) / sum(duration)`. P5, P10 and P50 are continuous percentiles of
the unrounded rates over the complete selected window, including in preaggregated
dashboard mode. For sorted rates, position `(N - 1) * p` is interpolated between
adjacent samples. Rates `[1, 10, 100, 1000]` produce P5=2.35, P10=3.7, P50=55
and avg=277.75. Empty samples give `sample_count=0` and null rates; query timeout
leaves the entire distribution unavailable.

Historical/unknown request types and incomplete records remain eligible when
their counts and duration are usable. Image count and billing mode alone do not
exclude a mixed response. Image/audio token counts are subtracted, and a known
media/compaction first output (or native compaction flag) requires strict observed
token timing to establish later non-media output. Live summaries are excluded.
The existing traffic `tps` measures aggregate token throughput and retains its
separate definition. Display rounding never changes aggregation; very low
positive request rates retain significant digits in both tables and dashboard.

## Why the denominator is total duration

A long wait followed by burst output can place all observed token events within
milliseconds. Dividing the entire billed output by `duration_ms - first_token_ms`
or `last_token_ms - first_token_ms` can yield enormous values. Event arrival
spacing does not establish the upstream token generation timeline, especially
with hidden reasoning, buffering and batched events. For example, a synthetic
request with 500 output tokens over 10000ms and token events at 9990ms/9991ms
has a request average of 50 tok/s. The other windows produce 50000/500000 tok/s.
First/last-token fields remain available for diagnosis, but neither window is
used for the displayed average. Streaming averages generally decrease on upgrade;
this is a metric-definition change, not evidence that the model became slower.

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
observed first-token timing, and request-average TPS remain available, with an
incomplete-result note.
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
| Cline, Command Code, OpenCode | API key, supported provider modes | Provider profile selects the native protocol; adapters preserve the same timing and non-media TPS definition |

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

Migration 285 adds nullable first_byte_ms to usage_logs and V1 probe histories,
and independent probe rollup sum/count fields. No historical timing is inferred.
Admin Excel and user CSV add an independent First Byte column and preserve the
First Token column, strict-token eligibility and existing sorting API semantics.
V1 keeps non-streaming probes, its total latency and HEAD Ping. See CHANNEL_MONITOR.

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

## Failed-request timing

Error records retain strict TTFT from a failed partial OpenAI Responses/Messages
result when a non-empty text, reasoning or tool token was actually observed.
Failure does not erase an observation, and a metadata/keepalive/media-only
attempt still has null TTFT. Timing is reset on a new forwarding attempt and
snapshotted per WebSocket turn, so another account's failure cannot borrow it.
Historical unobserved timing stays null. Upstream attempt diagnostics separately
record whether semantic output was committed and why replay was suppressed;
media/compaction commitment can prohibit replay without starting a token clock.
This does not change the request-average TPS definition or partial-usage status.


## V1 active streaming probes

V1 `first_text_ms` is separate from usage `first_byte_ms` and strict `first_token_ms`.
It measures the first non-whitespace assistant text delta, excluding reasoning,
tools, metadata and keepalives. Probe total duration ends at normal SSE completion;
a later error retains measured times but fails the probe. JSON probes and historical
rows have no text timing. Successful text samples have their own average/count;
zero is observed, NULL is absent. See [channel monitoring](CHANNEL_MONITOR.md#v1-流式探测).

## Console observation and overview

See [UI_INTERACTION](UI_INTERACTION.md) for the owner-scoped live observation API,
user-timezone overview queries and historical insertion boundaries. Observed HTTP
`finished` is not semantic success or settled billing. Elapsed observation time
is distinct from forwarding `duration_ms`; unavailable TPS remains unavailable.
The existing strict first-token, first-byte and equal-weight average TPS contracts
are unchanged.
