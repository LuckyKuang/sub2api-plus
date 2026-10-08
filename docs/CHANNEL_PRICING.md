# Channel Pricing Reference Prices

This document describes how the admin channel editor obtains official reference
prices, how they are matched, and what to do when a price is missing. It is the
durable specification for the admin API contract; code changes that alter this
behavior must update this file in the same commit.

## 1. Design principles

1. Channel prices are operator selling prices. A reference price is only a
   creation-time default; it is never applied in the background to a saved rule.
2. "Can list a model" and "Can give a safe reference price" are answered by the
   same service. A model that can be listed but has no safe price stays visible
   as `manual_required`; it never disappears and never becomes a zero price.
3. Missing, explicit zero, and positive price are three different states.
   The API encodes a missing field as `null` and an explicit free field as `0`,
   and the frontend preserves both.
4. Reference matching is stricter than request billing fallback. The billing hot
   path may use broad family fallbacks so a request is never interrupted; the
   admin reference API must not, because a wrong reference price gets written
   into operator configuration.
5. token, image-token, per-image, per-request, per-video, per-second and audio
   units are not interchangeable and are never converted into one another.
6. A remote refresh failure keeps the last usable snapshot. No failure clears or
   rewrites a saved channel rule.

For request billing, an unset channel image price inherits the catalog price.
Set an explicit `0` to keep image output free. An explicit zero image-output
price never falls back to text pricing.

## 2. Source priority

For one model, the reference service resolves in this order:

| # | Source | `source` | Notes |
| --- | --- | --- | --- |
| 1 | Exact entry in the validated Release catalog | `release_catalog` | The entry's provider label must belong to the requested platform. |
| 2 | Deterministic same-SKU alias | `release_catalog` | Registered spellings and documented thinking-tier mappings only. |
| 3 | Same-model built-in fallback | `builtin_fallback` | Must belong to the platform's model family. |
| 4 | Explicitly declared proxy reference | `proxy_reference` | A proxy price, not a vendor-published same-model price. |
| 5 | Nothing found | `none` | `manual_required` or `unsupported_unit`. |

The following are **never** used for an admin reference price:

- any `claude*` name mapped by an `opus` / `sonnet` / `haiku` substring;
- an unknown `gpt-*` name mapped to the default test model;
- unregistered `kimi-` / `glm-` / `minimax-` / `grok-` family substrings;
- a similar but different version, e.g. `kimi-k2.7-code` priced as `kimi-k2`;
  `claude-opus-5-5-preview` priced as `claude-opus-5-5`.

GPT-6.1 Sol, GPT-6 Sol, GPT-6 Luna, and GPT-6 Astra are separate SKUs. Registered
provider-qualified spellings and effort suffixes may use the same SKU's card;
bare `gpt-6` and preview names never inherit Astra pricing. GPT-6.1 Sol's
built-in Standard input/output/cache-write/cache-read reference rates per
million tokens are $2/$10/$2.50/$0.10; GPT-6 Sol's are $2/$10/$2.50/$0.20 and
GPT-6 Luna's are $0.10/$0.50/$0.125/$0.01. Saved selling prices remain explicit
overrides, including zero cache-write values across Standard, Fast, and Flex.
Astra Ultrafast uses six times the selected Standard token prices independently
of the operator's Fast multiplier; it does not grant upstream capability.

## 3. Status and reason codes

