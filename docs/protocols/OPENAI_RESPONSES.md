# OpenAI Responses and WebSocket Ingress

Sub2API Plus accepts OpenAI-compatible Responses requests over HTTP and
client-facing WebSocket ingress. Account routing can use an upstream WebSocket
or bridge the client WebSocket to an HTTP/SSE upstream.

## Prompt Cache Identity and Usage

Current Codex clients can supply the canonical `session-id`, `thread-id`, and
`x-client-request-id` headers. The gateway also accepts the supported legacy
session aliases, including `session_id`, for sticky routing compatibility.
When direct session headers are absent, the stable string `session_id` inside
`X-Codex-Turn-Metadata` is also accepted; request-scoped `turn_id` is never a
routing key. This lets `/v1/responses` WebSocket ingress and
`/v1/alpha/search` share one account route even when Alpha Search would
otherwise fall back to its search ID.
The same sanitized value is recorded as `usage_logs.session_id` and participates
in cyber-session blocking. Endpoint fallbacks and `turn_id` remain routing
details and are not persisted as client session IDs.
Thread and client-request identifiers remain paired request context and never
replace the session-scoped cache identity.

For OpenAI Responses and Compact requests, the gateway resolves one opaque,
tenant-isolated cache identity from an explicit `prompt_cache_key`, a supported
session header, or a stable content prefix with a meaningful user/input anchor.
It writes the finalized UUID to both the upstream `prompt_cache_key` and the
canonical `session-id` header; the legacy `session_id` alias carries the same
value. A model-only request does not receive a content-derived key. API-key
Chat Completions requests converted to Responses use the same behavior, while
raw Chat Completions forwarding does not receive Responses-only cache fields.

Under the default hard-affinity mode, account priority changes do not replace a
valid active session route. The optional sticky-weighted scheduler mode remains
score-based by design. If the configured health/concurrency sticky escape
temporarily bypasses a degraded account, a movable Responses continuation does
not fall back to that account through its older response ID; the temporary
candidate order is derived from the shared session identity, while the
canonical sticky binding remains unchanged. Removing an account from the
requesting group, disabling it, or making it incompatible is always a hard
invalidation: both ordinary sticky routing and `previous_response_id` routing
recheck the current account before reuse. Long-lived Responses WebSocket
connections also run current billing and reload the selected account before
every turn in `ctx_pool`, `http_bridge`, and `passthrough` modes. The refreshed
account must still satisfy group, status, schedulability, quota, client-model,
endpoint-capability, transport, parent-health, runtime-block, scheduling
threshold, and proxy-quarantine gates. Follow-up image intent also repeats the
group permission and Responses-capability checks. If any gate fails, the
gateway closes before sending that turn upstream; a reconnect then performs
normal account selection. The first turn is billed-eligible once before
selection, while every later turn is checked once at its pre-turn boundary. A
movable WebSocket continuation follows a session route that has already failed
over to another account and removes the old account's response ID before
forwarding. Tool-output continuations without complete call context remain on
the response owner and keep the existing fail-closed OAuth ownership checks.

`prompt_cache_options` is forwarded only for GPT-5.6-family OpenAI Platform
API-key Responses/Compact traffic. ChatGPT OAuth and older or unknown model
families have that field removed. Deprecated `prompt_cache_retention` is
removed on every path.

Usage ingestion treats ordinary input, cache-read input, and cache-write input
as mutually exclusive stored buckets. The UI reports prompt-cache hit rate as
`cache_read / (input + cache_read + cache_write)`; output tokens are excluded.
Canonical nested usage details take priority by field presence, including an
explicit zero, before known top-level compatibility aliases are considered.

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

## Codex Fingerprint Convergence

OpenAI OAuth accounts may rewrite outbound Codex installation, session, and
thread carriers. Unset accounts use `device` mode. Ordinary Responses,
Chat-Completions-to-Responses, Messages-to-Responses, HTTP-to-WebSocket, and
direct Responses WebSocket turns use the configured account mode. Native
remote Compact v2 is an ordinary Responses session for fingerprint purposes
and therefore also uses the full configured mode. The ChatGPT Codex OAuth
legacy compact compatibility path uses installation-only convergence for every
non-`off` mode and preserves its own compact session, cache, and thread
namespace.

