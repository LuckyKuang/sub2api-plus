## Automated coverage

- Local quota generation and response-header overlays assert both reset forms.
- Successful OAuth passthrough verifies case-insensitive absolute-header copy.
- Parser tests cover absolute-only, legacy-only, precedence, malformed,
  expired, integer overflow, duration overflow, and reversed-window cases.
- 429 reset scheduling verifies absolute-only upstream headers.
- A real WebSocket dial verifies the reset headers received in the client
  `101` response with the local view enabled and disabled.
- Snapshot persistence verifies absolute-only headers populate both relative
  reset seconds and normalized RFC3339 reset timestamps.

## Manual acceptance

- Inspect a successful HTTP/SSE response for both Primary and Secondary
  absolute and relative reset headers.
- With the local quota view enabled, verify all HTTP/SSE quota values are local
  and the WebSocket `101` contains both local reset forms.
- With the local view disabled, verify upstream `Reset-At` values pass through
  on HTTP/SSE responses and the WebSocket `101` does not inject local values.
- In a current Codex CLI, verify `/status` renders reset times for both windows.
- Treat Codex App API-key `account/rateLimits/read` unavailability as unchanged
  external behavior, not a regression in this change.
