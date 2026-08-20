## Why

Official Sub2API v0.1.178 adds channel-monitor quota modes, Chinese-provider
pricing and balance support, and platform/account UI changes. It also updates
Codex and WebSocket/Responses handling. These areas overlap
Plus-owned module identity, outbound identity precedence, OAuth session policy,
prompt-cache behavior, and release metadata, so a mechanical merge is not
sufficient.

## What Changes

- Merge official tag `v0.1.178` at commit
  `e0c48a19ed794a565e3858662520afe0a1f9f0ba` and no later upstream commit.
- Adopt the official channel-monitor quota lifecycle, CN-provider quota
  endpoints, channel model time pricing, and frontend locale/UI updates.
- Adopt official Responses/WebSocket protocol fixes while retaining the latest
  Plus Codex mode-only implementation, including its explicit `device` default,
  account persistence, OAuth authorization, prompt-cache, compaction, and
  turn-state safeguards.
- Keep the Plus Go module path and repository/distribution identity.
- Prepare `v0.1.178+custom.001` as a planned, unpublished mapping in
  `UPSTREAM.md` and synchronized release documentation.

## Impact

- Persistent data: forward-only migrations add platform quota coverage, time
  pricing, monitor quota fields, and the latest Plus Codex mode backfill. The
  imported CN-provider constraint is assigned prefix `228` because `224` is
  already owned by the latest Plus mode migration.
- Public API/UI: administrators can configure monitor quota mode and time
  pricing; supported CN providers expose balance/quota data in both locales.
- Security and protocol: credential-owner identity precedence and OAuth session
  authorization remain authoritative before fingerprint or request mutation.
- Operations: version and install/rollback examples advance without publishing
  a tag, Release, or image.
