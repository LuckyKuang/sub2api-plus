## Why

Sub2API Plus currently tracks official `v0.1.169`, while official
`v0.1.170` includes correctness fixes for interrupted-stream billing, OpenAI
stream retry behavior, Codex Responses namespace handling, tool-output media,
subscription windows, content-moderation proxying, SMTP, image offload, and
payment-setting persistence. These changes touch Plus-modified gateway and
settings paths and must be merged deliberately.

Plus also provides a compiled Codex identity and an administrator-controlled
global identity, but some OAuth, PAT, reauthorization, shadow-account, model
manifest, and retry paths can lose an account-specific identity. A single
OpenAI account must not present different client identities across one logical
operation.

## What Changes

- Merge official `v0.1.170` commit
  `b22f73e725236790f97d89bf0c3b908a48e591d5` while preserving intentional
  Plus gateway, security, deployment, pricing, and release behavior.
- Release the resulting Plus baseline as `v0.1.170+custom.001` with embedded
  version `0.1.170+custom.001` and OCI tag `v0.1.170-custom.001`. The
  upstream embedded `0.1.169` value is not a source of truth for Plus.
- Resolve an outbound Codex identity once from the credential-owning account,
  then the global setting, then the compiled default; apply its paired
  `User-Agent`, `Originator`, and `Version` to every eligible OpenAI request.
- Bind an existing account to its server-side OAuth reauthorization session,
  preserve its validated custom identity on credential replacement, and use
  identity-aware enrichment and PAT validation.
- Add account-level validation and management for optional Codex User-Agent
  overrides, with Spark shadows inheriting the credential parent's identity.
- Preserve official v0.1.170 stream-billing, namespace, subscription, admin,
  moderation, image, email, payment, Grok, and pricing behavior.

## Capabilities

### New Capabilities

- `upstream-v0-1-170-sync`: Defines the official v0.1.170 baseline and its
  required Plus-preserved behavior.
- `openai-codex-outbound-identity`: Defines one consistent client identity for
  all Codex/OpenAI upstream operations.

## Impact

- **Compatibility**: No database migration or new deployment environment
  variable is required. Existing accounts without a custom UA inherit the
  global or compiled default. Existing invalid stored account UA values remain
  fail-safe at runtime but cannot be newly saved.
- **Billing**: Interrupted streams with observed upstream usage are now
  recorded once; subscription quota windows are aligned with subscription
  terms instead of a midnight-only anchor.
- **Security**: Generic account header overrides and inbound client headers
  cannot replace Codex identity headers. OAuth reauthorization account context
  is server-side session state, not a trusted browser-supplied account ID.
- **Release**: This change does not create, push, move, or delete tags,
  Releases, or images. Publication remains a separate explicit action.
