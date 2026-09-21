Sub2API Plus v0.2.7+custom.001

## Highlights

First Plus release on official `v0.2.7`. It keeps Plus identity, ingress audit, session/quota accounting, and retired billing probes, while importing the official Seedance native video task API, generic plugin host services, redeem history pagination, DeepSeek reasoning and tool-output fixes, Antigravity Gemini thinking-variant and SSE keepalive handling, CN coding-plan quota pause, and the group usage rollup that no longer full-scans `usage_logs`. Plus adds egress metadata validation and outbound identity gap fixes on top of the v0.2.5 baseline.

## Changed

- Official v0.2.7 Seedance Ark native video task API with dedicated routes and service.
- Generic plugin host services with a read-only status bridge channel; plugin `status_json` test results surface to the config UI.
- User redemption history is paginated with stable ordering and per-user isolation.
- DeepSeek chat fallback passes thinking-mode `reasoning_content`; Responses tool output images are lifted and parallel tool outputs stay contiguous.
- Antigravity Gemini native requests resolve bare model names to `-low`/`-medium`/`-high` thinking variants, suppress SSE comment heartbeats for go-genai/python-genai clients, and strip Claude attribution metadata from system prompts.
- CN coding-plan accounts pause on quota-exhausted `403`; group usage rollups stop full-scanning `usage_logs`.
- OpenAI gateway persists response affinity after client cancel, normalizes developer roles for strict Chat upstreams, and keeps manifest key validation without duplicate parsing.
- OAuth tokens keep refreshing for paused accounts.
- Plus egress metadata (timezone/country) is validated and preserved across account, proxy, and global setting levels; outbound identity validation gaps are closed.

## Compatibility and migration

No new SQL migrations ship in this import. Back up the database before upgrade. Rollback image is `v0.2.5+custom.001`.

## Known issues

None.

## Upstream baseline

Official release: v0.2.7
Official commit: aea725f2ea644d5592d0bbb1d63b607efa7e200a
