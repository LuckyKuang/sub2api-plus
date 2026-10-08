# Kimi / Moonshot

Native OAuth login is available in account creation and editing. See
[login, refresh, endpoints and operational details](DOMESTIC_OAUTH.md).

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
the retired `kimi:apikey` mapping are rejected. The compiled identity is: `User-Agent: kimi-code-cli/2.1.1` plus the official companion
declarations `X-Msh-Platform: kimi_code_cli`, `X-Msh-Version: 2.1.1` and the four
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
