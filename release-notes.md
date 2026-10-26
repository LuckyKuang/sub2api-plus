Sub2API Plus v0.2.18+custom.002

## Highlights

- Align the gateway's implemented OpenAI/Codex, Grok, DeepSeek, Kimi, MiniMax, StepFun and ZCode businesses with the reviewed official coding-agent contracts. Native protocols, HTTP conversions, account types, regions and plans have separate acceptance boundaries; this release does not claim complete coding-agent equivalence. See the [supported business scope](docs/SEVEN_PROVIDER_SUPPORTED_BUSINESS.md) and [official baselines](docs/OFFICIAL_AGENT_BASELINES.md).
- Harden Codex Responses HTTP/WS, Chat/Messages conversions, Live and control lifecycles. Tenant-scoped PostgreSQL session ownership and CAS failover keep subsequent requests on the committed owner across replicas, while rejecting stale credentials and observations. The compiled Codex identity is manually maintained at `0.164.0`; valid credential-owner identity still takes precedence over the valid global identity and compiled default. Automatic version synchronization is removed.
- Preserve Grok identity across OAuth/API-key inference, media, Realtime and control requests. Cover SSO/device states, isolate issuer failures to the original credential/proxy snapshot, and atomically recover only the matching owner. Enterprise deployment features remain outside this release's scope.
- Align DeepSeek Harness PKCE login/reauthorization and native Messages authentication, Kimi CN/global device login and refresh, and MiniMax managed OAuth versus API-key/BYOK Anthropic inference. Model discovery, wallet/quota observations and scheduling retain their platform, region and credential owner. MiniMax OAuth uses the actual four-step quota chain; DeepSeek does not invent an unsupported business-login refresh operation.
- Support Step-Code's native Chat Completions contract and buffered/streamed Responses and Messages HTTP conversions across six profiles: OAuth Plan, API-key Plan and API-key PayG in CN/global regions. Preserve tools, usage, cached-token pricing and model restrictions. The static model key stays separate from the optional Passport account session.
- Separate ZCode business login from short-lived project PATs, and derive Start Plan models and entitlement from its own Bearer/balance evidence. Harden off-peak Take/Status/Settle, add optional durable recovery and exact-segment administrative termination, and preserve original-owner settlement across credential changes.
- Add a bounded first Files capability: DeepSeek Anthropic image upload/lifecycle and native Messages references, plus MiniMax global API-key Chat `video_understanding` upload/references and local access revocation. Gateway IDs are scoped to the caller and credential owner; unsupported combinations fail explicitly. See [Files](docs/providers/FILES.md).

## Changed

- Capture trusted outbound identity and the actual credential owner's proxy before inference, login, discovery, retries, probes and background cleanup. Bound-proxy failures or missing snapshots cannot authorize a direct fallback. Proxy edits refresh account/scheduler views across replicas; late grants, quota results, model discovery and recovery writes cannot mutate replacement credentials or routes.
- Preserve observed usage on partial streams without replay, duplicate charging or a false completed response. StepFun tool blocks receive no deltas after closure; malformed completed tool arguments fail safely. Ordinary output-budget truncation remains Messages `max_tokens` or Responses `incomplete` with reason `max_output_tokens`, preserving truncated Responses tool arguments.
- Retain a known ZCode ticket after an uncertain or incomplete admission response; the next Status uses that ticket without another Take. Concurrent waiters receive the original quota rejection/reset time. Ingress audit still precedes selection, billing, slots and upstream writes; escaped JSON Files references receive the same ACL checks as literal references, while opaque content retains the documented pass-through behavior.
- Fence frontend authorization and proxy-probe results when the account, proxy or flow changes. Old callbacks cannot complete a replacement flow, publish stale errors or release its loading state. OAuth credential application now checks the flow-start owner on the server as well as in the UI.
- Restore usage tables and expand real-time request inspection. Unify the console around rounded solid surfaces, responsive workspaces, shared dropdowns, fixed filters/actions and independently scrolling tables; add global accent, background and transparency controls and remove backdrop blur. Available Channels exposes clearer pricing details.
- Add opt-in V1 streaming probes with first-text timing kept distinct from first-byte latency and real-request TTFT. V2 model visibility/exclusions preserve raw history and rollups. Unlimited quota no longer appears as false zero-percent usage or triggers misleading alerts.
- Keep CSP nonces coherent when HTML is revalidated, and fail Redis health checks on authentication errors.

## Compatibility and migration

This release builds on `v0.2.18+custom.001` and retains the same official `v0.2.18` baseline. Back up PostgreSQL before starting the new server. The following **11 forward-only SQL migrations** are added; existing migration filenames and checksums remain immutable:

