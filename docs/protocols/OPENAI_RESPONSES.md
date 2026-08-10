# OpenAI Responses and WebSocket Ingress

Sub2API Plus accepts OpenAI-compatible Responses requests over HTTP and
client-facing WebSocket ingress. Account routing can use an upstream WebSocket
or bridge the client WebSocket to an HTTP/SSE upstream.

## Codex Rate-Limit Response Headers

For Codex Responses requests, successful HTTP and SSE responses can include
the following rate-limit fields for both `Primary` and `Secondary` windows.
WebSocket upgrade responses include the same fields when the local Codex
subscription quota view is enabled:

- `X-Codex-*-Used-Percent` is the consumed percentage.
- `X-Codex-*-Window-Minutes` is the window length in minutes.
- `X-Codex-*-Reset-At` is the next reset time as Unix seconds.
- `X-Codex-*-Reset-After-Seconds` remains alongside `Reset-At` for older
  clients that only understand a relative countdown.

When local Codex subscription quota is enabled, `Primary` represents the local
7-day window and `Secondary` represents the local rolling 5-hour window. The
gateway clears all upstream rate-limit fields before writing the local values,
so one response never mixes upstream reset times with local percentages or
window sizes. With the local view disabled, eligible upstream headers are
passed through unchanged for HTTP and SSE responses.

A client WebSocket `101` response is committed before the gateway connects to
the selected upstream. The gateway therefore writes only the local quota view
known before the upgrade. When that view is disabled, it does not inject Codex
quota headers into the `101` response and cannot pass through headers from a
later upstream handshake. HTTP and SSE headers are finalized before their
response bodies are written.

This response-header compatibility does not make Codex App API-key calls to
`account/rateLimits/read` available; that App Server authentication behavior is
outside this gateway's request path.

## Request Replay and Upstream Failures

When a Responses request replays a previous tool call, the gateway preserves an
item `id` only when its prefix matches the item type. In particular,
`custom_tool_call` uses `ctc...`; a mismatched item ID is removed rather than
rewritten. Its `call_id` remains unchanged so the paired
`custom_tool_call_output` continues to reference the original call.

The exact local proxy response `507` with
`exceeded request buffer limit while retrying upstream` is not an account or
model failure. The gateway stops replaying the request, keeps the selected
account eligible, and returns an OpenAI-compatible `413` with:

```text
Request payload is too large to retry safely
```

Reduce the request size or adjust the reverse-proxy retry-buffer policy before
retrying. The gateway does not retry that request through another account,
because doing so can duplicate work and billing while encountering the same
buffer limit.

Connection refusal/reset and HTTP `504` gateway failures are recorded as
provider or proxy transport failures rather than model-capacity failures. A
proxy-backed OpenAI account is isolated by the bounded proxy circuit, keyed by
the configured proxy ID; a shared proxy incident therefore does not directly
put every associated account into an account-level cooldown. Management error
records retain only structured, bounded diagnostic categories and never store
raw proxy URLs, credentials, or outbound User-Agent values.

For the native HTTP/SSE and WebSocket paths, diagnostics distinguish an edge
gateway timeout from the gateway's own response-header, first-semantic-output,
and WebSocket first-semantic-output deadlines. A first-output timeout may use
the existing single, pre-output controlled failover path; it is never enabled
after semantic output has reached the client.

## WebSocket Ingress Limits

`gateway.openai_ws` bounds the lifetime and aggregate count of client-facing
sessions independently from per-turn user and account concurrency:

```yaml
gateway:
  openai_ws:
    client_first_message_timeout_seconds: 30
    ingress_inter_turn_idle_timeout_seconds: 300
    max_ingress_connections_per_api_key: 64
```

- The first-message timeout covers receiving and decompressing the complete
  first client message.
- The inter-turn timeout closes idle sockets after a completed turn; `0`
  disables it.
- The API-key connection cap is distributed through Redis; `0` disables it.

Large contexts or slow image-heavy requests may require a higher first-message
timeout. The timeout expires before HTTP bridge routing and is not overridden
by bridge mode.

Distributed connection leases last 60 seconds and refresh every 20 seconds. If
a process cannot confirm a lease for a full lease lifetime, it closes the local
socket instead of continuing outside the global cap.

## Mode Router

Enable the v2 mode router before selecting an account WebSocket mode such as
`http_bridge`:

```yaml
gateway:
  openai_ws:
    mode_router_v2_enabled: true
```

The environment equivalent is
`GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true`. Use `http_bridge` when the
client keeps a WebSocket while the selected upstream uses HTTP/SSE.
