Sub2API Plus v0.2.11+custom.001

## Highlights

- Integrate official v0.2.11 while preserving Plus credential-owner identity, canonical ingress security audit, session/quota accounting and synchronous billing fallback.
- Keep in-flight balance reservations until asynchronous billing completes, and add configurable API-key creation limits.
- Add confirmed Claude reset-credit redemption, GPT-6.1 Sol and GPT-6 Astra Ultrafast support, remote Codex catalogs and distinct subscription SKU labels.
- Restore Sonnet 5 reference pricing for channel rules and model synchronization while preserving saved selling prices and explicit zero prices.

## Changed

- Claude Code restricted groups can use configured fallback routing for Chat Completions and Responses after ingress audit.
- API-key creation defaults to 200 non-deleted keys per user and 60 attempts per fixed one-hour window. Each limit can be disabled independently with zero.

## Fixed

- Upgrade frontend Axios to 1.20.0 to address the seven high-severity advisories reported by the production dependency audit.
- Preserve Plus identity snapshots and audit-before-side-effect ordering across newly imported paths; repair validation cache cleanup for read-only Go module directories.

## Compatibility and migration

No SQL migration is required. Back up persistent data before upgrading. Review the new API-key creation limits and balance-reservation configuration documented in deploy/README.md. Unknown or incompletely extractable security-audit content remains pass-through. Saved channel selling prices, including zero prices, remain authoritative. Compiled outbound identity fingerprints are unchanged.

## Known issues

Grok Realtime does not import the upstream pre-handshake balance reservation because canonical first-frame audit must precede reservation. Balance reservation cache failures and API-key frequency-limit Redis failures retain the documented fail-open behavior; database count failures remain errors. Unknown Claude reset-redemption outcomes are fenced rather than automatically retried.

## Upstream baseline

Official release: v0.2.11
Official commit: 96f4c115c9749078f90cbf210a01d39baf3f53b6
