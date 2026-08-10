## Why

The production error-request history contains several failures whose current
gateway behavior is either unsafe (a retry-buffer overflow is presented as a
retryable upstream failure), ambiguous (OAuth policy denial can mask another
selection outcome), or not sufficiently observable (local routing exhaustion,
transport resets, and timeout paths lack structured diagnostics).  The admin
error screen also treats a calendar-day range as “last 24 hours”, which makes
the operational view misleading.

## What Changes

- Release the completed repair set as `v0.1.173+custom.002` metadata only; no
  tag, image, Release, or remote publication is created.
- Preserve valid Responses item identifiers by item type, including
  `custom_tool_call` identifiers beginning with `ctc`.
- Convert the exact proxy retry-buffer-limit response (`507`) into a
  non-retryable, request-scoped client `413` without account health or cooldown
  side effects.
- Keep OAuth session-policy denials distinct from capacity, temporary
  scheduling, and disabled-account selection outcomes.
- Persist sanitized structured routing diagnostics and a separate routing
  capacity marker for Ops error records through a new forward-only migration.
- Classify upstream transport connection failures and gateway timeouts without
  incorrectly recording them as model-level account failures; expose safe
  diagnostics for operational triage.
- Make the admin default “last 24 hours” query an exact rolling `24h` backend
  range and add explicit Error/Excluded/All error-record views.
- Surface the selected safe OpenAI outbound-identity source in error
  diagnostics while preserving the immutable account → global → compiled
  default precedence.

## Impact

- Persistent data: a new forward-only Ops migration adds routing diagnostic
  fields and backfills the existing routing error phase marker.
- Public behavior: a retry-buffer overflow returns a deterministic `413` and
  valid Codex custom tool-call context is retained.
- Operations: error records, detail APIs, and the administration UI gain
  route/transport/timeout attribution and a rolling-window query mode.
- Security boundary: diagnostics contain only sanitized classifications and
  identity source metadata; credentials, URLs with secrets, and raw headers are
  not persisted.
