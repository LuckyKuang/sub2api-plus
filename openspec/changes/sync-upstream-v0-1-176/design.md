## Baseline

| Item | Value |
| --- | --- |
| Plus HEAD | `bd2f9e7a4872c345bae5b127ad121f706ca2aceb` (`v0.1.173+custom.004`) |
| Current Plus baseline | official `v0.1.173` / `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` |
| Merge input | official annotated tag `v0.1.176` |
| Merge input commit | `e803e3851c0a7e222cfadeafad7b8636ab959d11` |
| Merge-base | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` |
| Divergence | Plus +122 / official +107 from the 173 merge-base |
| Official 173→175 | 84 commits, 114 files, +7319 / -351, no SQL |
| Official 175→176 | 23 commits, 101 files, +3668 / -317, one SQL |
| Official 173→176 | 194 files, +10986 / -667 |
| `v0.1.174` | does not exist |
| `upstream/main` vs 176 | +5 commits / 9 files / +110 / -16 |

Merge official tag `v0.1.176` only. Do not merge `upstream/main` and do
not cherry-pick post-176 commits (`fd82dfd52`, `e29b93a1f`, `e215c98c2`).
Those stay on the next official baseline.

## Ownership

| Area | Owner | Rule |
| --- | --- | --- |
| README / DEV_GUIDE / VERSION / Docker args / `UPSTREAM.md` | Plus | Keep Plus branding. After implementation, retarget to `0.1.176+custom.001` and official `v0.1.176`. |
| `frontend/package.json`, `frontend/pnpm-lock.yaml` | Plus | Official 173→176 does not touch either file. Keep the Plus lock graph. |
| Published SQL `001`–`219` | Plus | Immutable. Do not rename, edit, or reuse official numbers. |
| Official `221_group_model_pricing.sql` | Official SQL, Plus filename | Import as `220_group_model_pricing.sql`. |
| Group fields `five_hour_limit_usd`, `allow_live`, profit-control | Plus | Keep. |
| Group fields `model_pricing`, `long_context_pricing_enabled` | Official | Take. Default `long_context_pricing_enabled=true`. |
| Codex UA / Originator / Version source precedence | Plus, immutable | Valid account UA > valid global UA > compiled default. Fingerprint must not change the selected source or client family. |
| Codex client-profile enforcement | Plus | Keep on Responses HTTP, Chat Completions, Messages, Count Tokens, Alpha Search, and WebSocket. |
| OAuth session access policy and namespace | Plus | `resolveOpenAIUpstreamSessionID` is the only session-namespace seam. Unauthorized groups fail closed. |
| Codex fingerprint convergence | Official | Apply after Plus authorization and namespace resolution. Outbound device/session/thread rewrite only. |
| `usage_logs.session_id` | Plus | Persist the sanitized client-original identifier. Never persist a converged UUID. |
| Response-model billing and admission hardening | Official | Take. Keep Plus usage session-id and request-id columns visible. |
| Grok 4.6, JWT tier, `/x_search`, group model pricing UI | Official | Take. |
| Grok cross-client mapping opt-in and password OAuth disable | Plus | Keep 173 safety defaults. |
| Channel monitor default | Plus | Stay on V1. V2 remains explicit opt-in. |
| Risk-control backend failure | Official | Take the 175 revert: do not fail closed when the backend is down. |
| Generated Ent / Wire | Regenerated | Resolve `backend/ent/schema/group.go`, then regenerate. Do not hand-merge `mutation.go`, `runtime.go`, or `migrate/schema.go`. |

## Session policy vs fingerprint

These layers share the word "session" and some of the same headers, but
they solve different problems.

Plus session policy decides **who may use which OAuth namespace**:

- Unconfigured OAuth accounts keep API-key isolation.
- An enabled policy shares one account-scoped namespace across allowed
  groups of the same user.
- An unauthorized group fails closed before any upstream write.
- The same helper covers ordinary HTTP, passthrough HTTP, and WebSocket.

Official fingerprint convergence decides **how many devices OpenAI sees**.
It is not Plus OAuth session-policy and not `usage_logs.session_id`.

Modes:

| Mode | Upstream view | Rewrite |
| --- | --- | --- |
| `off` | N devices + N sessions | none |
| `device` | 1 device + N sessions | installation only |
| `session` (unset default) | 1 device + 1 session + N threads | installation + session; thread derived from the client-original `session-id` |
| `full` | 1 device + 1 session + 1 thread | installation + session + thread all account-constant |

`session` rewrites these outbound carriers, using one shared ID set for
headers and `client_metadata`:

- `x-codex-installation-id`
- `session-id` and `session_id` → account-constant UUID
- `thread-id` and `x-client-request-id` → UUID derived from
  `accountID + client-original session-id`
- `x-codex-window-id` → that thread + `:0`
- `x-codex-turn-metadata` and body `client_metadata` fields
  `installation_id`, `session_id`, `thread_id`, `turn_id`, `window_id`

It does **not** rewrite User-Agent, Originator, Version, `sandbox`, or
`thread_source`. Compact requests skip rewrite. API-key accounts stay
`off`.

The Plus outbound identity triple is a different layer and does not
collide with `session` mode:

| Layer | Fields | Source |
| --- | --- | --- |
| Plus identity triple | `User-Agent`, `originator`, `version` | account UA > global UA > compiled default |
| Official `session` fingerprint | installation / session / thread / window / turn | `accounts.extra.codex_fingerprint_mode` |

Official 176 applies fingerprint headers first, then
`enforceCodexIdentityHeaders`. Fingerprint never writes the triple, and
identity never writes installation/session/thread. Keep that order. Do
not let fingerprint code, header allow-lists, or body transforms change
the selected identity source, client family, Originator, OS,
architecture, or terminal fingerprint. Version sync may still update
only the version declarations of the already selected identity.

Concrete `session` cases are in the companion spec
`codex-session-and-fingerprint`. Unset OAuth accounts follow official
`session`.

Required composition:

1. Ingress client-profile checks stay first and unchanged.
2. Plus session policy resolves access and the cache/continuation
   namespace. A deny never reaches fingerprint rewrite.
3. Fingerprint reads the **client-original** hyphenated `session-id` only
   as the `session`-mode thread seed. It must not read the already
   isolated or shared Plus namespace ID as that seed.
4. Fingerprint may rewrite Codex fingerprint carriers after step 2.
5. Fingerprint must not overwrite Plus usage-log extraction, must not
   change UA / Originator / Version, and must not reopen a denied group.
6. `session` / `full` modes intentionally collapse outbound Codex
   session/thread cardinality for one OAuth account. That is an upstream
   visibility choice and does not relax Plus access control.

Unset OAuth accounts use official `session`. Existing Plus OAuth
accounts therefore start converging installation and session IDs on
upgrade unless an administrator stores `off`.

## Migration renumbering

Plus local maximum is `219_ops_error_routing_diagnostics.sql`. Prefix
`220` is unused.

| Official file | Plus file | Notes |
| --- | --- | --- |
| `backend/migrations/221_group_model_pricing.sql` | `backend/migrations/220_group_model_pricing.sql` | Same SQL. Adds `groups.long_context_pricing_enabled BOOLEAN NOT NULL DEFAULT TRUE` and `groups.model_pricing JSONB`. The follow-up `UPDATE` keeps existing rows true. |

Do not keep the official `221_` filename. Official 173–176 contain no
other SQL. Do not add compatibility checksums for a draft of 220.

After schema resolution, regenerate Ent. The generated-file conflicts
listed below are discarded in favor of that regeneration.

## Conflict surface

`git merge-tree HEAD v0.1.176` reports **41 content conflicts**. Official
173→176 does not conflict on the frontend lockfile.

### Branding and version — take Plus, then retarget

- `DEV_GUIDE.md`
- `README.md`, `README_CN.md`, `README_JA.md`
- `backend/cmd/server/VERSION`

### Group pricing — combine both sides

- `backend/ent/schema/group.go`
- `backend/internal/handler/admin/group_handler.go`
- `backend/internal/handler/dto/mappers.go`
- `backend/internal/handler/dto/types.go`
- `backend/internal/service/admin_group.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/service/billing_service.go`
- `frontend/src/views/admin/GroupsView.vue`
- `frontend/src/types/index.ts`

Keep Plus five-hour, live, and profit-control fields. Add official
`model_pricing` and `long_context_pricing_enabled`. Then regenerate Ent
instead of resolving:

- `backend/ent/migrate/schema.go`
- `backend/ent/mutation.go`
- `backend/ent/runtime/runtime.go`

### OpenAI gateway — manual composition, not ours/theirs

- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_gateway_passthrough.go`
- `backend/internal/service/openai_gateway_response_handling.go`
- `backend/internal/service/openai_gateway_grok_chat_bridge.go`
- `backend/internal/service/openai_images_responses.go`
- `backend/internal/service/openai_oauth_service.go`
- `backend/internal/service/openai_privacy_service.go`
- `backend/internal/service/openai_responses_item_id.go`
- `backend/internal/service/openai_ws_v2/passthrough_relay.go`
- matching tests listed by merge-tree