Here `legacy` refers only to the ChatGPT Codex OAuth compatibility branch used
by this gateway. The public API-key
[`/v1/responses/compact`](https://developers.openai.com/api/reference/java/resources/responses/methods/compact)
endpoint remains a distinct supported OpenAI API surface. Response retrieve,
cancel, and other non-create subpaths are not session turns and receive no
fingerprint mutation.

Fingerprint preparation runs before final request construction. Plus
prompt-cache/session isolation is authoritative for the final `session-id` and
`session_id` headers, while fingerprint convergence remains authoritative for
installation and thread/turn metadata. `off` disables only fingerprint-owned
header and body mutation; it does not disable Plus cache isolation, security,
session sharing, or compact policy. WebSocket connection reuse compares final
stable handshake carriers even when `off` or `device` leaves those values
client-owned. Usage-log `session_id` stays the sanitized client-original value.

Only credential-owning OpenAI OAuth accounts participate. Personal access
token and Agent Identity accounts follow the same endpoint semantics because
they are OpenAI OAuth credential owners. API-key, setup-token, and non-session
endpoints such as count-tokens and alpha-search are excluded. User-Agent,
Originator, and Version use one source chain: valid credential-owner
`credentials.user_agent`, then valid global `openai_codex_user_agent`, then the
compiled default. Version synchronization changes only the version declaration
of the selected identity.

## Security Audit Content Boundary

Inbound Responses content is normalized for Content Moderation and Prompt
Audit before account selection, billing, concurrency acquisition, fingerprint
convergence, request adaptation, or upstream writes. API-key and OAuth account
paths therefore use the same audit content.

The boundary covers top-level and `response`-nested `instructions`, `tools`,
`input`, reusable `prompt.variables`, message text, tool definitions, and the
arguments, input, output, result, or dynamic tools carried by function, custom,
tool-search, local/hosted shell, apply-patch, computer, MCP, code-interpreter,
and programmatic-tool-calling items. In particular,
`function_call_output.output`,
`custom_tool_call_output.output`, the compatibility
`tool_search_output.output`, and official `tool_search_output.tools` are
audited on every HTTP or WebSocket turn. Media fields and encoded screenshots
remain Content Moderation image inputs but are removed before Prompt Audit text
serialization and persistence; ordinary text in the same structured result is
retained.
The current input item remains client-controlled even when it claims an
`assistant` role; role labels cannot force latest-turn auditing to scan an old
user message instead. Current message/tool content is scanned before repeated
instructions and tool definitions, while all context remains included.
A supported WebSocket control frame is an explicit no-content case only when it
contains no canonical content fields or unknown non-empty siblings. An envelope
`type` value never suppresses `input`, `instructions`, or nested
`response.input` that is actually present.
Direct passthrough runs the audit hook for every client text or binary frame,
including `conversation.item.create` and `session.update`, before any
non-`response.create` frame is forwarded. Invalid binary/JSON payloads fail
closed in blocking mode.
A recognized content-bearing item that cannot be normalized is observable and
fails closed whenever a blocking audit mode applies.
Top-level Responses requests, nested `response` objects, and `session.update`
session objects reject unknown non-empty siblings as incomplete extraction;
successfully extracted instructions or input never mask such a sibling.
Content Moderation reports that extraction failure as HTTP `503` with
`content_moderation_unavailable`; the coordinator classifies it as
`unavailable`, rather than a policy block or policy-violation error. Compact
keepalive output and channel mapping start only after this gate. The audit uses
an immutable copy of the inbound body so compact normalization and reasoning
policy rewrites cannot remove content from the audited view.

The complete protocol/source matrix is maintained in
[`docs/SECURITY_AUDIT_CONTENT_COVERAGE.md`](../SECURITY_AUDIT_CONTENT_COVERAGE.md).

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
