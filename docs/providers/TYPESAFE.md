# TypeSafe / Jev (System One)

TypeSafe accounts serve Jev through TypeSafe's native, non-streaming System One
JSON endpoint. This is not an OpenAI-, Anthropic- or Gemini-compatible protocol:
the gateway forwards the validated native payload unchanged and relays the
validated native JSON response.

## Endpoint and accounts

| Item | Value |
| --- | --- |
| Gateway endpoint | `POST /v1/systemone` |
| Upstream endpoint | `POST <base_url>/v1/systemone` |
| Default base URL | `https://api.typesafe.ai` |
| Model | `jev-latest` (the only accepted value) |
| Account platform / type | `typesafe` / `apikey` only |
| Credentials | `api_key` (required), optional `base_url` |
| Authentication | `Authorization: Bearer <api_key>` |

A `base_url` ending in `/v1` is normalized to its origin. Invalid or relative
values fall back to the documented default. Requests use the account's
configured proxy like every other platform.

When `security.url_allowlist.enabled: true`, the built-in `upstream_hosts` list
contains `api.typesafe.ai`. An operator override of `upstream_hosts` replaces
the built-in list, so a frozen allowlist must add that host explicitly or the
platform becomes unreachable.

## Request shape

```json
{
  "model": "jev-latest",
  "state": "text to evaluate",
  "questions": {
    "tone": {
      "type": "noul",
      "instructions": "Is the text hostile?",
      "criteria": { "true": "Hostile", "false": "Neutral" }
    }
  }
}
```

`state` is a string, object or array. `questions` is a non-empty object keyed by
question id. Each question accepts:

| Question type | `criteria` |
| --- | --- |
| `noul` | optional object with `true` / `false` descriptions |
| `choice` | object of choice value → description (may be empty) |
| `score` | non-empty array of score levels |

`instructions` is optional and nullable for every type; descriptions may be a
string, object or array. Unknown sibling fields inside the payload are preserved
and forwarded as-is.

## Basic validation

Basic validation runs before provider selection and returns a client error:

- Invalid JSON, a missing/empty/duplicate `model`/`state`/`questions`, or a model
  other than `jev-latest`.
- Duplicate object keys, and non-canonical spellings of `model`, `state`,
  `questions`, `stream`, `type`, `instructions` or `criteria` (any case variant).
- A `questions` value that is not a non-empty object, or a question object whose
  type is not `noul`, `choice` or `score`, or whose criteria do not match the
  type above.
- `stream: true`. System One is non-streaming; `stream: false`, `null` or an
  absent field is accepted and the field is forwarded unchanged.

Request size is bounded by the gateway body limit; responses are buffered up to
4 MiB (`typesafe.MaxSystemOneResponseBytes`) and must be valid JSON. A response
content type that is not JSON (or `+json`) is reported to the client as
`application/json`, so the gateway origin can never serve an upstream HTML body.

An unextractable or unknown-but-valid payload is a pass-through case, not an
audit or validation failure. Accepted requests enter the shared security audit
after authentication and basic validation and before channel mapping, account
selection, billing, concurrency or any upstream write.

## Endpoint isolation

A `typesafe` group only speaks System One:

- `/v1/messages`, `/v1/messages/count_tokens`, `/v1/chat/completions` and
  `/v1/responses` return `404 not_found_error` before scheduling for a native
  TypeSafe group instead of forwarding an incompatible protocol to the provider.
- A forced platform (for example the Antigravity or OpenAI passthrough entries)
  keeps its own protocol handling.
- Composite groups must explicitly allow the `typesafe` target for the requested
  model; `jev-latest` and `jev-*` route to the TypeSafe platform. Composite
  groups do not list `jev-latest` among their static candidate models.

## Channel mapping, scheduling and usage records

`ResolveChannelMappingAndRestrict` returns `(mapping, false)`: the second value is
a boolean, not an error, and the model-restriction decision lives in the
scheduling stage (`checkChannelPricingRestriction`). A configured channel model
mapping is recorded in the usage log (requested model, channel-mapped model,
mapping chain and billing model source) but never rewrites the native payload:
the upstream protocol only supports `jev-latest`, so the validated body is
forwarded verbatim.

Usage accounting computes `ChannelUsageFields` synchronously before the
asynchronous usage task is enqueued. The deferred worker reads only immutable
captured values, never the reusable Gin context. A stopped worker pool or a full
queue falls back to the mandatory synchronous billing record, and the inflight
balance reservation follows the existing handler/task reference lifecycle.

## Pricing

The built-in billing card for `jev-latest` is:

| Field | Value |
| --- | --- |
| Input | `$0.042` per million tokens (`0.042e-6` per token) |
| Output | `$0` per million tokens (explicit free) |

The channel reference-price service supports the `typesafe` platform with the
exact model `jev-latest`. TypeSafe has no Release-catalog row and no catalog
provider label, so both single-model lookup and sync resolve the same
same-model builtin card. Unknown models (`jev-*` variants or another provider's
model names) stay `manual_required` and never borrow another provider's family
price. A saved operator price, including an explicit `0`, is preserved by later
syncs.

## Errors and failover

| Upstream result | Behavior |
| --- | --- |
| `400`, `413`, `422` | Client error; account state is never touched |
| `401` | Account-auth failover (`typesafe_api_key_rejected`) |
| `403`, `402`, other account-level statuses | Existing account error policy, then failover |
| `429`, `5xx`, `529` | Existing rate-limit/error policy and failover |
| Other non-2xx statuses | `upstream_error` returned to the client |
| Transport error or timeout | Failover |
| 2xx body too large or not valid JSON | `upstream_error`; ops event kept for reconciliation |

Success responses are relayed with the upstream JSON media type and the
`x-request-id` header when present.

## Outbound identity

TypeSafe is an API-key compatible supplier with no provider-defined client
family, version or identity header. Inference, account connection tests, proxy
use, retries and failover all go through the shared owner-snapshot preparation
boundary used by every provider. The selected identity follows the existing
account/global/default source chain and the configurable preset mapping for
API-key compatible accounts; no TypeSafe CLI name, version or header is
invented. A same-owner retry keeps its snapshot while failover resolves the new
credential owner.

## Operations

Gateway scheduling, account health, quota and error handling otherwise follow
the standard account behavior. Keep credentials in the administrative account
store rather than configuration files or repository content. The canonical
configuration example is [`deploy/config.example.yaml`](../../deploy/config.example.yaml).
