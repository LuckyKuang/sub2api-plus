Sub2API Plus v0.2.4+custom.003

## Highlights

- Enforces trusted, credential-owner outbound identity consistently across provider, OAuth, probe, audit, monitoring, and auxiliary request paths.
- Follows confirmed OpenAI weekly quota resets safely with consistent lock ordering and repeated-reset evidence.
- Audits only current user prompt content while preserving the documented pass-through extraction contract, and reports TPS from the decode window.

## Changed

- Upgrades Vitest to 4.1.11, gRPC to 1.83.2, and pnpm/action-setup to 6.1.0.
- Adds provider-specific outbound identity configuration and synchronized English/Chinese administration controls.

## Fixed

- Applies Bedrock identity before IAM signing and preserves Antigravity companion headers.
- Prevents account/group lock-order inversion during weekly reset observation.
- Uses isolated trusted identity snapshots for monitoring, Prompt Audit, Content Moderation, Google Drive discovery, OAuth, probes, and related auxiliary traffic.

## Compatibility and migration

Database migration 265 is applied through the normal forward-only migration flow. It extends OpenAI weekly reset observations with pending confirmation, reset sequence, usage evidence, and poll scheduling fields; no manual data migration is required.

## Known issues

None.

## Upstream baseline

Official release: v0.2.4
Official commit: 5de5e2bed035d43591a2e10e51f420ef6a84eb98