Replace official `isolateOpenAISessionID` call sites with Plus
`resolveOpenAIUpstreamSessionID`. Keep official fingerprint apply sites,
empty-completed failover, deterministic-400 passthrough, HTML 403
classification, nested usage parsing, and visible TTFT fixes.

### Account and usage UI — keep all three Plus/official surfaces

- Account create / edit / bulk modals: Plus session policy + client
  profile copy, plus official `codex_fingerprint_mode`.
- `frontend/src/i18n/locales/{en,zh}/admin/accounts.ts`: keep the Plus
  scheduling-path fix and the official threshold-nesting fix, then add
  fingerprint strings.
- Usage tests: Plus session-id column and official request-id column
  both stay visible.

### Compatibility types

- `backend/internal/pkg/apicompat/types.go`
- `backend/internal/server/api_contract_test.go`

Take official `x_search` fields and sources extraction. Keep Plus
contract assertions that already encode client-profile or session-policy
behavior.

## Auto-merged files that still need a human pass

Git will not stop on these, but they sit on Plus-owned seams:

- `backend/internal/handler/openai_gateway_handler.go`
- `backend/internal/service/openai_gateway_chat_completions.go`
- `backend/internal/service/openai_gateway_messages.go`
- `backend/internal/service/openai_gateway_scheduling.go`
- `backend/internal/service/openai_gateway_upstream_errors.go`
- `backend/internal/service/identity_service.go`
- `backend/internal/service/wire.go`
- `frontend/src/components/admin/usage/UsageTable.vue`
- `frontend/src/views/admin/UsageView.vue`
- `frontend/src/views/admin/AccountsView.vue`