`claude-sonnet-5` has a separate exact catalog entry and same-model built-in
fallback. Its Standard input/output/cache-write-5m/cache-write-1h/cache-read
prices are $2/$10/$2.50/$4/$0.20 per million tokens, from
[Anthropic's official pricing](https://platform.claude.com/docs/en/about-claude/pricing).
Single-model add and platform synchronization resolve the same complete card,
even before a remote catalog includes the SKU. Provider-qualified spellings and
the registered `-thinking` variant use this card. Sonnet 5.5 remains a separate
SKU, and `claude-connect-5`, unknown suffixes, and preview names receive no Sonnet
5 reference price. Saved channel prices remain operator overrides.

| `status` | Meaning |
| --- | --- |
| `priced` | A complete form-compatible price card is present. |
| `manual_required` | No safe source. The operator must enter prices. |
| `unsupported_unit` | The model is billed per audio/character/second, which the channel pricing form cannot express. |

| `reason_code` | Meaning |
| --- | --- |
| `exact_price_unavailable` | No exact catalog entry, alias, or same-model fallback. |
| `provider_price_unpublished` | The provider exists in the catalog but does not publish a price. |
| `unsupported_billing_dimension` | Only an unrepresentable billing unit exists. |
| `platform_model_unsupported` | The model is not supported by the requested platform. |
| `catalog_unavailable` | No usable snapshot. |

Reason codes are stable keys. Operators and the UI must match on the key, never
on free text.

## 4. Platform model lists

Each platform list is the union of the platform supported-model source and the
matching Release catalog provider rows. Scanning only catalog provider rows left
OpenCode Go, Kimi, Zhipu, MiniMax and parts of DeepSeek empty, because those
models only exist in the built-in fallback table. TypeSafe has no catalog row at
all, so it lists only its registered built-in model.

| Platform | Catalog provider labels | Supported-model source |
| --- | --- | --- |
| `openai` | `openai` | `openai.DefaultModelIDs()` |
| `anthropic` | `anthropic` | `claude.DefaultModels` |
| `gemini` | `gemini`, `vertex_ai-language-models`, `vertex_ai-embedding-models` | `gemini.DefaultModels()` |
| `antigravity` | `anthropic`, `gemini`, `vertex_ai-language-models` | `antigravity.DefaultModels()` |
| `grok` | `xai` | `xai.DefaultModelIDs()` |
| `kimi` | `moonshot` | built-in `kimi-*` / `k3*` fallback ids |
| `zhipu` | `zhipu` | built-in `glm-*` fallback ids |
| `deepseek` | `deepseek` | built-in `deepseek-*` fallback ids |
| `minimax` | `minimax` | built-in `minimax-*` fallback ids |
| `opencode_go` | `openai`, `anthropic`, `gemini`, `vertex_ai-language-models`, `xai`, `moonshot`, `zhipu`, `deepseek`, `minimax`, `opencode-go` | `DefaultOpenCodeGoModelIDs()` |
| `typesafe` | none (no Jev row exists) | built-in `jev-*` fallback ids (`jev-latest`) |

Labels must match the values actually used by the bundled
`model_prices_and_context_window.json`: Gemini SKUs live under `gemini` and the
Vertex labels `vertex_ai-language-models` / `vertex_ai-embedding-models`. There
is no bare `vertex_ai` row in the catalog, so that label would match nothing.
Embedding rows are admitted only for `gemini`: that platform serves embedding
SKUs and the form can express their token prices (an explicit `0` output stays
`0`). `antigravity` does not proxy an embedding endpoint, so its list omits the
embedding label.

Provider labels only decide whether a catalog row *belongs* to the platform.
They never decide which models a platform supports, and never assign a price
across platforms.

Two exceptions to the plain union:

- **Aggregator platforms.** OpenCode Go forwards each model to that model's
  upstream endpoint, so the upstream labels may *price* its listed models, but
  they must not *expand* its list: only rows under the gateway's own
  `opencode-go` label join the list (none exist today). Without this rule the
  whole upstream catalog — including models the gateway does not serve — would
  show up under `opencode_go`.
- **Registered same-SKU aliases.** A `-thinking` suffix (Claude 4.5 family,
  `gemini-2.5-flash-thinking`) and the registered xAI spellings
  (`grok-4.20-0309-reasoning` → `grok-4.20`, `grok-composer-2.5-fast` →
  `grok-build-0.1`) resolve to their base SKU: the suffix changes reasoning
  behavior, not the published rate. Unregistered suffixes such as `-preview`
  are never guessed; the response reports the matched SKU in `matched_model`.
- **Exact-model-only platforms.** TypeSafe registers no catalog provider label
  and has no published Release row, so every catalog lookup misses by
  construction. `jev-latest` resolves through the verified same-model built-in
  card (`$0.042`/M input, explicit `$0` output); unknown `jev-*` models and other
  providers' model names stay `manual_required` instead of borrowing a family
  price. Single-model lookup and sync share this one resolution path, and an
  operator-saved price — including an explicit `0` — is never overwritten by a
  later sync.

## 5. Admin API

### 5.1 Single-model lookup

```http
GET /api/v1/admin/channels/model-pricing?platform=openai&model=gpt-6-sol
```

`platform` and `model` are both required. A missing parameter or an unsupported
platform returns `400`. A missing price is **not** an error:

```json
{
  "data": {
    "model": "gpt-6-sol",
    "matched_model": "gpt-6-sol",
    "platform": "openai",
    "status": "priced",
    "source": "release_catalog",
    "reason_code": "",
    "pricing": {
      "platform": "openai",
      "models": ["gpt-6-sol"],
      "billing_mode": "token",
      "input_price": 0.000002,
      "output_price": 0.00001,
      "cache_write_price": 0.0000025,
      "cache_write_1h_price": null,
      "cache_read_price": 0.0000002,
      "fast_multiplier": 2,
      "flex_multiplier": 0.5,
      "reasoning_effort_multipliers": null,
      "image_input_price": null,
      "image_output_price": null,
      "per_request_price": null,
      "intervals": [
        {
          "min_tokens": 272000,
          "tier_label": ">272000",
          "input_price": 0.000004,
          "output_price": 0.000015,
          "cache_write_price": 0.000005,
          "cache_read_price": 0.0000004
        }
      ]
    }
  }
}
```

All prices are USD per token, matching the backend storage unit. The frontend
converts them to USD per million tokens for display and back on submit.

### 5.2 Refresh and sync

```http
POST /api/v1/admin/channels/pricing/sync-models
Content-Type: application/json

{"platform":"openai"}
```

```json
{
  "data": {
    "platform": "openai",
    "refresh_status": "refreshed",
    "catalog_version": "a1b2c3d4",
    "last_updated": "2026-09-28T12:00:00Z",
    "warning_code": "",
    "models": [ { "...": "ModelPricingReference" } ]
  }
}
```

This endpoint is a `POST`: it fetches a Release manifest, validates it, and
atomically installs the catalog. It is not idempotent and it must report what
happened, so it cannot be a `GET`.

| `refresh_status` | Meaning |
| --- | --- |
| `refreshed` | A newer trusted release was downloaded and installed. |
| `current` | The catalog was already current; the current snapshot is returned. |
| `stale` | Refresh failed, the last usable snapshot is returned, plus a warning. |
| `disabled` | Remote refresh is not configured; local catalog and fallbacks are used. |

Refresh only uses the existing manifest / digest / version / allowed-host /
proxy / validated-cache chain. It never bypasses them.

Error responses:

| Condition | Status | Code |
| --- | --- | --- |
| `platform` missing | 400 | `MISSING_PARAMETER` |
| `platform` unsupported | 400 | `UNSUPPORTED_PLATFORM` |
| Refresh failed and no platform snapshot can be built | 503 | `PRICING_CATALOG_UNAVAILABLE` |
| Catalog available but some models lack a price | 200 | per-model `manual_required` |

## 5.3 Billing units, media cards and interval bounds

Channel pricing intervals are **left-open, right-closed** `(min_tokens,
max_tokens]`: a request matches when `total_tokens > min_tokens`. The reference
card therefore encodes the documented threshold semantics exactly:

| Upstream semantics | `min_tokens` | `tier_label` | Effect |
| --- | --- | --- | --- |
| Strictly above the threshold (OpenAI, Anthropic: `>272000`) | `272000` | `>272000` | 272000 stays on the base tier; 272001 enters the long-context tier. |
| At or above the threshold (xAI: `>=200000`) | `199999` | `>=200000` | 200000 enters the tier. |

Using `threshold + 1` for the strict case would leave 272001 on the base tier
and silently under-bill.

Billing dimensions are never converted into each other:

| Source capability | `billing_mode` | Card shape |
| --- | --- | --- |
| Text / embedding token price | `token` | token + cache fields; an explicit `0` output (free embedding) stays `0`. |
| Per-image price | `image` | `per_request_price` at the base resolution (1K); higher resolutions become intervals with `tier_label` `2K` / `4K`. |
| Per-video price | `video` | `per_request_price` at the base resolution (480p); `720p` / `1080p` tiers become intervals. |
| Audio / character / second units only (`mode` `audio_*` or `realtime`) | — | `unsupported_unit` + `unsupported_billing_dimension`, no price card. |

Two consequences worth calling out:

- **Audio models are `unsupported_unit` even when the catalog row also carries
  text token prices.** `gpt-4o-mini-tts`, `gpt-4o-transcribe`,
  `gemini-2.5-flash-preview-tts` and the Gemini live native-audio preview are
  billed per audio token or per second; the channel form cannot express that
  dimension, and a token card would mis-price them. They stay listed and
  explicitly unsupported instead of silently priced.
- **Grok Imagine media models are priced from the project-maintained xAI tier
  table** (the same prices the billing path uses): `grok-imagine-image` is
  `image` at $0.02 (1K) with 2K/4K at $0.02, `grok-imagine-image-quality` at
  $0.05/$0.07, and `grok-imagine-video-1.5` is `video` at $0.08 (480p) with
  720p $0.14 and 1080p $0.25. No media model ever falls into the unknown-text
  family fallback and receives a token price.

## 6. Editor behavior

### 6.1 Synchronization

1. POST the sync endpoint and show an in-progress state.
2. Remove models already covered by this platform's rules, using the same
   case-insensitive exact match and wildcard rules as the conflict validator.
   A `claude-*` rule therefore blocks a new `claude-sonnet-4-5` rule.
3. Create **one rule per model**. Models are never merged into a shared rule,
   even when two models happen to have the same price.
4. Apply the complete price card for `priced`. For `manual_required` /
   `unsupported_unit`, create an independent incomplete rule and show the reason.
5. Do not modify existing rules, including their empty fields, explicit zeros,
   intervals, multipliers, and time pricing.
6. Report added, auto-filled, and needs-manual-pricing counts separately. Show
   the refresh warning separately from per-model price status.
7. Repeated synchronization creates no additional rules.

### 6.2 Manual addition and paste

- An untouched rule with one model resolves that model's reference price.
- Multiple pasted models are resolved independently and split into separate
  rules. The first model's price is never applied to the others.
- A rule with any user-edited field, interval, or multiplier is not overwritten
  by an asynchronous response. An explicit "complete empty fields" action fills
  only the fields that are still empty.
- Numeric zero counts as an edited value. Neither automatic fill nor the
  explicit completion action replaces it.
- Deleting a model writes the change back (the removal is never silently
  dropped). Removing a non-first model keeps the rule's prices; removing the
  only/first model empties the rule.
- Switching the primary (first) model — a rule that already had a model and is
  now replaced by a different one — clears the previous model's auto-filled
  token price, context intervals, and Fast/Flex/reasoning multipliers, then
  re-resolves the new primary model's reference price. Otherwise a stale price
  or a leftover Fast multiplier from the old model would survive and be read as
  "already filled", blocking the re-lookup (e.g. gpt-6-sol → gpt-6-luna).
- Adding the first model to an empty rule is not a switch: effort and tier
  multipliers configured there are user input and are preserved, and auto-fill
  does not overwrite them.
- Every lookup is bound to the rule identity, a request sequence number, and the
  model snapshot. A response is discarded when the model or rule was removed,
  when a newer lookup was started, or when the target field was edited.
- Network failures, `manual_required`, and `unsupported_unit` each show a
  distinct, retryable message. None of them are swallowed.

Create and edit flows use the same components and the same logic.

## 7. Built-in same-model fallbacks for new models

Remote mirrors can lag a model release. These models therefore have built-in
same-model fallbacks so a missing or stale catalog cannot silently bill them as
a different model. Values are USD per token.

| Model | input | output | cache write | cache read |
| --- | ---: | ---: | ---: | ---: |
| `gpt-6-sol` | `2e-6` | `10e-6` | `2.5e-6` | `0.2e-6` |
| `gpt-6-luna` | `0.1e-6` | `0.5e-6` | `0.125e-6` | `0.01e-6` |
| `claude-opus-5-5` | `4e-6` | `20e-6` | `5e-6` (5m) / `8e-6` (1h) | `0.2e-6` |
| `claude-sonnet-5-5` | `2e-6` | `10e-6` | `2.5e-6` (5m) / `4e-6` (1h) | `0.2e-6` |

- Without `gpt-6-sol` / `gpt-6-luna` fallbacks, an absent catalog entry fell
  through to the OpenAI default test model and billed Sol as `gpt-5.1-codex`.
- Without a `claude-opus-5-5` fallback, the `opus-5` substring matched and
  billed Opus 5.5 as Opus 5 (`$5/$25`), a 1.25x over-charge, and lost the
  5m/1h cache split.
- `claude-opus-5-5` and `claude-opus-5` are isolated: neither falls back to the
  other, and a similar SKU such as `claude-opus-5-5-preview` cannot answer for
  `claude-opus-5-5`.

GPT-6 Sol/Luna long context: total input strictly greater than 272000 tokens
uses an input/cache multiplier of 2 and an output multiplier of 1.5. At exactly
272000 tokens the base tier applies. Flex uses the generic 0.5 service-tier
multiplier; Fast/priority is 2x. These semantics are identical in reference
pricing and actual billing.

Sonnet 5.5 uses the same fallback rates throughout its 1,000,000-token context
window; it does not inherit the GPT-6 long-context threshold. Opus 5.5 retains
its Fast/priority 2x pricing. Explicit channel prices, context intervals and
tier multipliers retain their existing precedence over model defaults.

Explicit `cache_creation_input_token_cost: 0` stays zero. Derived cache-write
rules only apply when the catalog field is absent, and the derived value is
never presented as an operator override.

## Inflight balance reservations

When `billing.inflight_reservation.enabled` is enabled, admission estimates the
same billing-model candidates and channel/group selling prices used by billing.
Reservations run only after authentication, basic validation, security audit,
and billing eligibility. Simple mode and subscription billing retain their
existing behavior. Unknown pricing normally admits without a reservation;
`fail_closed_on_unpriced` controls whether an unpriced estimate rejects instead.
An explicit zero selling price is a successful pricing result and requires no
reservation, even when rejection of unpriced requests is enabled. Explicit free
token/image/video/audio cards do not fall back to paid model or account defaults.

The handler owns one reference and each accepted billing task acquires its own
reference. A dropped task returns its reference immediately. Handler completion
stops renewal; release waits for all billing references, with the remaining TTL
bounding stuck tasks. Successful billing synchronously updates the balance cache
before returning the task's reference. Plus invalidates exhausted/below-reserve
balances first, preserving the minimum-balance admission policy. A cache error
falls back to the existing queued deduction, so cache failures remain fail-open
and may briefly permit admission using stale balance after a reservation expires
or releases. Reservations are an admission estimate, not a replacement for
final usage billing or an absolute overdraft guarantee.

## Accepted usage after API-key deletion

Deleting an API key stops future authenticated requests; it does not cancel
settlement for usage already accepted. If the key is missing or soft-deleted at
settlement, skip only its own quota and rate-window counters. User balance or
subscription charges, account quota, usage persistence and request deduplication
retain their existing transaction semantics. Retrying the same settlement must
not charge twice. Other database errors still fail and roll back the transaction.

## 8. Verifying

StepFun uses exact official international USD cards for five chat models; see
[StepFun default prices](providers/STEPFUN.md#default-prices). The Step Plan
router remains visible with `manual_required`, since its underlying engine and
Credit charge vary per request. Unknown StepFun suffix/date variants never
inherit a family price. Saved channel prices remain authoritative.

Run inside the platform validation container (Apple Containers on macOS, Docker
inside WSL2 Debian/Ubuntu on Windows, Docker on Linux); host-side validation is
forbidden.

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestNewModelPricing|TestChannelPricingReference|TestPlatformCoverage'
go test -tags=unit ./internal/handler/admin -run 'TestSyncPricingModels|TestGetModelDefaultPricing'

cd frontend
pnpm exec vitest run src/components/admin/channel/__tests__ src/views/admin/__tests__/ChannelsView.pricing-sync.spec.ts
pnpm run test:run
pnpm run lint:check
pnpm run typecheck
pnpm run check:i18n
```

`TestPlatformCoverage_*` is the platform matrix: every platform must be
"listable with a safe price" or "listable with an explicit manual/unsupported
status", single-model lookup and platform sync must agree, and the boundary,
unit and alias rules above are pinned there.

Structured logs for this feature carry `component`, `stage`
(`resolve` / `list` / `refresh`), `platform`, `request_id`, the stable
`error_code` / `warning_code`, and the catalog version and model count. They
never log caller fields beyond the requested model name, credentials, or
request bodies.
