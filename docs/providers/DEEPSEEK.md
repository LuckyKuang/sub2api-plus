# DeepSeek

Native OAuth login is available in account creation and editing. See
[login, refresh, endpoints and operational details](DOMESTIC_OAUTH.md).

PAYG balance probes freeze the complete API-key and proxy snapshot before sending
the request. Concurrent probes share work only within that generation. A late
response cannot update a replacement key's balance metadata or cooldown. Metadata,
pause and recovery also compare the exact cooldown deadline and reason observed
before the probe, preserving newer reactive balance markers and cooldowns.
The `success` field reports a valid provider response, while `persisted` requires
the conditional local write to have committed. Missing conditional storage never
authorizes an unconditional state change.

Sub2API Plus accepts DeepSeek accounts through the OpenAI-compatible gateway
and Claude Code-style Anthropic entry points.

## Empty model mapping

When a DeepSeek account has no `model_mapping`, the scheduler admits only the
supported request names (case-insensitive):

- `deepseek-flash`
- `deepseek-v4.1-flash` (versioned request name; upstream determines support)
- `deepseek-v4-pro`
- `deepseek-v4-flash` (compatibility alias; upstream still accepts it)
- `deepseek-v4-flash-0731` (versioned flash name)
- `deepseek-v4-flash-vision-exp` (compatibility alias)
- `deepseek-v4-pro-0813` (versioned pro name)

The scheduler and model selectors share the [model catalog](../CN_PROVIDER_MODELS.md),
which displays Flash aliases and versioned Flash/Pro names separately.
With no explicit administrator mapping, their spelling is sent upstream unchanged;
the project does not rewrite aliases to `deepseek-flash` or pin that name to V4.1.
The current pricing page recommends `deepseek-flash` and confirms the two V4 Flash
compatibility names; it does not separately document `deepseek-v4.1-flash` as an API ID.

Unknown names fail at scheduling with `404 model_not_found`. They are not
forwarded upstream, so they cannot trigger per-(account, model) cooldown or be
silently rewritten to `deepseek-flash` by DeepSeek.

Claude Code's `ANTHROPIC_MODEL=deepseek-flash[1m]` form is normalized before
the whitelist check, and the `[1m]` suffix is stripped on the DeepSeek outbound
path only. Configured `model_mapping` keeps mapping-first semantics. OpenAI
passthrough accounts still skip mapping admission and send the client model
unchanged.

## Outbound identity

DeepSeek OAuth and API-key accounts use the pinned **DeepSeek** preset by default
(`nativeOutboundPreset`), so DeepSeek receives exactly one declaration:

```
User-Agent: deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)
```

The parenthesized product comment belongs to that same User-Agent value
(published-harness attribution), so no `Originator` or `Version` header is sent.
The preset replaced the earlier Codex default, which had sent
`Originator: codex_cli_rs` and `Version: 0.158.0` alongside the Codex
User-Agent; those two declarations are now absent on this platform. The harness
`x-deepseek-harness-user-id`, `-session-id` and `-compact` request headers keep
their protocol-layer ownership and are never rewritten by the identity layer.

OAuth and API-key accounts retain the Harness family. System Settings configures
the global DeepSeek profile; valid account parameters can override that profile.
There is no configurable `deepseek:apikey` mapping or cross-family account override.
Protocol selection
(`api_protocol` / adaptive routing), API-protocol conversion, model admission,
billing and the ingress audit order are unaffected: identity chooses outbound
declarations only. Ingress audit order and Plus session/quota accounting are
unchanged.

The **DeepSeek · Harness** card follows the official
[deepseek-harness source](../OFFICIAL_AGENT_BASELINES.md) directly. The compiled
version is the reviewed `0.2.1-rc.3` prerelease; valid pins at `0.2.0-rc.2` or
newer retain their priority. It uses no desktop-shell version.

Native Messages requests carry an independently persisted anonymous UUID v4 in
`x-deepseek-harness-user-id` and `session-<UUIDv4>` in the session header,
following the official UI/session-controller default. Other official entrypoints
accept other session formats; inbound formats remain valid routing signals.
Explicit tenant/conversation/owner mappings are persisted atomically across
turns and restarts. Retries retain both IDs; failover resolves another owner.
New authorizations persist an internal `oauth_login_id`; normal token rotation
keeps it, while relinking the same account row or replacing an API key creates
a separate session mapping. Legacy grants without this field retain their
account-row scope until reauthorization. It is never sent upstream.
Without a reliable session signal, only the current logical request shares an
ID. Migration 292
backfills existing owners. These values contain neither a provider user ID nor
a credential. Caller attribution/compaction headers are removed; no gateway
native compaction operation is invented. Discovery/control/Chat extensions do
not acquire native Messages turn headers.

Native streaming Messages use `Accept: text/event-stream`; the non-streaming
gateway extension uses JSON. Foreign Claude/Codex session and SDK runtime
headers are removed. Files and mid-conversation tool-change beta declarations
are derived from the actual native body.

OAuth model discovery calls `/models` with `x-dsh-auth-token`. Official-origin
API-key accounts explicitly using native Anthropic Messages call `/models` with
`x-api-key`. Both paths freeze the acquired owner and selected identity through
response decoding and conditional metadata persistence. Listed models
require usable `api_capabilities.anthropic_messages`, text output, supported
input modalities and valid positive capacities. Explicit Harness minimum
versions gate availability against the selected trusted version; without a
minimum, only built-in `deepseek-flash` and `deepseek-v4-pro` are offered.
Unknown rows and array values are skipped without hiding recognized modalities;
duplicate IDs retain the first row, following Harness's decoder.
API-key Chat/Responses/adaptive and custom compatible discovery retain their
own catalog semantics; they are not filtered to native Messages capabilities.
No OAuth token is used for API-key discovery.
