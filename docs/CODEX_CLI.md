# Managed official Codex CLI bridge and tasks

Sub2API Plus can install the reviewed official Codex CLI and bridge existing
gateway traffic for credential-owning OpenAI ChatGPT OAuth and API Key accounts. The
project pins CLI **0.165.0**. Installation never resolves `latest`, upgrades a
system installation, or changes account/global outbound identity declarations.

Two explicit switches control bridging: the global bridge environment and the
selected credential owner's account setting. Both default off. Installation or
detection never opts traffic in. Enabled traffic keeps the gateway's existing
authentication, canonical ingress audit, scheduling, concurrency, billing,
protocol conversion and conversation affinity. The reviewed official
`responses-api-proxy` supplies the HTTP transport hop; it does not run an agent,
execute local tools or select a different account.

The separate `POST /v1/codex-cli/responses` single-turn task endpoint remains an
`exec --json` agent interface with the narrower task contract below. It also
requires the global bridge switch. Its task-only limitations must not be applied
to the transparent bridge, and its tasks do not acquire conversation affinity.

## Installation and account setup

In **System Settings → Outbound identity**, inspect the pinned CLI runtime and
install the managed version or use a verified system CLI. A system `codex` is
eligible only when its native executable matches the embedded official binary
SHA-256 and exact pinned version. Detection supports PATH/symlink resolution
and the official npm package's known native-executable layout; a version string
alone is insufficient proof. An unverified candidate is never executed, even
to read its version, so its installed version remains unavailable. Unverified or mismatched installations require
the managed installer. Supported artifacts are macOS and Linux, each for arm64
and amd64; Linux uses the official musl artifact.

