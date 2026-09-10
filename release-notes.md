Sub2API Plus v0.2.4+custom.001

## Highlights

Integrates the official v0.2.4 baseline while preserving Plus security,
identity, accounting, administration, and deployment behavior.

## Changed

- Adds MiniMax account, routing, quota, monitoring, and composite-route support.
- Adds Image 2.5 support, long-stream HTTP/2 keepalive, OpenAI weekly usage
  estimates, and administrator controls imported from the official baseline.
- Enforces model allowlists for both discovery and inference while preserving
  Plus aliases, credential-owned Codex identity, and ingress audit ordering.
- Preserves Plus asynchronous image paths, usage alerts, export controls,
  payment flows, session accounting, and hardened deployment defaults.
- Keeps Grok cross-client rewriting opt-in and requires conclusive or explicit
  media eligibility before forwarding media requests.

## Compatibility and migration

Database migrations 259 through 263 run automatically. They rename and repair
the group model policy, add MiniMax constraints, normalize legacy allowlists,
and preserve explicit access-log persistence on existing installations. Back
up the database before upgrading because replacing the binary alone cannot
reverse the renamed schema. Management API clients must use `model_allowlist`
instead of `models_list_config`.

## Known issues

The official v0.2.4 tag embeds source version `0.2.3`; this release intentionally
uses the official tag commit as its upstream baseline.

## Upstream baseline

Official release: v0.2.4
Official commit: 5de5e2bed035d43591a2e10e51f420ef6a84eb98
