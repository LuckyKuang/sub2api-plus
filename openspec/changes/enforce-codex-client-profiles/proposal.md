## Why

The account option currently labelled "only allow Codex official clients" is a
security boundary, but its implementation combines independently spoofable
headers, an arbitrary `x-codex-*` prefix, and a broad App Server escape hatch.
It also does not protect every OpenAI-compatible ingress path. In particular,
Responses WebSocket and `/v1/messages` can consume an account that has the
option enabled without going through the same gate.

OpenAI exposes Codex through multiple surfaces (CLI, desktop app, IDE
integrations, and App Server). Those surfaces can share one network identity,
so a growing list of UI product names is not a safe or maintainable access
rule.

## What Changes

- Replace the account-side authorization meaning of `codex_cli_only` with a
  versioned built-in Codex client-profile classifier. A profile represents a
  verified network identity, not a UI product name.
- Require a coherent `User-Agent` and `Originator` pair plus a valid Codex
  engine version for built-in profiles. Remove authorization fallbacks based on
  either header alone, a UA trailer, case-insensitive `Codex ` matching, or a
  substring match.
- Keep reviewed official transport identities and the upstream product-family
  rule in built-in profiles, so documented desktop and IDE surfaces are not
  reduced to guessed product-name aliases. Preserve explicitly configured
  non-official compatibility entries as a separate,
  auditable allow path; they are not classified as official profiles.
- Retain the four historical identities `codex_app`, `codex_exec`,
  `codex_sdk_ts`, and `codex_vscode_copilot` only behind a new global,
  default-off compatibility switch. This is a closed migration set with the
  same coherent identity, version, and known-evidence requirements; it never
  expands the official registry or authorizes arbitrary `codex_*` values.
- Replace the arbitrary `x-codex-*` fingerprint requirement with a fixed set
  of known non-empty Codex header names. The evidence remains a heuristic and
  is never described as binary attestation.
- Remove generic App Server allow switches and the per-account App Server
  override from authorization. Remove `ForceCodexCLI` from authorization; it
  remains an outbound behaviour flag only.
- Enforce the same account policy for Responses HTTP, Chat Completions,
  Anthropic Messages, Anthropic Messages Count Tokens, Alpha Search, and
  Responses WebSocket before an upstream credential or connection is used.
  Candidate selection skips an ineligible account without recording an account
  health failure.
- Update the admin description to state that this is a Codex request-feature
  restriction, not proof that the client binary is official or unmodified.

## Impact

- Security boundary: enabled accounts reject incomplete or mismatched Codex
  identities on all supported OpenAI-compatible ingress paths.
- Public/admin behavior: the old generic App Server and configurable generic
  fingerprint controls are removed; custom integrations use the explicit
  compatibility allow-list path instead.
- Compatibility: official desktop and IDE surfaces are accepted through their
  verified underlying network profiles. A new upstream transport identity
  requires an explicit profile update with source and regression fixtures;
  production does not fetch remote rules dynamically.
- Persistent policy/API/UI: the global compatibility switch changes both the
  enabled-account ingress decision and validation of administrator-configured
  account/global outbound identities, so a stored legacy identity falls
  through normally when compatibility is disabled.
