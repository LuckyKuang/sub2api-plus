## Why

The unpublished Plus v0.1.182 integration has been carried forward locally, and
official Sub2API v0.1.183 adds Codex session-id affinity, sticky capacity
spillover, OAuth 429 quota scheduling, Responses custom tool-call IDs, email
rebind guards, Kimi 403 recovery, Antigravity token clamp, and channel-monitor
v2 NULLIF. The planned Plus release must replace the unpublished 182 candidate
with one coherent 183 baseline.

## What Changes

- Create `release/0.1.183-custom.001` from the completed
  `release/0.1.182-custom.001` line, then merge only official annotated tag
  `v0.1.183` at commit `e8cb019fabf8b55199436229044cbf9aa7a82564`.
- Retain Plus source identity precedence, audit ordering, mode-only Codex
  fingerprinting, session affinity helpers, usage TPS, deployment ownership,
  and distribution branding.
- Combine official sticky spillover and OAuth 429 classification with Plus
  `bindOpenAIStickySessionAccount` and request-body / proxy-buffer failover
  reasons.
- Prepare unpublished metadata for `v0.1.183+custom.001`; do not publish a
  branch, tag, release, image, or artifact.

## Impact

- Public protocol behavior: OpenAI sticky routing, WebSocket session headers,
  Responses custom tool-call IDs, and OAuth 429 handling.
- Auth: email rebind alias and concurrency guards.
- Routing and monitoring: Kimi 403 recovery, Antigravity token clamp,
  channel-monitor v2 composite aggregation.
- Deployment: embedded version, image tags, install examples, release notes,
  and upstream mapping.
- Persistent data: no new official v0.1.183 SQL migrations; Plus migrations
  229–233 remain forward-only and pending for upgrades from the published 178
  line.
