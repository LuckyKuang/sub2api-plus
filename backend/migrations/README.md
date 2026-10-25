# Database Migrations

SQL migrations are embedded in the application and applied automatically at
startup. The runner records each filename and SHA256 checksum in
`schema_migrations`.

Migration `294_outbound_identity_ids.sql` stores official-client random IDs by
semantic namespace and irreversible scope digest. PostgreSQL atomically chooses
the first value across replicas. Records have no scheduling-cache TTL, so an
application restart or cache eviction cannot rotate an active conversation ID.
They contain no prompts or credentials; backing up/restoring the database also
preserves these installation and session identities. This operational table is
owned by the SQL repository, like `openai_session_routes`, rather than Ent.
See [outbound identity](../../docs/OUTBOUND_IDENTITY.md).

Migration `295_channel_monitor_v2_model_visibility.sql` supplies explicit display
mode and empty exclusion defaults in V2 platform JSON while retaining operator
lists, custom fields and raw history. It advances the configuration version once
when needed; exclusions are applied on reads without rebuilding stored rollups.
See [V2 model visibility](../../docs/CHANNEL_MONITOR_V2.md).

## Mandatory Rules

- Existing migration files are immutable: never modify, delete, or rename them.
- Create a new forward-only migration for every database change.
- Revert behavior with a new compensating migration; there are no Down blocks.
- Use the next unique, increasing numeric prefix.
- Keep each migration focused and idempotent where practical.

The repository contains historical duplicate numeric prefixes. They remain
unchanged because filenames and checksums are already deployed. Do not add new
duplicates.

Migration `281_drop_platform_check_constraints.sql` transfers the quota-platform
and composite-route-platform whitelist to the shared application catalog. It
runs after Plus migration 277, which registered StepFun in those CHECKs, so new
installations and upgraded databases reach the same state. Application API,
service, repository and Ent validation still reject unregistered platforms.
Channel-monitor provider CHECKs continue to enforce implemented probe capability.

Migration `282_redeem_order_snapshots.sql` adds opt-in redeem-code order snapshots
and payment-order source, currency, plan-name and unique redeem-code linkage.
Existing codes default to no order; existing orders keep the `payment` source
and their established provider-snapshot currency resolution. Historical orders
remain valid after a code or plan is deleted. See [payments](../../docs/PAYMENT.md).

Migration `283_channel_monitor_v3_probe_sources.sql` extends V3 fact, state and
incident keys with source and scope. Existing rows become `request` observations
with their original group ID as scope, preserving history and incident IDs.
Probe scopes use monitor IDs and nullable group IDs; extra probes default off.
See [channel monitoring](../../docs/CHANNEL_MONITOR.md).

