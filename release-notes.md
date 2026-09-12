Sub2API Plus v0.2.4+custom.002

## Highlights

Corrects usage timing and OpenAI OAuth weekly quota reset behavior across
streaming adapters, group membership changes, and concurrent reset processing.

## Changed

- Defines one verified first-token, total-duration, and estimated-TPS contract
  across supported account and protocol variants.
- Follows explicit 10,080-minute OpenAI OAuth windows for group quota resets and
  preserves source membership and baseline state when groups are copied.
- Acquires client WebSocket connection leases only after first-frame validation
  and security audit.
- Synchronizes MiniMax localization and deployment defaults introduced by the
  official v0.2.4 baseline.

## Fixed

- Prevents image, signature, grounding, compaction-only, empty-tool, and repeated
  metadata output from being counted as generated text tokens.
- Extends Gemini tool-call timing through later non-empty argument deltas and
  keeps low positive TPS values visible without rounding them to zero.
- Reconciles missing group reset baselines on repeated accepted observations and
  rechecks source membership after database lock waits.
- Commits copied group configuration, account membership, and scheduler outbox
  entries atomically so reset workers never observe a partial copy.

## Compatibility and migration

No new database migration or configuration field is introduced by this release.
Existing usage rows keep their historical timing provenance; only newly verified
rows participate in strict first-token and TPS reporting. Existing weekly quota
usage is reset only after an eligible bound OpenAI OAuth source reports an
accepted later weekly window.

## Known issues

The official v0.2.4 tag embeds source version `0.2.3`; this release continues to
use the official tag commit as its upstream baseline.

## Upstream baseline

Official release: v0.2.4
Official commit: 5de5e2bed035d43591a2e10e51f420ef6a84eb98
