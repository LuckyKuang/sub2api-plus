## Baseline and branch strategy

Merge only official tag v0.1.171 at
f0e7a9c7a23a7d02fb159b62fa809621eb0475a6. Do not include local v0.1.172.
The merge base is the actual official v0.1.170 tag target
b22f73e725236790f97d89bf0c3b908a48e591d5. Resolve the normal merge on an
isolated branch so upstream ancestry remains explicit.

## Conflict ownership

Official behavior is authoritative for security, financial correctness,
concurrency, and new CAPTCHA capabilities. Plus is authoritative for module
identity, custom release rules, deployment topology, account quota extensions,
Agent Identity, and the one-source OpenAI outbound identity invariant.

For Codex identity, retain openai_outbound_profile.go and its credential-owner
resolution. Do not restore the upstream openai_codex_identity.go as a second
identity pipeline. Instead, adapt the upstream version-sync and normalization
logic to the existing resolver, then cover every HTTP, passthrough, WebSocket,
probe, model, and retry path with exact identity tests.

## Migration and generated code

Keep every existing migration unchanged. Rename the imported official
192_group_profit_control.sql and 193_group_profit_control_auth_cache_invalidation.sql
to the next unused local prefixes, preserving their SQL and ordering. Resolve
Ent schemas and Wire definitions first, regenerate Ent and Wire output, and
never hand-merge generated files.

## Configuration and CAPTCHA

Treat CAPTCHA as one feature slice: provider settings, fail-closed validation,
login/OAuth/passkey protection, administration UI, English and Chinese locale
keys, configuration examples, and CSP domains must move together. Existing
settings auditing, default/environment binding, and deployment policies remain
in effect.

## Version and release

After source integration, set the application version to 0.1.171+custom.001,
synchronize Docker arguments and release examples, and add an UPSTREAM.md row
mapping v0.1.171+custom.001 to official v0.1.171 and f0e7a9c7a23a7d02fb159b62fa809621eb0475a6
with planned status. Do not alter, retag, or reuse any prior published or
pending release tag.
