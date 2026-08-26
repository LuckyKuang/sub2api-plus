Sub2API Plus v0.1.183+custom.002

## Highlights

- Preserve the OpenAI OAuth client identity as one coherent User-Agent, Originator, and Version triplet across every audited outbound path.
- Keep the immutable identity source order: credential-owning account, then global setting, then compiled default.

## Fixed

- Messages bridging, native Alpha Search, PAT web-search fallback, and OAuth model-manifest synchronization now finish with the account-aware identity resolver.
- Agent Identity task registration and its immediate retry now reuse one resolved identity snapshot, so a concurrent settings update cannot split the triplet.
- Account test and model synchronization fallbacks now honor the configured SettingService and credential-owner identity.
- Static path guards prevent inbound headers, force mode, generic overrides, retries, probes, and future endpoint staging from bypassing identity precedence.

## Compatibility and migration

- No database migration, configuration migration, or public API change is required.
- Existing valid account-level `credentials.user_agent` values retain priority; empty or invalid candidates continue to fall through to the next configured source.

## Known issues

None.

## Upstream baseline

Official release: v0.1.183
Official commit: e8cb019fabf8b55199436229044cbf9aa7a82564
