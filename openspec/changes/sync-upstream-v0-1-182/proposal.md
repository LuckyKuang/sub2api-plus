## Why

The unpublished Plus v0.1.181 integration has been carried forward locally, and
official Sub2API v0.1.182 adds Responses Lite compatibility, OAuth image prompt
preservation, billing, routing, monitoring, and payment fixes. The planned Plus
release must replace the unpublished 181 candidate with one coherent 182
baseline.

## What Changes

- Merge only official annotated tag `v0.1.182` at commit
  `5a7d469622911a6b1291a692376df5fa03f9ac2e` after carrying forward the local
  Plus integration and its conflict resolutions.
- Retain Plus source identity precedence, audit ordering, mode-only Codex
  fingerprinting, session affinity, usage TPS, deployment ownership, and
  distribution branding.
- Combine official Responses Lite and verbatim image-prompt fixes with Plus
  prompt-cache identity, WebSocket audit, account identity, and Codex headers.
- Prepare unpublished metadata for `v0.1.182+custom.001`; do not publish a
  branch, tag, release, image, or artifact.

## Impact

- Public protocol behavior: OpenAI Responses Lite HTTP and WebSocket paths.
- Billing and routing: Anthropic cache accounting, Composite routing and
  channel-monitor attribution.
- Deployment: embedded version, image tags, install examples, release notes,
  and upstream mapping.
- Persistent data: no new official v0.1.182 SQL migrations; Plus migrations
  229–233 remain forward-only and pending for upgrades from the published 178
  line.
