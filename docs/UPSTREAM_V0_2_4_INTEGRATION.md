# Upstream v0.2.4 integration

This source integration uses the commit and publication status recorded in
[UPSTREAM.md](../UPSTREAM.md).

## Upgrade and public API

The group field `models_list_config` becomes `model_allowlist` in storage and
management JSON. Enabled policies govern both model listing and inference
admission, using the client's model before account/channel/composite mapping.
Management clients must use the new field. An enabled empty list is rejected
on new writes. Trailing wildcards and existing model alias equivalence remain
supported; a literal `*` is an explicit administrator policy, never an automatic
upgrade replacement.

Migrations 259 and 260 rename/repair the field; 261 adds MiniMax platform
constraints. Their committed contents remain immutable. Migration 262 trims
entries, removes case-insensitive duplicates in order, and disables legacy
enabled empty/blank lists, preserving the old display-only empty-list behavior.
It emits a PostgreSQL notice containing the affected group ID. Malformed JSON
shapes, non-string entries and non-trailing wildcards stop the migration with
the group ID; repair these configurations before retrying the upgrade. No model
names, prompts or credentials are included in those diagnostics.

Migration 263 recognizes an existing bootstrapped installation by the presence
of user rows before migrations run. It preserves explicit `persist_access_logs`
values and fills a missing field with true for those installations. Fresh
databases have no users at this stage and keep the application default false.
Existing unrelated runtime-log fields are preserved. Malformed runtime-log
configuration must be repaired before upgrading. Ordinary access-log persistence
does not control required security-audit exception logging.

Back up the database before deploying this schema change. Replacing the binary
with an older version alone cannot reverse renamed columns. Recovery requires
the matching pre-upgrade database backup or a reviewed forward compensation;
never edit applied migration checksums.

## Plus integration decisions

| Area | Integrated behavior |
| --- | --- |
| Astra defaults | Retain Plus strict `gpt-6-astra` recognition, low default reasoning and existing context-window policy while incorporating mode/Ultra metadata. Bare or invented aliases do not become supported inference models merely because a prompt selector recognizes their family. |
| Model discovery | Public/API-key and Codex/OAuth catalogs share raw source caching; group filtering occurs afterwards. Credential-owner identity resolves before building cache keys and outbound headers. |
| WebSocket lifecycle | Each accepted turn is audited before side effects. `BeforeTurn` executes once per turn, with terminal/error cleanup and Plus timing retained. Unknown valid frames follow the canonical pass-through audit contract. |
| Tool events | Partial deltas and terminal `done` events are combined without duplication. Missing terminal tool inputs can be recovered from the matching observed call; explicit populated terminal inputs are preserved. |
| MiniMax | Platform, group, quota, monitor and composite-route support is included. Coding-plan credential origin must be an exact approved HTTPS hostname; third-party host/path/query/userinfo lookalikes do not initiate an official quota query. |
| Grok | Missing or inconclusive OAuth entitlement does not authorize media forwarding. Explicit administrator enable/disable wins; clearing it restores automatic evaluation. Native billing observation remains separate from retired generic upstream probes. |
| Defaults | Grok cross-client model rewriting stays disabled; channel rankings remain visible unless hidden; redeem failures use a new atomic fixed ten-minute window with a limit of 30. |
| Images | Image 2.5 and Plus synchronous, batch and asynchronous paths coexist. `SUB2API_IMAGES_MAIN_MODEL` defaults to `gpt-5.6-luna` in deployment examples. |
| Administration | Preserve usage alerts, export controls, inactive-group selection, quota reset windows, payment checkout tabs and simple-mode server restrictions. |

## Validation boundary

All local generation and validation runs in Apple Containers with the pinned
repository toolchain. Relevant backend suites, frontend lint/typechecking and
Vitest, real PostgreSQL/Redis repository integration, and deployment/migration
checks are required for this integration. PR submission additionally requires
the official full local matrix through the repository submission CLI. Local
test success is not evidence of a published release or a production upgrade.
