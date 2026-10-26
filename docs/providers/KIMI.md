# Kimi / Moonshot

Native OAuth login is available in account creation and editing. See
[login, refresh, endpoints and operational details](DOMESTIC_OAUTH.md).

New OAuth authorizations persist an internal `oauth_login_id`; routine token
rotation preserves it. An old attempt cannot refresh or adopt a new login on
the same account row. Legacy grants retain their account-row scope until
reauthorization. This internal ID is never sent upstream.

Sub2API Plus accepts Kimi and Moonshot-compatible account endpoints through the
normal OpenAI-compatible account and channel configuration. This release adds
Kimi/Moonshot model, billing, and gateway compatibility updates from upstream
`v0.1.168`.

## URL Allowlist Boundary

When `security.url_allowlist.enabled: true`, the built-in upstream-host allowlist
now includes these additional outbound destinations:

- `api.kimi.com`
- `api.kimi.ai` (global managed OAuth inference)
- `api.moonshot.ai`
- `api.moonshot.cn`

The list only permits those exact hosts for configured upstream requests. It
does not permit arbitrary Moonshot subdomains, and it does not broaden the
separate private-host or HTTPS controls. When an operator overrides
`security.url_allowlist.upstream_hosts`, the override replaces the built-in
list; retain the required Kimi/Moonshot hosts explicitly.

With `security.url_allowlist.enabled: false`, URL allowlist validation is
disabled globally, so the hosts above are not an egress restriction. Enable the
allowlist in production deployments that require a bounded upstream egress
surface.

## Outbound client identity

Kimi and Moonshot OAuth/API-key accounts retain the Kimi Code client family.
Account parameters may override its global profile; cross-family selections and
the retired `kimi:apikey` mapping are rejected. The compiled identity is: `User-Agent: kimi-code-cli/2.3.1` plus the official companion
declarations `X-Msh-Platform: kimi_code_cli`, `X-Msh-Version: 2.3.1` and the four
device headers `X-Msh-Device-Name`, `X-Msh-Device-Model`, `X-Msh-Os-Version` and
`X-Msh-Device-Id`. The official client declares no `Originator` and no standalone
`Version` header, so neither is sent. `SUB2API_KIMI_CODE_VERSION` may select a
supported version; `2.1.1` is the accepted floor.

The gateway uses the fixed Ubuntu 24.04 / x86_64 environment documented in
[Outbound identity](../OUTBOUND_IDENTITY.md). Device model and OS version are
pinned, independent of the container hostname and host kernel. Device name
defaults to `ubuntu`; the device UUID is generated once and persisted. Only
name and UUID accept global or account overrides through **System Settings →
Outbound identity → Kimi Code → Runtime identity declarations**. The same-account
snapshot keeps the device declarations stable across retries.

Two consequences are deliberate:

- One deployment presents one device identity to upstream, while the official
  client presents one per end-user install. The declaration set matches; the
  cardinality does not. Override the device headers per account if an upstream
  rate-limits or risk-scores by device.
- A host description is editable, but the client family (`X-Msh-Platform`), its
  version companion (`X-Msh-Version`) and the User-Agent are derived
  declarations. The management API rejects naming them, so no configuration tier
  can send a platform token that disagrees with the selected family or a version
  companion that disagrees with the User-Agent.

The full precedence, validation and fallthrough contract lives in
[Outbound Identity](../OUTBOUND_IDENTITY.md).

## Operations

Use the configured account base URL supplied by the Kimi/Moonshot account.
Keep account credentials in the administrative account store rather than
configuration files or repository content. Gateway scheduling, account health,
and error handling remain the standard OpenAI-compatible behavior.

The canonical configuration example is
[`deploy/config.example.yaml`](../../deploy/config.example.yaml). Review the
full effective `upstream_hosts` list before enabling the allowlist in an
existing deployment, because every configured provider endpoint must be
represented there.

The **Kimi Code** settings card shows the complete effective block for OAuth
and API-key identity resolution. Both use the first-party host declarations from
`packages/oauth/src/identity.ts`; runtime values remain stable across retries
and version updates. An invalid account header candidate falls through as a
whole, and generic overrides cannot replace device or version companions. See
[source evidence and priority](../OUTBOUND_IDENTITY.md#domestic-provider-source-evidence-and-source-priority).

Kimi's inference SDK adds a protocol-specific `X-Stainless-*` block: Anthropic
SDK `0.95.2`, OpenAI SDK `6.34.0`, with a pinned supported Linux/arm64 Node
v22.19.0 fingerprint. OAuth/token calls retain the same UA/device identity without
those inference SDK headers. Settings show the resolved protocol variants; see
[SDK wire profiles](../OUTBOUND_IDENTITY.md#domestic-sdk-wire-profiles).

Model discovery follows the [reviewed Kimi Code source](../OFFICIAL_AGENT_BASELINES.md).
OAuth calls the regional `/coding/v1/models` with its account Bearer token and
trusted device snapshot. Catalog reasoning accepts `type: only|both|none` and
`effort.valid/default`; defaults outside the advertised efforts are discarded.
Optional malformed effort fields do not discard a recognized reasoning type;
unknown values inside an effort array are ignored. On Coding endpoints, absent
or null reasoning means thinking is disabled, so stale effort declarations and
registry metadata cannot re-enable it. Explicit `none`/`false` likewise clears
efforts and their default. Existing public/PAYG boolean reasoning catalogs remain
readable; missing reasoning on those endpoints remains unspecified.

## Managed usage and account recovery

Native Kimi OAuth usage queries use the fixed regional `/coding/v1/usages`
endpoint with Bearer access-token authentication and the same trusted device
identity as inference. A 401 refreshes the selected account once before retry;
concurrent token rotation is reused, and an invalid refresh grant allows account
failover. Shared refresh dependencies do not quarantine an otherwise valid grant.

Managed `usages.limit_5h`, `limit_7d`, `limit_month_total` and `limit_month_code`
map to independent five-hour, weekly, monthly-total and monthly-code windows.
Finite numeric or numeric-string `used_ratio` values become percentages; each
window retains its own reset time. Both monthly windows participate in scheduling
thresholds. The API-key `limits[].detail` / `usage` response remains supported.
Malformed responses and responses with no recognized valid window report a
failed probe without replacing the last valid quota snapshot.

Concurrent probes and monitor caches are isolated by credential/proxy generation.
After 401 recovery, conditional persistence binds usage to the refreshed grant
that actually supplied it. Reauthorization cannot inherit the old grant's quota
pause; legacy unbound windows become authoritative after the next successful
probe. See [quota snapshot ownership](DOMESTIC_OAUTH.md).

Moonshot PAYG balance probes likewise freeze the full API-key/proxy owner and
isolate concurrent work by its generation. Their balance metadata and threshold
pause/recovery writes are conditional on that owner. All three also compare the
cooldown deadline and reason seen before the probe, including a previously absent
cooldown, preserving newer reactive balance markers and failures. `success` describes the
provider response; `persisted` is true only after its conditional local write.
