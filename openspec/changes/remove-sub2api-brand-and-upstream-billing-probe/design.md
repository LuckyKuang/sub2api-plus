## Context

Billing interop is a closed loop: one Sub2API instance publishes `/v1/sub2api/billing`, another probes it, persists a snapshot in `accounts.extra`, and may overwrite `accounts.rate_multiplier`. Group profit control separately consumes ordinary account and downstream multipliers to calculate local scheduling admission. It does not generate an external UA, request header, request body, or response body.

The product requirement removes billing interop but preserves local profit control. A separate protocol-boundary cleanup removes unnecessary system-generated identifiers visible to third parties. That cleanup is neither a product rebrand nor authorization to remove unrelated local business behavior.

## Decisions

### 1. Billing interop is deleted without a compatibility window

`GET /v1/sub2api/billing` and every upstream-probe admin route become ordinary framework 404s. Runtime code no longer interprets `sub2api.key_billing`, the three probe extra keys, or `upstream_billing_probe_settings`. Old backup imports discard the retired extra keys at the import boundary.

### 2. Manual multipliers survive

`accounts.rate_multiplier`, group rates, user rates, model multipliers, and channel pricing multipliers remain ordinary operator-controlled billing inputs. The migration MUST NOT reset or rescale them. OAuth scheduling-rate behavior unrelated to the retired probe remains unchanged.

### 3. Group profit control remains local and supported

Group profit control, its create/edit controls, schema fields, cache projections, scheduling gates, and profit-preview tooling remain. After probe removal, the feature evaluates the manually managed `accounts.rate_multiplier`; it does not read or display a probe snapshot. Existing migrations 198 and 199 remain unchanged, and no migration drops the profit-control columns.

### 4. Probe cache invalidation uses current repository mechanisms

Probe-data cleanup enqueues the existing scheduler outbox events for affected accounts. Existing group auth-cache invalidation and profit-control snapshot behavior remain unchanged.

### 5. Wire code is regenerated from sources

Wire provider graphs are edited at their sources. `go generate ./cmd/server` regenerates output; generated files are not manually patched.

### 6. External protocol cleanup is structured and narrow

System-generated project identifiers are removed from third-party request URL/query/header/default-body construction. Examples include non-provider UA strings, `referrer=sub2api`, synthetic connectivity text, injected prompt markers, generated validation tokens, batch display names, and object-storage healthcheck keys.

There is no global body replacement. User prompts, tools, model output, credentials, arbitrary user fields, and payment/product subjects remain unchanged. Official provider CLI/OAuth identity profiles remain where required.

Header override validation reserves explicit project-specific header names such as `X-Sub2API-*`; it does not reject an otherwise legitimate custom header value merely because the value contains `Sub2API Plus`.

### 7. Public private metadata is removed without rebranding

Locally generated `X-Sub2API-*` public response headers are removed, and response-header allowlist extensions cannot add them back. Existing standard `Retry-After` and `X-RateLimit-*` behavior remains. Structured Google error metadata uses a neutral technical domain such as `gateway.security`.

The following compatibility and identity values are explicitly protected:

- WebAuthn `rp_display_name = Sub2API` and TOTP issuer `Sub2API`;
- the default `site_name` and HTML title from the `main` baseline;
- mail, payment, compliance, and other user-visible product copy;
- admin WebSocket subprotocol `sub2api-admin`;
- backup types `sub2api-data` and `sub2api-bundle`, and existing export filenames;
- `SUB2API_API_KEY`, `model_provider = "sub2api"`, browser storage keys, plugin protocols, and internal namespaces;
- Codex identity precedence and the coherent User-Agent/Originator/Version contract.

The implementation diff MUST NOT add a generic gateway label as a replacement brand.

### 8. Grok request cache behavior uses a neutral local control header

The request-level Grok client-tool cache behavior remains available under the
explicitly approved `X-Grok-Client-Tool-Cache` name. This is an inbound gateway
control for custom clients and integrations, not an official Grok/xAI header.
The gateway consumes it locally and removes it at every HTTP, plugin, and
WebSocket outbound boundary. The unshipped branded and generic-gateway names
are not recognized, so no compatibility shim retains a project protocol
fingerprint.

### 9. Historical data is retained

Historical audit logs, usage logs, ops records, and existing on-disk backups are not rewritten or deleted.

## Rejected Alternatives

- Repair the previous broad rebrand branch: its diff mixes protocol cleanup with product and compatibility changes, making omissions more likely.
- Delete group profit control because it compares upstream and downstream multipliers: the comparison is local business logic and does not expose a project protocol identifier.
- Rename the billing endpoint: preserves a capability the requirement removes.
- Reset account rates to 1: destroys operator-controlled billing data.
- Replace every case-insensitive `sub2api` occurrence: corrupts product identity, compatibility contracts, internal namespaces, and possibly user content.
- Rename the product to a generic gateway label: outside scope and explicitly prohibited.
