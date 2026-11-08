# v0.2.20+custom.001

## Upstream baseline

Official v0.2.20, peeled dab829a4e9b7d1cc307189aea8b0319eb29e29d0.
Plus contracts remain authoritative; post-tag upstream main is excluded.

## Changes

- Bound gateway request buffering, socket operations, upstream draining, stream
  parsing, background work and session caches.
- Add opt-in OAuth management MCP, explicit administrator permissions/full-access
  confirmation, and exact registration-email blacklists.
- Import Responses completion, reasoning/tool-content, provider quota and image
  routing/native-option corrections while preserving Plus identity, audit,
  proxy ownership, Composite access and frozen billing.
- Align Terra API effort/tool rules with official documentation without changing
  the pinned Codex client descriptor or manually selected 0.167.0 identity.

## Compatibility and upgrade

- New MCP migration is Plus 307; deployed migrations remain unchanged.
- MCP remains off until MCP_PUBLIC_URL is configured. Existing administrator
  keys retain their scopes; full access requires explicit confirmation.
- Resource settings use gateway.*, including a 1-GiB ingress buffering budget
  and 120-second per-operation read/write deadlines by default. Zero disables
  the corresponding configurable limit. Tune against instance capacity.
- Post-disconnect draining is finite. Only observed usage can be settled if
  final provider usage arrives after the drain window. Prices remain frozen.
- Terra API minimal effort is rejected; none is accepted. Tool calls require
  Responses. Existing Codex catalogs retain their separate client contract.

## Known limitations

The ingress budget is conservative buffer accounting, not a total RSS cap.
MCP public URLs require the exact root /api/v1/mcp path; subpath deployment
is unsupported. Cache bounds may evict continuation bindings before TTL.

Publication status: planned.
