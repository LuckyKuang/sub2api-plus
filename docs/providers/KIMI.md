# Kimi / Moonshot

Sub2API Plus accepts Kimi and Moonshot-compatible account endpoints through the
normal OpenAI-compatible account and channel configuration. This release adds
Kimi/Moonshot model, billing, and gateway compatibility updates from upstream
`v0.1.168`.

## URL Allowlist Boundary

When `security.url_allowlist.enabled: true`, the built-in upstream-host allowlist
now includes these additional outbound destinations:

- `api.kimi.com`
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