The authenticated administrator endpoints are:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/admin/settings/codex-cli` | Local runtime status; no download or release lookup |
| `PUT /api/v1/admin/settings/codex-cli` | Step-up protected explicit `bridge_enabled` boolean |
| `POST /api/v1/admin/settings/codex-cli/install` | Step-up protected installation of the pinned official artifact |
| `GET /api/v1/admin/accounts/:id/codex-cli` | Account configuration, masked auth status and opaque owner values |
| `PUT /api/v1/admin/accounts/:id/codex-cli` | Atomically save configuration and an optional write-only auth patch |

Installation failures return the static `CODEX_CLI_INSTALL_FAILED` reason;
transport and filesystem diagnostics are never returned. Refresh runtime status
to inspect the sanitized readiness reason before retrying.

The installer downloads only the embedded official release asset, checks its
embedded SHA-256 digest and verifies the executable version. The retained
release archive is part of later integrity verification. Files are stored
under `${DATA_DIR:-./data}/codex/cli/versions/0.165.0/`; the host's global
`codex` installation is left in place.

After the runtime is installed and verified, enable **Enable bridge environment**
in the same Outbound identity card and save. `bridge_enabled` is independent of
`execution_available`: the latter continues to report verified runtime readiness.
Enabling requires the exact installed release version, verified official source
and execution availability. Disabling remains possible when the runtime is no
longer ready. A rejected enable or failed storage write leaves the persisted
choice unchanged. Reads are bounded to two seconds and retain an earlier caller
deadline or cancellation, including background WebSocket protocol selection.
Read timeouts and storage failures do not authorize a native bypass for an
opted-in account. Settings API errors expose stable reasons rather than database,
proxy or filesystem diagnostics.

When the global bridge environment is enabled, both **Add account** and
**Edit account** expose **Official Codex CLI** in the same OpenAI OAuth or API Key settings
position for eligible accounts. With the global bridge disabled, or while its
status is unknown, both forms hide these controls. Opening either form refreshes
the global status, so reopening it after saving global settings uses a fresh
runtime response.
Hiding the controls preserves existing account configuration and does not turn
an account's saved opt-in off. Creation saves `enabled` and `config_toml` with
the account's credentials in the original create operation; it does not create
an account and then issue a second configuration write. Editing retains the
credential-owner comparison and write-only token controls below. Account opt-in
requires the global switch to expose its controls and a ready runtime to enable
the account. Changing either switch does not reset a session route.

The account's `config.toml` configures the independent agent task interface,
including its sandbox and local tools. The transparent transport bridge does not
execute those agent tools. Enabling Official Codex CLI during account creation
fills an empty `config.toml` with the server's defaults below, returned as
`default_config_toml` by the runtime status API. Existing custom
text is preserved, including when toggling the account setting repeatedly:

```toml
sandbox_mode = "danger-full-access"
approval_policy = "never"
cli_auth_credentials_store = "file"
```

An empty configuration restores those defaults. Each account supplies one
`config.toml`; saved root `sandbox_mode` settings apply to subsequent tasks.
If a partial edited configuration omits `sandbox_mode`, execution still uses
the project's Full Access default. An explicit supported sandbox setting wins.
The reviewed noninteractive `exec` interface always uses `approval_policy =
"never"`. Other approval policies are rejected on save because they cannot
provide interactive approvals through this task endpoint.
Top-level `profile` and `profiles` are rejected. External
profile files and legacy profile mappings are unsupported.

The file credential store and `project_root_markers` remain managed. Authentication, provider routing,
arbitrary credential environment values and outbound identity declarations
cannot be supplied in the TOML. Update checks, analytics, feedback, web search
and OpenTelemetry export are controlled off for task mode; account TOML cannot
reactivate them. Invalid TOML and rejected fields return sanitized errors without
echoing source lines. Switching from disabled to enabled requires a verified
official pinned runtime, otherwise the write returns HTTP 422
`CODEX_CLI_RUNTIME_UNAVAILABLE`. Editing or disabling an already enabled account
remains possible while the runtime is unavailable.

API-key, setup-token, AgentIdentity, personal-access-token and credential-shadow
accounts are ineligible. Ordinary account responses omit the entire managed
CLI configuration; use the dedicated account endpoint to edit it.

## Credential ownership and writes

The database's existing OAuth credential fields remain canonical. The CLI
configuration stores only `enabled` and `config_toml`; there is no second
persisted `auth_json` credential bundle. Reads expose the ChatGPT workspace ID
and booleans indicating whether access, refresh and ID tokens are configured.
Tokens are never returned.

The dedicated account PUT requires the initiating form's `expected_owner` and
`config_toml`; optional `enabled` preserves its current value when omitted.
An optional `auth.tokens` object accepts `account_id`, `access_token`,
`refresh_token` and `id_token`: omission preserves a value, a nonempty string
replaces it, and `null` clears it. Empty or masked token placeholders and
unknown fields are rejected. Replacement access/ID claims are checked for
consistency with the existing ChatGPT user/workspace; decoding claims does not
verify issuer authenticity. An opaque refresh token alone cannot establish a
different owner. Use the existing OAuth authorization flow to change accounts.

`expected_owner` binds the form to the credential grant, route and CLI
configuration. Credential or proxy changes require reloading the form. The
write also uses the existing database OAuth-owner compare-and-swap, so a login
or route change racing with the save cannot be overwritten. The returned
`oauth_credential_owner` refreshes the ordinary account editor's comparison
value. Unrelated account names, priorities and usage observations do not
invalidate the CLI form.

Save proxy changes separately before editing CLI settings or credentials. The
account editor rejects a combined proxy/CLI change before either write; reopen
the editor after saving the proxy to bind the CLI form to its new route.
API-key accounts also save API key or base URL changes separately from CLI
settings. A combined edit is rejected before either write and both drafts stay
in the form. Revert the CLI edits, save the credential or destination change,
then reopen the account and apply CLI settings against its new owner snapshot.
Key-only, CLI-only and unrelated metadata plus CLI edits retain their normal
save flow; this restriction does not relax owner CAS or reorder the writes.

Before each task, existing project token-provider locking/refresh and owner
checks acquire one account snapshot. The private execution `auth.json` uses
the official static `apikey` file mode with that owner's current access token
as its opaque `OPENAI_API_KEY` value. This execution projection does not change
the account's OAuth type. Refresh and ID tokens stay exclusively in the project
database; no duplicate full OAuth file is created. The controlled provider also
receives the same access bearer through its private process environment.

Native ChatGPT file mode can independently fetch cloud configuration and
refresh credentials before a custom provider is selected. The static execution
projection avoids those native OAuth paths, leaving login, refresh locks and
owner compare-and-swap with this project. CLI auth-file changes are never
imported into the database or used as a second refresh authority.

The execution overlay manages the account's root configuration and disables
plugin catalogs, remote plugins, apps, plugin sharing/recommendations, tool
suggestions and cloud skills. These features otherwise perform independent
startup requests, including GitHub catalog synchronization and ChatGPT catalog
fallback. Local task tools still follow the account's saved root sandbox and
approval policy.

Each execution supplies `-c project_root_markers=[]` so project discovery stops
at the fresh private working directory. A trusted ancestor Git project's
`.codex/config.toml` must not override the account's saved configuration or
reactivate independent network features.

## Transparent bridge and conversation affinity

When both switches are on, existing Responses requests reach the official
proxy after the gateway has resolved and prepared the credential owner's
destination, bearer, identity and proxy. Existing Chat and Messages conversion
paths continue to produce Responses requests through their owning adapters;
their parsing, error mapping, SSE processing and billing remain in the gateway.
Caller tool definitions, tool outputs, opaque response IDs, unknown sibling
fields and streaming bytes are not converted into a task prompt or synthesized
completion. The request method, original trusted URL, body and provider response
status/body are preserved by the bridge.

Transport selection is evaluated again for the next request, probe or accepted
WebSocket turn. The current global switch and the current credential owner's
account switch are an AND condition: both enabled selects the CLI bridge;
either disabled selects the existing unified native sender. Changing a switch
does not restart, replay or move an already submitted request. A live account
lookup reads only that owner's bridge flag; it does not replace the acquired
credential, identity or proxy snapshot. A known disabled switch selects native
transport even if the other switch cannot be read. When neither switch is
known disabled, failure to read a required permission is an error, not
permission to silently choose another transport.

The unified native sender retains the normal identity source order: valid
credential-owner `credentials.user_agent`, then valid global
`openai_codex_user_agent`, then the compiled default. Switching transport never
clears an account override, resets identity settings or substitutes CLI ambient
headers. Subsequent requests resolve identities under the same existing rules.

The official proxy has an HTTP Responses interface, not native upstream
WebSocket support. Bridge-enabled accounts use the gateway's existing
`http_bridge` WebSocket ingress adapter, which owns SSE-to-frame conversion and
history replay. The direct upstream WebSocket path is unavailable while
bridging is selected. This does not disable per-turn audit or replace the
ordinary HTTP/WebSocket conversation routing contract.

If bridging is enabled while a native upstream WebSocket is already open,
the accepted turn may finish, but the next turn cannot reuse that socket. The
gateway requests a reconnect before another upstream write; it retains the
session owner and generation. The reconnected client follows the existing
HTTP bridge continuation and full-history requirements.

For a connection already using the HTTP bridge adapter, each subsequent turn
selects the current CLI or native HTTP hop without reconnecting. Disabling
either switch therefore takes effect on the next turn of that connection;
enabling both on an existing native upstream socket uses the reconnect rule
above. Neither direction changes the session owner, generation or migration
lease, and a transport toggle does not authorize account failover.

Logical sessions retain their authorization-domain-scoped route, owner,
generation and migration lease. Global/account switches cannot clear or rewrite
that ownership, and bridge processes never maintain a second shared Codex
conversation home. Failover resolves the newly selected credential owner and
its identity/proxy anew; a request's acquired owner snapshot stays fixed while
it is sent. Opaque continuation IDs remain under the existing protocol and
history-replay rules and are not silently moved to an unrelated account.
Concurrency and transport resource accounting retain the logically selected
account, including a shadow's own limits; the credential owner determines the
provider bearer, workspace, identity and proxy.

Each transport hop starts a private, verified official proxy and a loopback-only
relay with a random one-use nonce. The nonce, not an OAuth JWT, is the proxy's
stdin API-key-shaped credential. The relay can enter only the one already
prepared provider callback: its original destination and authentication (OAuth bearer/workspace or API Key bearer)
are fixed by the gateway. Proxy-supplied URLs, headers or payload changes cannot
choose another owner or provider. The callback retains the existing plugin,
proxy and native TLS sender; the CLI does not replace trusted outbound identity
or signing declarations. Cancellation closes the body/process/relay resources.
Local bridge failures have stable error codes; after the provider callback has
started, the attempt cannot be automatically replayed through another transport
or account. Provider HTTP failures remain provider HTTP responses.
This includes proxy exit or relay read failure before the first response byte:
Responses, Chat, Messages and WebSocket adapters preserve the terminal bridge
error instead of turning it into account failover or temporary unscheduling.

Disabling the global switch prevents new bridge admissions and leaves saved
account configuration, installed artifacts and session routes intact. Ordinary
gateway requests then follow their existing native transport selection. The
separate task endpoint refuses execution while the global switch is off.

## Task protocol

`POST /v1/codex-cli/responses` uses ordinary gateway API-key authentication,
account/model admission, concurrency and billing. It requires a string `model`
and nonempty string `input`. Optional fields are string `instructions`, boolean
`stream`, and `reasoning: {"effort":"..."}`, where effort is `none`, `minimal`,
`low`, `medium`, `high`, `xhigh` or `max`.
When the request omits reasoning effort, the account's saved root
`model_reasoning_effort` remains effective.
Model names are at most 128 ASCII characters, beginning with a letter or digit
and otherwise containing letters, digits, `.`, `_`, `:`, `/` or `-`. Combined
`input` and `instructions` are limited to 1 MiB of UTF-8 bytes.

```json
{
  "model": "gpt-6-astra",
  "input": "Explain the difference between a process and a thread.",
  "stream": false
}
```

Other fields, structured/multiturn input, caller-defined tools, media and
continuation/resume requests return HTTP 400 during basic validation. Each
accepted request starts an independent CLI task. The CLI may execute its own
tools according to the saved sandbox and approval policy.

Session and conversation headers do not create task affinity. Tasks do not
read or write the ordinary HTTP/WebSocket conversation route or sticky-account
cache, so reusing a client session ID cannot change an existing conversation's
owner or apply its first-output migration timeout to a task.

Input plus instructions are limited to 1 MiB. CLI JSONL output is bounded at
8 MiB total and 2 MiB per line; stderr is bounded at 256 KiB and never exposed
in client errors or logs. Exceeding an execution output limit terminates the
task without replay.

A successful response is a completed Responses-shaped result with assistant
`output_text` and aggregate usage, including cached input usage. This limited
mapping does not implement the full Responses protocol. With `stream=true`,
the gateway waits for completion and emits a `response.completed` event and
`[DONE]`; it does not manufacture token deltas from CLI JSONL events.

Tasks have a 30-minute hard timeout. Cancellation terminates the child process
group. Once execution has started, the gateway never restarts or replays the
task, switches its account or uses an HTTP forwarding fallback, because tool
side effects may already exist. Usage already reported by the CLI remains eligible for billing
when a task fails or ends incomplete. Tasks have no continuation state.
The effective request or saved account reasoning effort is retained for the
existing pricing multipliers, including already reported usage on failures.

Tasks support **Standard service tier only**. The execution overlay always
sets the CLI's `service_tier = "default"` sentinel, which suppresses paid model
catalog defaults. Account TOML may omit this field or set it to `default`;
other values are rejected on save. The same group/global Fast policy is
evaluated before token lookup, task files or process startup. If its final
decision requires Fast or blocks the request, the endpoint returns HTTP 403
`cli_service_tier_policy_denied` without execution. Filtering a forced Fast
tier back to Standard permits the task. The fixed CLI can silently discard
paid tiers according to model metadata and does not report the actual tier in
JSONL, so a configured paid tier cannot establish correct usage billing.

## Isolation and deployment

Binary, Docker and Apple-container deployments use the same managed runtime.
The service user needs write/execute access to its data directory and HTTPS
access to the pinned GitHub release download. Persist `DATA_DIR` with the
application data volume. A binary on the Docker host is not automatically
available inside the container; install the managed runtime through the running
gateway's settings. The global choice is stored in the settings table as
`openai_codex_cli_bridge_enabled`, default `false`, with no environment override.
Account `credentials.codex_cli` retains its existing `enabled`/`config_toml`
shape. No database migration, additional volume or second credential bundle is
needed for bridge configuration.

Each execution receives
`${DATA_DIR:-./data}/codex/accounts/{account-id}/runs/{random}/home` as its
independent `CODEX_HOME`, with a sibling `workspace` working directory. Private
directories use mode 0700 and auth/config files use 0600. The task's run
directory, secrets and temporary tool state are removed when it exits. The
project never switches a service-wide or personal `~/.codex/auth.json` between
accounts.

The pinned CLI can merge system settings into the account's configuration,
and managed settings can override it. Task mode therefore requires the absence of
`/etc/codex/config.toml`, `/etc/codex/managed_config.toml` and
`/etc/codex/requirements.toml`. On macOS it also checks the service user's forced
preferences in domain `com.openai.codex` for `config_toml_base64` and
`requirements_toml_base64`, using the system Foundation API through `osascript`.
It inspects only whether these preferences are forced; it never reads their
contents. A conflicting source or an inconclusive check disables execution and
installation with `host_configuration_conflict` or
`host_configuration_check_failed`, while preserving detected version details.
The task runner repeats the check before process startup. Use a dedicated host
or container with an independent configuration environment for this mode.

One service OS user plus separate account/task directories provides logical
state isolation. **Full Access can execute tools with the service user's OS
permissions.** Directory separation and file modes do not create a strong
security boundary between processes sharing that user. In Docker, this includes
everything exposed to the service container by its mounts and permissions.
Deployments needing mutually untrusted account execution require independent
OS/container isolation beyond this mode.

All accepted task input enters the shared Responses security audit after basic
validation and before account selection, concurrency, billing, file creation
or process startup. Recognized ingress content is shared by Content Moderation
and Prompt Audit. CLI-internal model turns and tool output remain inside that
agent execution and are not represented as separately audited client turns.
See [security audit coverage](SECURITY_AUDIT_CONTENT_COVERAGE.md) and
[outbound identity](OUTBOUND_IDENTITY.md).

The reviewed source and release baseline are recorded in
[official agent baselines](OFFICIAL_AGENT_BASELINES.md). Runtime management,
version checks and task execution live in `backend/internal/codexcli`; account
validation and credential projection live in
`backend/internal/service/codex_cli_account.go`.

## Verification scope

Requirement-based backend tests cover installation/integrity failures, account
ownership and stale writes, independent concurrent task state, effective saved
permissions, audit ordering, usage on failure, cancellation, conversation-route
isolation and forbidden replay. Frontend tests cover runtime management and the
account editor's save, cancellation, late-response and partial-failure paths.
Run checks through the [validation container workflow](../CONTRIBUTING.md).

`python3 tools/test_codex_cli_wire.py` runs the pinned official Linux executable
against a local synthetic Responses server. Its transparent-proxy cases and
`codexcli/TestCodexBridgeOfficialBinaryWire` exercise the actual pinned
`responses-api-proxy`, including the Go nonce relay, unchanged request/response
bytes, terminal provider errors, one provider send and rejected local requests.
The Go wire cases also retain API Key authentication and custom destinations
for model GETs, Responses, image generation JSON and binary multipart image
edits, without introducing OAuth workspace headers.
Injected-process fixtures separately cover unverified runtime, payload tamper,
cancellation, cleanup and stable errors. Service tests cover both switches,
credential-owner snapshots, failover identity/proxy, forbidden native bypass and
unchanged conversation ownership across switch changes. Frontend tests cover
explicit global saves, ready-only enabling, unavailable-runtime disabling,
atomic account creation and stale/failed saves. These fixture and wire checks
do not establish a real OpenAI account's entitlement or live model acceptance.

The task cases check exact trusted headers,
account/global/default identity values, final model and usage, saved root
reasoning effort, unchanged credential snapshots and terminal 401 behavior.
Every case also verifies that no paid `service_tier` is sent.
The business fixture runs beneath a real, explicitly trusted ancestor Git
project whose configuration selects a different reasoning effort and enables
plugins, apps, cloud skills and keyring storage. The actual wire must retain
the account's saved effort, use the same unchanged credential snapshots and
make exactly one controlled provider request with no external proxy attempts.
Its proxy trap rejects auxiliary requests that honor the proxy environment; it does not
prove the absence of arbitrary direct sockets opened by task tools. Synthetic
subscription claims exercise credential projection without a production OAuth
account or a real OpenAI model request.
The real-binary harness executes Linux. The macOS forced-preferences command is
covered through its injected command boundary; it has not been executed on a
native macOS runtime by these Linux-container checks.


### OpenAI API Key accounts and the official CLI bridge

Credential-owning OpenAI API Key accounts use the same account `codex_cli.enabled`
and `config_toml` settings and default TOML as ChatGPT OAuth accounts. Both the
current global bridge switch and the current credential owner's account switch
must permit each new request/turn. Disabling either restores the existing unified
sender. An already submitted generation completes without replay; a native WS
connection requiring the bridge reconnects with 1012 before the next generation.
The HTTP WS adapter chooses the CLI hop again on each turn.

API Key bridging includes existing OpenAI compatible custom base URLs. The trusted
prepared request retains its endpoint, API Key bearer, account proxy and coherent
outbound identity; no ChatGPT workspace or OAuth token is added. Live configuration
reads select only the bridge boolean and never replace acquired credentials or
identity. The authenticated CLI configuration API exposes no API Key secret and
uses a configuration-only account/route CAS; OAuth auth patches are rejected for
API Key accounts. Standalone agent tasks and OAuth auth.json generation remain
ChatGPT OAuth only. The account TOML cannot override authentication, routing or
identity, and the transparent HTTP hop does not execute agent tools.

All existing HTTP/WS ingress audit, canonical content extraction, billing,
concurrency, session affinity and no-replay boundaries remain ahead of provider
writes. API Key bridge regression coverage includes eligibility and default
configuration, secret-free config edits, hot global/account switches, gateway and
probe endpoint/auth/identity/body preservation, native WS admission and absence of
OAuth workspace leakage.

The same switches cover model discovery, account connection/image/chat probes,
the existing internal Codex usage inference probe, and supported WHAM usage,
credit-detail and credit-consumption requests. They retain prepared protocol,
credential owner, proxy and headers. OAuth issuer login, refresh and revocation
remain separate operations. Enabling bridging generates no additional probe.
OAuth availability is `http_bridge_and_tasks`; API Key is `http_bridge_only`.
API Key custom endpoints and bearers never acquire OAuth workspace/token fields.
