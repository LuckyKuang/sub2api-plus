Sub2API Plus v0.1.178+custom.004

## Highlights

- Align Content Moderation and Prompt Audit extraction behavior with
  `v0.1.177+custom.003`: incomplete or unrecognized content is observable and
  passes through without an audit-derived block.
- Preserve extracted sibling content for independent policy evaluation across
  HTTP, Responses WebSocket, and Live Sideband request paths.
- Add safe structured logs for extraction, worker, queue, persistence, and
  runtime audit exceptions without exposing request content or raw errors.

## Changed

- Use one canonical extraction result for both audit engines, with bounded and
  sanitized incomplete-reason diagnostics.
- Treat recognized-object unknown sibling fields as compatible extensions,
  while unknown item types, frames, and wholly unrecognized structures are
  counted and logged before pass-through.
- Keep the official v0.1.178 baseline and existing Codex identity precedence.

## Fixed

- Prevent extraction failures from being converted into policy blocks,
  unavailable decisions, HTTP 503 responses, or WebSocket closes.
- Ensure an unsupported frame carrying recognized content remains observable
  while its extracted content can still trigger an independent policy block.

## Compatibility and migration

- No database migrations or configuration changes. Clients can continue to
  send compatible extension fields and unknown protocol frames; those requests
  now retain the established pass-through behavior.

## Known issues

- None known.

## Upstream baseline

Official release: v0.1.178
Official commit: e0c48a19ed794a565e3858662520afe0a1f9f0ba