Treat an auto-merge as unreviewed until the ownership rules above are
checked in the resolved file.

## Official behavior to take

From 175:

- Codex fingerprint convergence (`openai_codex_fingerprint.go`)
- Safe upstream response-model billing
- Large-file backup parts
- HTML 403 is not an account penalty
- Empty `response.completed` failover
- Deterministic Responses 400 is not rewritten to retryable 502
- OAuth image-stream error failover
- Codex capacity exponential backoff
- Nested `data` usage envelopes
- Visible-output TTFT and WS v2 terminal-event exclusion
- Personal OAuth expiry no longer overwritten by workspace entitlement
- Revert risk-control fail-closed
- Simple-mode security-audit menu
- API-key quota and expiry validation

From 176:

- `grok-4.6` catalog, pricing, and request path
- JWT subscription-tier detection
- Group `model_pricing` and `long_context_pricing_enabled`
- `POST /x_search` and Chat↔Responses `x_search` preservation
- Backup leader lock
- Invalidate channel cache on group platform change
- Inconclusive Responses probe stays unknown
- Realtime audio billed only after audio is observed
- Unregistered Grok text models fall back to the grok-4.5 card

## Plus behavior that must survive

- Custom module path, branding, and `+custom.NNN` release mapping
- Codex identity source precedence
- Client-profile enforcement on every documented ingress
- OAuth session policy and fail-closed unauthorized groups
- Persisted client session IDs on usage rows
- Five-hour Codex quota
- Channel monitor V1 default
- Grok mapping opt-in and hard-disabled password OAuth
- Frontend security-override lock graph

## Implementation sequence

1. Create `upgrade/upstream-v0.1.176` from current `main`.
2. Merge `v0.1.176`. Do not resolve generated Ent files by hand.
3. Renumber official 221 → Plus 220.
4. Resolve branding, group schema, gateway, and UI conflicts using the
   ownership table.
5. Review the auto-merged gateway and usage files.
6. Regenerate Ent and Wire.
7. Set version metadata to `0.1.176+custom.001` with status `planned`.
8. Run the 173-era full verification set plus the new fingerprint,
   session-policy, 403, empty-completed, and group-pricing regressions.

This assessment does not implement those steps.
