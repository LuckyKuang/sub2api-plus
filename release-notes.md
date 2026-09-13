Sub2API Plus v0.2.4+custom.004

## Highlights

- Restricts OpenAI group quota follow-reset evidence to fresh weekly-window observations from real inference sessions.
- Prevents WebSocket handshake headers and standalone quota refreshes from establishing or confirming group reset events.
- Runs release metadata and finalization-tree validation only in the repository's supported platform containers.

## Changed

- Updates the English and Chinese administration guidance to describe the inference-session baseline requirement.
- Clarifies repository-wide agent rules, Windows WSL2 Docker validation, and the deployed-instance scope of the Sub2API admin skill.
- Makes stale validation image cleanup deterministic without pruning unrelated runtime resources.
- Forwards configured standard proxy variables into validation containers without exposing their values in commands or logs.

## Fixed

- Removes the background quota polling path that could drive group reset state without user inference traffic.
- Keeps WebSocket connection-time usage headers available for account cache refresh while excluding them from later turn reset evidence.
- Preserves fresh HTTP and in-band WebSocket rate-limit observations across retries and pass-through adapters.

## Compatibility and migration

No new database migration is required. Existing group follow-reset bindings wait for a real inference session on their configured OpenAI OAuth source before establishing a missing baseline or confirming off-schedule evidence. Standalone quota refreshes no longer advance that state.

## Known issues

None.

## Upstream baseline

Official release: v0.2.4
Official commit: 5de5e2bed035d43591a2e10e51f420ef6a84eb98
