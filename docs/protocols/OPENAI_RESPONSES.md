# OpenAI Responses and WebSocket Ingress

Sub2API Plus accepts OpenAI-compatible Responses requests over HTTP and
client-facing WebSocket ingress. Account routing can use an upstream WebSocket
or bridge the client WebSocket to an HTTP/SSE upstream.

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
