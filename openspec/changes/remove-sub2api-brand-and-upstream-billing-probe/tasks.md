## 1. Specification

- [x] 1.1 Define billing-probe retirement, narrow outbound identity cleanup, and public private-metadata cleanup.
- [x] 1.2 Record protected product identity and compatibility contracts and prohibit introduction of a generic replacement label.
- [x] 1.3 Define preservation of ordinary manual multipliers and local group profit control.

## 2. Billing interop and probe removal

- [x] 2.1 Delete `GET /v1/sub2api/billing`, its handler/tests, and the API-key auth special case.
- [x] 2.2 Delete probe runner, admin routes/handlers, settings, extra keys, lifecycle bindings, and Wire providers; regenerate Wire.
- [x] 2.3 Remove probe fields from account DTO/service/repository/cache/CRS/scheduler paths while preserving manual `rate_multiplier` and unrelated OAuth scheduling behavior.
- [x] 2.4 Remove account probe UI, types, clients, settings, locales, and tests while preserving manual-rate editing.
- [x] 2.5 Strip retired probe keys at the import boundary and add forward migration cleanup plus scheduler outbox invalidation.

## 3. Outbound third-party protocol identity

- [x] 3.1 Remove unnecessary system-generated project UA/query/default-body/object-name identifiers while retaining official provider identities and product/payment copy.
- [x] 3.2 Reject reserved branded override header names and add a final outbound guard without rejecting arbitrary values containing the product name.
- [x] 3.3 Add focused regressions proving user/model content remains byte-preserved and protected Codex identity precedence is unchanged.

## 4. Public private protocol metadata

- [x] 4.1 Remove locally generated `X-Sub2API-*` response metadata and prevent `additional_allowed` from restoring those names.
- [x] 4.2 Rename the unshipped Grok cache control to `X-Grok-Client-Tool-Cache`, keep its behavior local, and block it from HTTP/plugin/WebSocket upstreams.
- [x] 4.3 Neutralize implementation-specific structured error domains while preserving standard `Retry-After` and `X-RateLimit-*` behavior.
- [x] 4.4 Add regression checks for protected product defaults, admin WS protocol, backup types, and generated client identifiers.

## 5. Verification

- [x] 5.1 Run focused backend Go tests and migration tests while iterating.
- [x] 5.2 Run frontend lint, typecheck, and relevant Vitest.
- [x] 5.3 Run repository-required generated-code and OpenSpec validation checks.
- [x] 5.4 Review the final diff against `main` for removed business surfaces, preserved protected values, and unintended branding changes.