Migration `284_usage_service_tier_snapshots.sql` adds independent nullable
outbound-request and upstream-response service-tier diagnostics. Historical
rows stay unrecorded; billing tiers and prices are unchanged. These fields are
administrator-only. See [Responses diagnostics](../../docs/protocols/OPENAI_RESPONSES.md#administrator-usage-service-tier-diagnostics).

Migration `285_first_byte_timing.sql` adds independent nullable body-first-data
timing to usage and V1 histories and independent successful-probe rollup sums
and counts. Existing rows stay NULL/zero-sample; no token clock is backfilled.
See [usage timing](../../docs/USAGE_TIMING.md).

Migration `286_group_i18n.sql` adds a non-null JSONB translation map to groups,
defaulting to `{}` for existing and new groups. Original names/descriptions,
Plus five-hour limits, reset sources and custom prices remain intact. The
official source names this new migration `242_group_i18n.sql`; Plus already
uses prefix 242, so only the new Plus 286 migration is imported. Existing
filenames and checksums remain immutable.

Migration `287_channel_monitor_stream_probe.sql` adds opt-in V1 streaming probes,
nullable first-text/mode history and independent successful-text rollup sums/counts.
Only supported existing replace-mode boolean `stream:true` snapshots are promoted;
merge/off snapshots stay disabled and historical timings stay unknown. See
[channel monitoring](../../docs/CHANNEL_MONITOR.md#v1-流式探测).

Migration `288_openai_session_routes.sql` adds the authoritative logical-session
route table `openai_session_routes`. PostgreSQL is authoritative for the
canonical route owner; Redis only accelerates reads and carries the same
generation. Rows store the authorization domain, an irreversible session
digest, the owner account and the compare-and-set generation/lease state used to
commit a migration; no prompt text, credentials, or conversation history is
persisted. Existing sessions have no row until their next bind, and the request
path does not consume the table yet. See
[Responses routing](../../docs/protocols/OPENAI_RESPONSES.md).

Migrations `287_gemini_cached_contents.sql`,
`288_gemini_cached_contents_channel_usage.sql`, and
`289_gemini_cached_contents_mutation_lease.sql` add Gemini explicit-cache
tables, channel-usage snapshots, and mutation leases. Official numbered these
243–245; Plus already uses those prefixes, so they land at 287–289. Historical
SQL is unchanged.

The main/feature merge retained the independently committed 287/288 files above.
Their full filenames and checksums remain immutable. The channel-monitor/session
route migrations and Gemini cache migrations have independent dependencies;
lexicographic execution and upgrades from either branch retain all five files.
The seven-platform branch independently deployed 290–294 and
`295_channel_monitor_v2_model_visibility.sql`; main independently published
`295_enable_gemini_image_generation_groups.sql`. Their dependencies are separate,
so synchronization retains both complete 295 filenames and original checksums.
New migrations start after 295 and must use unique increasing prefixes.
The migration policy checker pins the complete filenames and original Git blobs
of these twelve migrations in `tools/check_new_migrations.py`. Only those unchanged
historical files may enter comparisons against either branch's newer baseline.
They also reserve their numeric prefixes when comparing against an older release.
Modified or renamed files, other old prefixes and new duplicate prefixes remain
forbidden.

Migration `295_enable_gemini_image_generation_groups.sql` enables image
permissions for existing Gemini and Antigravity groups. Composite and other
platforms retain their saved value. Official migration 246 uses a prefix
already occupied by Plus, so main imported it as 295 before this synchronization.

## File Naming

Regular migration:

```text
NNN_description_in_snake_case.sql
```

Non-transactional concurrent-index migration:

```text
NNN_description_in_snake_case_notx.sql
```

The full filename is the migration identity. Files execute in lexicographic
filename order.

## Execution Model

Regular `*.sql` files execute as a single transaction. Do not add executable
Down SQL or transaction-control sections; the runner executes the complete file
as-is.

Example:

```sql
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS example_column VARCHAR(100);
```

`*_notx.sql` exists only for concurrent index operations. It may contain:

- `CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`
- `DROP INDEX CONCURRENTLY IF EXISTS ...`

It must not contain other DDL/DML or explicit `BEGIN`, `COMMIT`, or `ROLLBACK`.

## Creating a Migration

1. Find the largest existing numeric prefix.
2. Create one file using the next unused number.
3. Write a forward-only change.
4. Add tests for schema, data, compatibility, or migration behavior.
5. Run:

   ```bash
   cd backend
   go test ./migrations ./internal/repository/...
   go test ./...
   ```

6. Verify startup against both a new database and a representative upgraded
   database when the change is operationally significant.

## Checksum Failures

A checksum mismatch means a previously applied migration differs from the
tracked file. The correct recovery is:

1. Restore the exact released migration content.
2. Create a new migration for the intended change.
3. Re-run the migration and repository tests.

Do not update the database checksum manually. The compatibility allowlist in
`internal/repository/migrations_runner.go` is only for explicitly audited
historical incidents and must not be used for routine development.

## Operational Notes

- PostgreSQL advisory locking serializes migrations across application
  instances.
- Applied files with matching checksums are skipped.
- New migrations run before the application begins normal service.
- A failed regular migration rolls back its transaction.
- A failed non-transactional migration requires operator review before retry.

## Domestic identity environment

Migration 276 deletes the superseded Kimi/ZCode OS, kernel and architecture
overrides from global and account identities. They now use the pinned Ubuntu
24.04 environment. Device name/UUID, locale, timezone and unrelated configuration
are preserved. See [outbound identity](../../docs/OUTBOUND_IDENTITY.md).

## Native client family selection

Migration 278 removes retired default mappings and whole foreign identity
candidates from native provider/cloud accounts. Compatible supplier mappings,
correct-family candidates, global profiles/runtime declarations, credentials,
model restrictions and billing settings remain intact. The cleanup is tested
against isolated PostgreSQL, including repeated execution and empty settings.
See [outbound identity](../../docs/OUTBOUND_IDENTITY.md#selection-and-persistence).

## Upgrade Prerequisites

Back up PostgreSQL before upgrading. Replacing the application binary alone
cannot reverse renamed columns or data cleanup; recovery requires the matching
pre-upgrade backup or a reviewed forward compensation. Applied migration files
and checksums remain immutable.

- **Model policies (259–262):** `models_list_config` becomes `model_allowlist`
  in storage and management JSON. Update management clients to the new field.
  Migration 262 trims entries, removes case-insensitive duplicates in order,
  and disables legacy enabled empty/blank lists. It never replaces them with
  `*`. Malformed JSON, non-string entries and non-trailing wildcard patterns
  stop this historical migration; repair the indicated group before retrying.
  Diagnostics contain group IDs, without model names or credentials. Current
  runtime policies support `*` at any position; that does not change migration
  262's one-time validation. See [model admission](../../docs/protocols/OPENAI_RESPONSES.md).
- **Access logs (263):** an installation with pre-existing user rows preserves
  explicit `persist_access_logs` and receives `true` only when that field is
  missing. Fresh databases keep the application default `false`. Unrelated
  runtime-log fields are preserved; malformed configuration needs repair.
  This switch does not disable required security-audit exception logs.
- **Platforms and quotas (261, 266, 267, 273, 277):** platform constraints retain the
  full Plus platform set, including MiniMax, OpenCode and TypeSafe. Migration
  267 removes quota rows whose daily, weekly and monthly limits are all NULL;
  those rows are unlimited. Migration 273 expands both quota and composite
  target constraints without removing existing platforms. Migration 277 adds
  StepFun to those constraints and the legacy probe-provider constraints.
- **Usage and payments (269, 270, 274):** rollout budget units remain a reserved
  usage dimension; affiliate `operation_id` supports idempotent ledger writes.
  Historical payment orders receive `bonus_amount=0`. See [payment behavior](../../docs/PAYMENT.md).
- **Authentication caches:** password-reset links stored in the previous
  plaintext form are rejected; users request new links. Verification codes
  carry forward legacy attempt counters: four attempts permit at most one
  more comparison; five or more permit none. See [authentication](../../docs/AUTHENTICATION.md).

For explicit upstream URL allowlists, add `api.minimax.io` for the international
MiniMax site (`api.minimaxi.com` for China); new defaults do not merge into
existing explicit lists. The system-log cleanup fallback
`ops.cleanup.system_log_retention_days` is 30 and must be positive when cleanup
is enabled; runtime ops settings can override it. See the maintained
[deployment configuration](../../deploy/config.example.yaml).

Runner implementation:
`backend/internal/repository/migrations_runner.go`.

## Local security audit error attribution

Migration 279 corrects reliably identifiable historical local 403 refusals to
request/client attribution with their exact policy or prompt-guard code.
Malformed, ambiguous and provider records remain unchanged. It preserves
bodies, timestamps, business-limit flags and unknown timing and is safe to repeat.
The runtime uses trusted decisions for new records. See
[error diagnostics](../../docs/ERROR_REQUEST_DIAGNOSTICS.md).

## Retired Grok cache control

Migration 280 removes `grok_client_tool_cache_enabled` only from Grok account
extra objects. Credentials, unrelated extras, foreign-platform rows and
non-object historical values are preserved. The retired switch no longer grants
hosted search tools or changes cache routing. See [Grok](../../docs/providers/GROK.md#cache-and-tool-declarations).

## Codex native API-key identity and manual versions

Migration `290_codex_native_identity_and_manual_versions.sql` removes the retired
Codex synchronization settings and the `openai:apikey` default-family mapping.
Foreign account identity candidates are discarded atomically; credential-owner
UA, explicit version pins, valid Codex selections, upstream supplier mappings
and every unrelated provider setting remain intact. Runtime ignores stale
synchronization values and rejects foreign API-key families before cleanup.

Migrations 291–293 align the [official agent baselines](../../docs/OFFICIAL_AGENT_BASELINES.md):
Grok API-key native identity, DeepSeek anonymous Harness owner UUIDs, and removal
of linked ZCode copied project secrets in favor of memory-only PAT issuance.
Manual API keys and valid native identity pins are preserved.

Migration `294_outbound_identity_ids.sql` persists official-agent request-state
identifiers by namespace and irreversible owner-scope digest. Both independently
deployed 295 migrations retain their original identities as described above;
neither existing SQL file is renamed to resolve the historical prefix collision.

### Codex observation ownership (296)

Weekly reset observations retain a principal/proxy baseline generation and the
exact credential generation for pending corroboration. Queued follow-reset
events retain their verified principal/proxy generation (or the exact grant
when the principal is unknown) and are skipped
when that upstream principal or proxy no longer owns the source account. Legacy empty generations
cannot authorize a reset. Ordinary token rotation preserves a known principal's
baseline, while pending samples cannot corroborate across rotating grants.
