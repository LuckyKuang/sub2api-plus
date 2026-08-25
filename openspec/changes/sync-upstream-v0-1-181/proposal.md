## Why

Official Sub2API v0.1.179–v0.1.181 adds adaptive Chinese-provider protocols,
channel Fast/Flex/context multipliers, Composite Codex/CN routing, usage
aggregation, Fast `service_tier`, reset-card auto-use, experimental OAuth
transport plugins, Go 1.27, and four compatibility fixes. Plus currently sits on
`v0.1.178+custom.005` with overlapping identity, security-audit, admin support,
IP-control, usage TPS, and release-tooling changes, so a mechanical merge is not
sufficient.

## What Changes

- Merge official annotated tag `v0.1.181` at commit
  `3af5443b224823ae507a50c7b113aa50604409c8` and no later upstream commit.
- Adopt official 179–181 product capabilities: adaptive protocols, channel
  multipliers, Composite Codex/CN, usage GROUPING SETS, Fast `service_tier`,
  reset-card auto-use, model-list read limits, marketplace time pricing, OAuth
  transport plugins, Go 1.27.0, and the four 181 compatibility fixes.
- Retain Plus-owned Prompt Audit extraction, Content Moderation scope, admin
  user support view, IP access master switch, Codex mode-only identity with
  `device` default, S3 date paths, failed image-task cleanup, usage TPS,
  release tooling, module path, and brand.
- Import official migrations as Plus prefixes `229`–`233`. Keep published Plus
  migrations 224/225/226/228 unchanged. Do not reintroduce
  `225_backfill_codex_fingerprint_seed.sql` or seed persistence.
- Prepare `v0.1.181+custom.001` as a planned, unpublished mapping in
  `UPSTREAM.md` and synchronized release documentation.

## Impact

- Persistent data: five new forward-only migrations starting after published
  prefix `228`. Existing checksums stay frozen.
- Public API/UI: adaptive protocols, channel multipliers, plugins, Fast tier,
  Composite endpoints, and admin settings coexist with Plus support view and IP
  switch.
- Security and protocol: security audit still runs after auth and before account
  selection, billing, concurrency, or upstream writes. Codex identity remains
  mode-only and credential-owner derived.
- Billing: long-context gating becomes “any enabled switch”, which can raise
  existing OpenAI bills above 272k context.
- Operations: version and install/rollback examples advance without publishing a
  tag, Release, or image.
