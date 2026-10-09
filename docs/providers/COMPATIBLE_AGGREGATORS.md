# Compatible API-key aggregators

The platform catalog (`backend/internal/domain/platforms.go`) owns platform
identifiers, display order, gateway selection and quota eligibility. Provider
profiles own supported modes and native endpoints. StepFun remains a registered
Plus platform with its region-specific Chat endpoint and domestic OAuth path.
Migration `281_drop_platform_check_constraints.sql` removes the superseded
platform CHECKs for composite routes and user quotas; API, service, repository
and Ent validators continue to reject unregistered platform identifiers.

## Cline

`cline` API-key accounts default to `https://api.cline.bot/api/v1`. Forwarding
uses Chat Completions; Responses and Anthropic Messages ingress are converted
through the existing audited adapters. Provider-prefixed model IDs are retained.
Automatic protocol selection cannot enable a native Responses or Messages
endpoint that this profile does not provide.

Credit models, `cline-pass/*` and `cline-free/*` have separate scheduling scopes.
Credit exhaustion cools only credit models. ClinePass exhaustion cools only
subscription models. Free-model limits remain model-specific. A wallet limit
does not disable the entire account or block another wallet. Connection tests
use the subscription test model when a usable ClinePass snapshot exists;
otherwise they use the configured/default credit model.

Official-host accounts query `/api/v1/users/me`, the returned user's balance,
and `/api/v1/users/me/plan/usage-limits` using the same Bearer API key. Balance is
microcredits converted to dollars (`balance / 1,000,000`, four decimal places).
The subscription windows are five hours, seven days and thirty days. A missing
subscription clears obsolete window snapshots. Custom-relay credentials are
never sent to the official control API.

## Command Code

`command_code` API-key accounts default to
`https://api.commandcode.ai/provider/v1`. Explicit account protocol and matching
account model rules take precedence. Otherwise `/models` `supported_endpoints`
permits the matching inbound protocol to pass through. Before a usable catalog
is available, built-in rules send Claude to Messages, GPT to Responses or Chat
as supported, and other models to Chat. Provider-prefixed model IDs are retained.

Model catalogs cache successful results for thirty minutes and failed refreshes
for five minutes. Background refreshes retain the selected account's identity
snapshot. Account, URL, credential, tenant header, proxy or resolved identity
changes cannot reuse another request's catalog or authorization-failure backoff.

Official-host usage queries call `/alpha/whoami` before selecting personal or
organization credits; failure to resolve the owner fails the probe. Credits,
subscription period and usage summary supply balance and window snapshots.
A replenished balance clears only cooldowns attributable to recovered usage
windows. Custom-relay keys do not access the official usage API.

## Shared Plus contracts

Both platforms use the configurable compatible-supplier identity policy in
[Outbound Identity](../OUTBOUND_IDENTITY.md), with the existing Codex preset as
the compiled default. A valid account selection takes precedence over the
global type/preset default. Forwarding, tests, discovery, catalog refreshes,
balances and quota batches use trusted declarations from the same credential
owner; ordinary header overrides cannot change identity.

All three HTTP ingress protocols enter [both audit engines](../SECURITY_AUDIT_CONTENT_COVERAGE.md)
before selection, billing, concurrency and upstream traffic. Protocol conversion,
model catalog discovery and account type do not introduce a bypass. Unknown
content follows the existing permissive extraction contract, while successfully
extracted current user content remains auditable. Provider-side Claude models
use the effective provider/model billing path and retain Plus channel pricing,
mapping and reservation/settlement rules.

OpenCode's generic gateways reject `gemini-*` and `jev-*` models that require
provider-specific endpoints after effective model mapping. This does not affect
the separate Gemini or TypeSafe gateways. OpenCode Zen `qwen3.8-max` uses Chat.
OpenCode usage refresh caps `Retry-After` at 24 hours; a 403 is reported as
forbidden and does not establish that a subscription is absent. The retired
upstream billing-probe endpoint remains removed.
