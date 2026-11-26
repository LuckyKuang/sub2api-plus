Sub2API Plus v0.2.25+custom.001

## Highlights

- Integrates official v0.2.25. Account header overrides gain `{{user_id}}` / `{{api_key_id}}` / `{{group_id}}` / `{{request_id}}` identity-report variables: only API-key accounts resolve them, identity header names stay blocked, OAuth accounts drop the whole header, unresolved variables drop it while removing a forged client copy, client-supplied request IDs are sanitized, shared model lists resolve without an inbound scope, and the OpenAI WS pool buckets connections by the reported identity. Ops error events record only variable-generated header values.
- Hardens OpenAI passthrough error responses: configured base-URL hosts and account credentials are redacted, `error.type` / `code` / `param` accept scalars only and are length-bounded, and deterministic `invalid_encrypted_content` failures return the audited public code. The audited public error catalog remains the only source of user-visible text.
- Prices Zhipu GLM-5.4 in the channel pricing editor. GLM-5.4 now carries a same-model card mirrored from the reviewed GLM-5.3 card, so reference lookup reports `priced` and request settlement no longer depends on the platform fallback switch.
- Reworks the Codex settings surface for phones: the outbound identity block wraps instead of overflowing, and the upstream error catalog opens in a dedicated configuration dialog with an internal scroll region.

## Changed

- Bumps the application version to `0.2.25+custom.001`, the Git tag to `v0.2.25+custom.001` and the GHCR image tag to `v0.2.25-custom.001`.
- Keeps the retired upstream billing probe retired: upstream's upstream-group synchronization and probe cluster is intentionally excluded, so the probe route stays absent and no upstream-declared rate can rewrite `accounts.rate_multiplier`.
- Synchronizes the upstream mapping, seven-provider matrix, outbound identity, channel pricing and Zhipu provider documentation.

## Compatibility and migration

- Application version `0.2.25+custom.001`. Git tag `v0.2.25+custom.001`. GHCR image tag `v0.2.25-custom.001`.
- No SQL migration is added by this release.
- No environment variable, configuration key, Go dependency or frontend lockfile change is required; the gorilla/websocket pseudo-version from the upstream tag was already pinned.
- Header-override identity variables are limited to API-key accounts. Official OAuth and setup-token subscriptions never receive gateway user identity, and identity header names remain blocked by the outbound identity guard.
- OpenAI passthrough rules continue to match internally and record Ops evidence, but the audited error catalog owns the client-visible status, type, code and message.

## Known issues

- BigModel has not published a pay-as-you-go price for GLM-5.4. Its compiled card mirrors GLM-5.3 and must be replaced when the official price lands.
- Live provider entitlements depend on the credential owner's plan. Container verification uses synthetic providers for protocol coverage.
- Layout regressions are validated only inside the platform validation container; host fallback is intentionally unsupported.

## Upstream baseline

Official release: v0.2.25
Official commit: 348997267d756eab5729bb6b78a275a15d18796a