| Migration | Purpose |
| --- | --- |
| `287_channel_monitor_stream_probe.sql` | Opt-in streaming-probe configuration; existing timing history is not backfilled. |
| `288_openai_session_routes.sql` | Authoritative tenant/session ownership, generations and migration leases. |
| `290_codex_native_identity_and_manual_versions.sql` | Remove automatic Codex version-sync settings and foreign OpenAI API-key identity candidates. |
| `291_pin_grok_native_agent.sql` | Remove foreign Grok API-key identity candidates and retired type-default mappings. |
| `292_deepseek_harness_anonymous_user.sql` | Persist a Harness anonymous UUID for each applicable credential owner. |
| `293_zcode_project_tokens_memory_only.sql` | Remove obsolete copied coding-project keys, off-peak keys and `X-Coding-Plan-Api-Key` overrides from linked accounts. |
| `294_outbound_identity_ids.sql` | Persist official-client random identities by namespace and irreversible scope. |
| `295_channel_monitor_v2_model_visibility.sql` | Update monitor defaults while preserving operator configuration and history. |
| `296_codex_observation_owner_generation.sql` | Bind reset observations/events to credential-owner generations. |
| `297_provider_files.sql` | Persist Files ACLs, metadata, reservations and deletion-recovery state. |
| `298_zhipu_offpeak_tickets.sql` | Persist off-peak segments, leases, encrypted cleanup snapshots and settlement state. |

Historical `287`, `288` and `295` prefixes intentionally coexist with other published migrations. Preserve the complete filenames; do not rename, renumber or remove them. Cleanup migrations delete retired settings and credentials. **Reverting the container image alone cannot restore deleted data or undo the schema changes.** A rollback needs a reviewed database restore/recovery plan. Preserve the new identity tables in backups to retain stable client IDs. See the [migration guide](backend/migrations/README.md).

Custom admin clients calling `POST /api/v1/admin/accounts/:id/apply-oauth-credentials` must submit `expected_owner`, captured from the account DTO's `oauth_credential_owner` **before authorization starts**. It is a 64-character lowercase hexadecimal digest. Missing/malformed values are rejected; an owner conflict requires restarting authorization. Reading the new owner and replaying an old grant is invalid. The bundled frontend implements this contract; credentials and recovery state commit atomically. See [authentication](docs/AUTHENTICATION.md).

Files use the local namespace `file-s2a-<UUID>` (`mm_file://` wraps it for MiniMax). ACLs bind the user, API-key row, group, platform and credential owner. Unknown, expired, revoked or uncertain local references fail closed; credential replacement may require re-upload, and inference never silently falls back to another owner. DeepSeek local references work only in native Messages; MiniMax local references work only in native Chat. Other conversion entries reject them. MiniMax deletion revokes local access and explicitly reports that remote deletion is unsupported; its remote list/metadata/delete remain unavailable. Neither provider supports cross-account re-upload. Files CRUD does not create Chat-token charges or imply storage/media-fee accounting.

`zhipu_offpeak.persistence_enabled` defaults to `false`; applying migration `298` does not enable durable recovery. Enabling it requires a dedicated shared external keyring: `ZHIPU_OFFPEAK_ACTIVE_KEY_ID` and `ZHIPU_OFFPEAK_SNAPSHOT_KEYS` (a JSON map of key IDs to base64-encoded 32-byte AES keys). `ZHIPU_OFFPEAK_SNAPSHOT_RETENTION_HOURS` defaults to `168` and accepts `1`–`720`. Missing/invalid keys prevent startup when persistence is enabled. All replicas need the same keyring; retain old keys until dependent tickets drain or reach retention, and do not reuse JWT, TOTP or payment keys.

Durable off-peak mode stores minimal encrypted original-owner/proxy cleanup evidence, never prompts or model responses. Unknown Take results are not blindly retaken; model requests are not replayed. Administrative inspection and exact `{record_id,generation}` termination are available only in durable mode; memory mode returns `501`. Accepted termination means retirement intent was persisted: admitted physical requests drain before original-owner settlement. Settle does not return rolling-window Take quota. See [ZCode recovery](docs/providers/ZHIPU.md#durable-off-peak-recovery).

## Known issues

- This is gateway capability alignment. Local workspaces, editors, terminal/TUI, local MCP, OS integration, tool-execution loops and all internal coding-agent recovery strategies are outside scope. Grok enterprise deployment, turn/resubmit/tracing lineage, doom-loop recovery and an exact Rustls TLS fingerprint are not implemented here.
- DeepSeek Files are limited to the reviewed Anthropic image family. MiniMax Files are limited to global API-key Chat video understanding; managed OAuth, other regions/purposes, unsupported remote management and cross-account re-upload remain unavailable. External native references and inline media keep their separate compatibility contracts.
- StepFun Responses and Messages are HTTP conversions, not upstream-native protocols. Responses WS, specialist speech/image endpoints and their additional fees are not implied by Chat support. Remote search MCP received a contract audit only; no search gateway is included.
- ZCode recovery cannot restore an official agent's historical queue position. Start and off-peak do not borrow coding-plan permissions. Business-login expiry requires reauthorization; PAT renewal is not login refresh. With durable mode disabled, off-peak tickets remain process-local and do not survive restart or provide cross-instance recovery.
- Real platform account permissions, regional/plan availability and complete browser/OS authorization require separate acceptance with actual accounts. Isolated provider responders do not prove those properties. The [recorded code verification](docs/SEVEN_PROVIDER_TEST_MATRIX.md#post-commit-branch-verification) includes complete backend unit/integration/lint, frontend checks/build, 14 Chromium management scenarios, independent MinIO storage and focused race checks in Apple Containers. It does not claim full-repository race or every protocol × account type × plan × lifecycle combination.

## Upstream baseline

Official release: v0.2.18
Official commit: ac1b8f1271be0d674fa6f074403c1410f30a3a81
