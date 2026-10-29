# Channel Pricing Reference Prices

This document describes how the admin channel editor obtains official reference
prices, how they are matched, and what to do when a price is missing. It is the
durable specification for the admin API contract; code changes that alter this
behavior must update this file in the same commit.

Channel updates preserve `features_config.web_search_emulation` dimensions that
the client omits. Supplied platform values replace that dimension, including
explicit `false`; an explicit empty object or `null` clears the whole feature.
Other feature keys retain whole-block replacement semantics. The editor writes
explicit values for its supported platform sections and preserves dimensions
it cannot edit, including while global emulation is disabled.

## 1. Design principles

1. Channel prices are operator selling prices. A reference price is only a
   creation-time default; it is never applied in the background to a saved rule.
2. "Can list a model" and "Can give a safe reference price" are answered by the
   same service. A model that can be listed but has no safe price stays visible
   as `manual_required`; it never disappears and never becomes a zero price.
3. Missing, explicit zero, and positive price are three different states.
   The API encodes a missing field as `null` and an explicit free field as `0`,
   and the frontend preserves both.
4. Request billing uses configured model prices first, then an identified
   same-model built-in price, then the selected platform's explicit fallback
   model. A similar family name never authorizes an implicit price substitution.
   Reference prices remain same-model suggestions and do not turn a platform
   fallback into that model's official reference price.
5. token, image-token, per-image, per-request, per-video, per-second and audio
   units are not interchangeable and are never converted into one another.
6. A remote refresh failure keeps the last usable snapshot. No failure clears or
   rewrites a saved channel rule.

For request billing, an unset channel image price inherits the catalog price.
Set an explicit `0` to keep image output free. An explicit zero image-output
price never falls back to text pricing.

### Platform pricing fallback settings

The administrator settings API (`GET`/`PUT /api/v1/admin/settings`) exposes
`pricing_fallback_models`, a JSON object mapping each concrete platform ID to a
literal price-model ID. The object is persisted in the existing settings store;
it requires no deployment environment variable or database schema change.
The effective order is configured group/channel/global model pricing, an
identified same-model built-in card, then the selected concrete platform's
configured fallback price model. Missing channel pricing uses this chain rather
than requiring the administrator to duplicate system prices in a channel card.
Explicit zero prices remain configured values and never advance to a later
source. Units, context tiers and service tiers retain their existing contracts.

The channel's `restrict_models` switch checks whether effective pricing is
available. It accepts a matching channel card, an explicit group price, a usable
same-model system card, or the selected platform's usable final fallback; it no longer treats the list
of custom channel cards as a model authorization list. With no valid price
evidence, the pricing check rejects the request explicitly. Group and account
model authorization remain the independent controls for which models may run.

Fallback selects a price card only. It does not rewrite the request model,
advertise new capabilities, change routing or failover ownership, or bypass
group model authorization, account model mappings, endpoint capabilities or
quota checks. This setting is independent from the older
`enable_model_fallback` / `fallback_model_*` settings, which may replace an
Antigravity upstream model after a model-not-found response.

All concrete platforms in the shared registry have an independent entry:

| Platform | Default fallback price model |
| --- | --- |
| `anthropic` | `claude-sonnet-5` |
| `openai` | `gpt-5.4` |
| `gemini` | `gemini-3.8-flash` |
| `antigravity` | `gemini-3.8-flash` |
| `grok` | `grok-4.6` |
| `kimi` | `kimi-k3` |
| `zhipu` | `glm-5.3` |
| `deepseek` | `deepseek-v4-flash` |
| `minimax` | `minimax-m3` |
| `stepfun` | `step-5-preview` |
| `opencode_go` | `glm-5.3` |
| `typesafe` | `jev-latest` |
| `command_code` | `deepseek-v4-flash` |
| `cline` | `deepseek-v4-flash` |

These defaults name existing price cards and introduce no new price numbers.
`composite` is not a concrete supplier: its request uses the resolved target
platform's entry. Aggregators such as OpenCode, Command Code and Cline retain
their own entries and may explicitly name an existing global SKU from any
vendor they serve; a model's vendor prefix does not change the selected platform.
The lookup never borrows a different platform's fallback configuration.

For updates, omitting the entire field or sending `null` preserves the stored
object. A supplied object replaces the previous object; omitted platform keys
receive their server defaults, and `{}` restores all defaults. An explicit empty
string disables the final fallback for that platform:

```json
{
  "pricing_fallback_models": {
    "openai": "",
    "cline": "openai/gpt-6.1-terra"
  }
}
```

The API returns all effective platform entries. Unknown platform keys,
`composite`, non-string values (including a nested `null`), control characters,
embedded whitespace, wildcard model names and names longer than 255 bytes are
rejected before any settings write. Surrounding ordinary spaces are trimmed.
Saving validates the literal format; the price lookup must still find a valid
card for the configured target at admission. A disabled or unavailable fallback
does not become a zero price or trigger another fallback chain.

The final fallback supplies text-token prices. Its system card must explicitly
contain both input and output token prices, including an explicit `0` for a
free dimension. Input-only embedding cards, per-image prices and image-token
prices cannot supply a missing text-output price. An incomplete catalog card
may inherit missing text dimensions from the complete compiled card for that
same fallback SKU, preserving its configured dimensions and explicit zeros;
it never selects a different SKU. Existing image, video and audio meters keep their own unit
prices and are never billed with a text-token fallback. The fallback model's
group or channel selling prices are not consulted.

New requests and WebSocket turns freeze the fallback configuration with the
catalog and channel-price generation. Retries and accepted deferred jobs retain
that evidence across administrator edits. Ungrouped requests also freeze their
catalog, fallback configuration, default rate and pricing time without creating
a group membership; new requests capture later edits. A missing settings key uses the
declared defaults; a settings dependency error or invalid stored JSON is an
error and must not silently select defaults.
Deferred snapshots persist the selected fallback model, platform and complete
price card. Settlement after restart uses that captured evidence, even if the
live settings or catalog are no longer available; absent evidence stays absent.
Durable ungrouped evidence has an explicit discriminator and default rate;
a missing group in older evidence remains invalid.

### Customer rate policies

Saved unit prices remain separate from the group's selling-rate policy.
`billing_rate_policy` applies to concrete supplier platforms from the shared
platform registry and public request model IDs. The platform factor and longest
matching model factor (exact before prefix) multiply independently; each missing
factor inherits 1 and an explicit zero is free. A unit-specific model rule wins an otherwise
equal all-unit rule. See [Composite groups](COMPOSITE_GROUPS.md#consumption-rate-policies-and-sale-catalogs)
for configuration, examples and bounds.

The customer base is the personal group override or group default. Independent
image/video bases retain precedence, even for a media card billed per request;
the policy still matches that card's actual `request` unit. For new admissions,
the applicable subscription peak multiplies every actual meter, including
independent image/video bases, search, duration audio and cache storage.
Price-card time periods, context tiers and service tiers keep their own scopes.
Channel `TimePricing` continues to apply only to token pricing; it is independent
of the group subscription peak factor and the two applicable factors multiply.
Supplier account multipliers remain separate for ordinary inference,
media and cache requests. The existing Gemini balance-only batch API preserves
its historical account-adjusted unit-price, discount and hold formula; its
submission estimate and stored snapshot govern that operation, rather than a
synchronous model offer. Subscription quota uses the final customer charge
rather than the price before these selling factors.

New HTTP requests and Responses WebSocket turns freeze their customer rates,
price-card generation and pricing time after ingress audit and before admission
effects. Grok Voice Realtime keeps its existing whole-session minute meter and
freezes rates at session admission; its frames remain independently audited.
Retries and detached settlement reuse this evidence. Deferred media and batch
jobs persist their accepted evidence rather than applying later administrator
edits at completion. A pricing dependency error rejects new admission; it must
not silently select a cheaper or free rate. This does not change the documented
same-model billing fallback for an otherwise valid price lookup.

An explicit public model price survives Composite routing and account-level
model mapping. A missing/empty card retains normal concrete-model fallbacks.
Explicit `upstream` or `response_model` billing sources keep their selected
models; `channel_mapped` keeps the actual channel mapping when one exists.
Codex server-model correction also preserves an accepted public card, including
explicit zero and per-request prices. The reported upstream model remains in
diagnostics. With no accepted public card, an empty card, an explicit billing
source that selects another model, or accepted version-1 price selection, the
existing identified server-model correction remains available.
Catalog, reservation and settlement share this price-model selection, while
platform/model selling factors always use the public model and actual supplier.
New durable pricing evidence uses private snapshot version 2 for this price
selection. Restored version-1 tasks retain their original candidate order and
re-export as version 1; a restart cannot switch their accepted upstream card to
a newly preferred public card. Calculation and peak-scope compatibility remain
independent of this private format version.

Sale and authorized catalogs use the same backend rate selection. They publish
supplier, public model, billing unit, policy revision, timestamp and factor
breakdown, including separate platform/model factors; unresolved or request-dependent prices remain unknown.
New selling rates use base × platform × model × applicable subscription peak. A
model multiplier does not require a custom price card; omitted prices inherit
the next configured price source (channel, then catalog) instead of becoming zero.
An empty group card cannot hide a lower channel price or change its billing mode;
partially specified same-mode cards inherit omitted flat prices and service-tier
factors from that channel. An effective group price-card override selects the
group price source and does not apply channel `TimePricing`, even when some
prices inherit. A selling model multiplier without a price override keeps the
lower price source and its existing time-pricing behavior.
Explicit zero prices remain valid, including zero-valued request tiers. A missing
unit price or a pure multiplier is unavailable when neither a same-model system
card nor a usable platform fallback supplies that unit; it never becomes free.
For a partial token card without a same-model system price or usable platform
fallback, each actually used input, output, cache or image dimension must have
explicit price evidence or its documented text/image fallback. An omitted output price does not become zero
and does not inherit another model's account-mapped price. Explicit zero is valid.
Input-only embeddings may use an input-only card; their reservation estimates
no generated output. Once an explicit public card is selected, an incomplete
used dimension rejects pricing rather than replacing that whole card with a
mapped model's prices. Known same-name catalog/channel fields still inherit.
Catalogs retain known fields and leave unpriced fields null, reporting
`pricing_unavailable` when the general token quote is incomplete. Frozen card
pointer presence preserves this distinction through durable export and restore.
An output-only card still publishes its known output price, including zero;
the input tokens used only to select a display-probe context tier do not create
an input selling price or relax real usage validation. When a same-name catalog
has one common cache-write price, adding a separate one-hour override preserves
that inherited five-minute price. An explicit zero one-hour override stays free,
including in matching context intervals and after durable restoration; outside
the interval, the original common price still applies to both TTLs.
Settlement returns a pricing error before usage/ledger writes when every billing
candidate is unpriced, even if an additive search surcharge has a known price.
Fixed-amount reservation rejects non-finite/negative quotes; known zero is valid.
This preserves the configured unpriced-admission policy while preventing a
served request from being reported as a successful free settlement.
Once a native audio/image/video model card is selected, a missing applicable
tier cannot fall back to a group flat price, catalog default or a free charge.
Grok video group and channel cards accept the existing `per_request`, `image`
and `video` modes: the first two charge per returned video, and `video` charges
duration in seconds times the video count. Admission and settlement use the
same quantity and independent platform, model and applicable peak multipliers.
Durable tasks retain their
accepted calculation version across restarts, including the historical override
calculation for version-1 snapshots without a discriminator. New snapshots carry
`peak_scope: all_meters`; accepted snapshots without that field retain the
historical `token_only` peak scope, independently of their calculation version.
The catalog
does not create upstream capability, entitlement or account availability, and
selling factors alone do not prove subscription profitability.

The admin group editor's image, video and search previews show base references:
flat unit price times the group base, or the independent media base when enabled.
They have no selected public model or personal rate and do not promise final
charges. Actual model prices, platform/model factors, personal bases and
applicable subscription peak factors are resolved by backend billing and the
authorized model catalog. For example, a $2 base reference with platform 2,
model 3 and peak 1.5 charges $18 for that meter when the customer base is 1.

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

GPT-6.1 Terra, GPT-6.1 Sol, GPT-6 Sol, GPT-6 Luna, and GPT-6 Astra are separate SKUs. Registered
provider-qualified spellings and effort suffixes may use the same SKU's card;
bare `gpt-6` and preview names never inherit Astra pricing. GPT-6.1 Sol's
built-in Standard input/output/cache-write/cache-read reference rates per
million tokens are $2/$10/$2.50/$0.10; GPT-6 Sol's are $2/$10/$2.50/$0.20 and
GPT-6 Luna's are $0.10/$0.50/$0.125/$0.01. Saved selling prices remain explicit
overrides, including zero cache-write values across Standard, Fast, and Flex.
Astra Ultrafast uses six times the selected Standard token prices independently
of the operator's Fast multiplier; it does not grant upstream capability.

GPT-6.1 Terra has an exact release-catalog card and same-SKU built-in fallback,
reviewed against [OpenAI API pricing](https://developers.openai.com/api/docs/pricing)
and the [Terra API model page](https://developers.openai.com/api/docs/models/gpt-6.1-terra)
on 2026-10-28. Its Standard input/output/cache-read/30-minute cache-write rates
are $1/$5/$0.05/$1.25 per million tokens; Fast is 2x, and Flex/Batch are 0.5x.
When total input, including cache reads and writes, exceeds 272000 tokens, the
whole request's input/cache prices double and output prices increase 1.5x.
Exactly 272000 remains at the base price. Saved catalog/channel prices retain
precedence, including explicit zero cache-write prices; registered provider and
effort aliases share this card, while unknown Terra suffixes receive no guessed
price, including suffixes containing another provider/model family such as
`-codex` or `-claude`; an explicitly configured exact SKU price remains valid.
Their literal channel cards, explicitly declared billing-model alternatives and
the selected platform's configured final fallback remain valid; a different
model's card, tier policy or usage candidate is never inferred from those
suffixes.
Admission and durable settlement pin the selected card across reloads.

The official 12-hour cache-write reference rate is $2.50 per million tokens
before context and speed adjustments. The existing OpenAI usage contract
provides aggregate cache-write tokens and uses the 30-minute card; this update
does not add a 12-hour usage breakdown. The release pricing resource records
the API's 1050000 context window, 922000 maximum input and 128000 maximum output.
These API limits are separate from the reviewed Codex client descriptor and do
not change its pinned metadata, request behavior or identity.

## 3. Status and reason codes

OpenAI OAuth/setup-token requests whose final sent model is GPT-6.1 Sol do not
apply the model-card long-context surcharge. Explicit administrator intervals
and selling prices retain precedence. The actual credential owner determines
eligibility, including Spark-linked accounts; aliases and response-model billing
cannot change it. API-key GPT-6.1 Sol, Astra and other model policies keep their
existing rules. See [Responses billing](protocols/OPENAI_RESPONSES.md#gpt-61-sol-and-gpt-6-solluna).

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

These reference-fill rules apply to channel pricing and account cost-statistics
cards. Group model cards use the separate selling-policy editor described below.

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

### Group model selling-factor editing

The group's model-pricing section edits the same
`billing_rate_policy.model_multipliers` saved with the group. It does not add a
second selling-factor field to a channel price or an account cost card. A price
card containing several models exposes a separate all-unit factor for each
model. Mixed groups require a concrete supplier selection; the registered
platform catalog supplies these options, including newly registered suppliers.

An unset model factor is `1` when no advanced exact/prefix or unit rule matches.
Clearing a card's factor removes its exact all-unit rule; explicit `0` remains
free. It does not clear a matching prefix or unit-specific rule. Those rules,
and rules without a price card, remain visible in the rate-only/advanced editor.
Platform and model factors are independent: clearing `M` does not remove `P`.

Group cards do not automatically fetch reference prices or split pasted models.
They can configure factors while all unit prices remain empty. Saving such a
card writes only the policy and creates no empty unit-price override, so the
existing channel/catalog price remains available. A model factor alone does not
publish a new model or establish a previously unknown price. Explicit zero unit
prices are saved. Prices shared by a multi-model group card remain unchanged
when its first model is removed; only the removed model's exact policy rules
are cleared.

Deleting a group price card removes its models' rules for the same supplier,
including unit-specific rules. The explicit **Keep rates only** action removes
the price card while retaining those rules visibly in the advanced editor.
Prices and the one policy draft save in a single version-checked group update;
invalid or unfinished edits block submission. Editing another policy section
does not discard an unfinished draft.

Channel unit prices can serve several groups. The channel editor offers a
bound-group selector and opens that group's same model-factor editor; it never
writes one customer factor into all bound groups. Account cost-statistics cards
do not expose or apply these customer selling-factor controls.

## Inflight balance reservations

When `billing.inflight_reservation.enabled` is enabled, admission estimates the
same billing-model candidates and channel/group selling prices used by billing.
Text request estimates retain explicit service tiers and reasoning levels using
the endpoint's usage parser after ingress policy rewriting. Unknown sibling
fields from another protocol cannot select a parser or replace the endpoint's
output-token bound, effort or service tier. Configured reasoning factors
also apply to per-request, image, video and audio per-request reservations; an
omitted or unconfigured effort retains factor `1`. Explicit free cards remain
free. Actual usage and response-model declarations can still differ from an
admission estimate.

After selecting and revalidating the account, supported synchronous inference
handlers reconcile the estimate before forwarding. The selected account's actual
model mapping and protocol policy determine the concrete price candidate without
reading other accounts. An explicit public group/channel card (including zero)
and the configured billing-model source keep their settlement precedence. The
public requested model still owns the platform/model selling multipliers.
Increasing an existing balance hold is atomic, excludes that same owner from the
balance sum, and never decreases the amount accepted for an earlier attempt.
Insufficient balance returns the endpoint's billing error, releases this
request's slots and hold, and does not fail over or send the request upstream.
Expired or released owners cannot be recreated by an increase. A preliminary
free or unpriced estimate can create a hold after selection if the selected
contract becomes paid.

Responses WebSocket requests hold a separate reservation for each accepted turn;
each turn uses its own public model, pricing snapshot and output bound. A turn's
billing task takes ownership before handler cleanup returns that turn's reference.
Connection closure cleans up any remaining handler references. A retried turn
creates a new owner while keeping its accepted public model and price snapshot.
WS passthrough preserves its channel model; the pool mode applies account model
mapping. Reservation reconciliation follows that actual transport behavior.
Forced HTTP and large-first-frame bridging use the mapped HTTP model even when
the account is configured for passthrough. Reservation and forwarding share the
same attempt's captured transport decision. Later account edits can revalidate
eligibility, but an existing connection reserves against the mapping snapshot
actually used by its proxy; a new connection adopts the updated account mapping.
Channel mapping and OpenAI Fast rules instead refresh after each external
turn's audit. That turn's frozen generation drives its upstream model/tier,
reservation, profitability admission and settlement in passthrough, pool and
HTTP bridge modes. Same-turn retries retain the accepted generation. Ungrouped
keys retain the full turn execution context even without a retail snapshot.
Reservation caps, the existing first-request allowance when there are no other
owners, and dependency-error fail-open behavior remain in effect. These estimates
do not guarantee that a balance can never become negative.

For supported Chat, Responses and Messages conversions, the estimate also
evaluates the reachable account's actual protocol effort scale on the same
public selling card. This includes GLM medium-to-high/xhigh-to-max, Grok effort
normalization, OpenAI-to-Anthropic xhigh-to-max, Messages conversion defaults,
Claude 5.5 model defaults, and group effort rules applied after account mapping.
Native Messages that retain xhigh do not acquire the OpenAI conversion's max
level. Account metadata comes from the scheduler snapshot with a two-second
budget; admission does not start provider model discovery. A model-routed
account whose catalog can select another protocol considers only protocols
supported by that same account. Raw Chat settlement records the final outbound
effort after provider rewriting, so the charged factor matches the wire value.
Nested and flat OpenAI effort fields retain their positions: a typed protocol
converter may omit a compatibility field that a direct Responses path accepts.
Its resulting default factor `1` remains part of the reservation estimate.
If account metadata is unavailable, an authoritative public card reserves the
largest amount among its own configured effort factors and the unconfigured
factor `1`; recovery cannot substitute another public SKU or turn a free card
into a paid one.
Fast-tier, effort-conversion and mapped-model fallback reads within one estimate
reuse the same success or failure for each authorization pool and share one
two-second lookup budget. Results never carry into another request; recovery
and another group, platform or forced-routing pool obtain their own evidence.

OpenAI token reservations also evaluate the frozen Fast policy and execution
group's forced-priority setting. This covers Chat, Responses and Messages
conversion to either OpenAI protocol. Ungrouped keys also pin and evaluate global
and user-scoped Fast rules before ingress and selected-account reservation;
their absence of a retail group does not disable an upstream tier rewrite.
The OpenAI gateway's ungrouped default is OpenAI, including when no target
context was supplied. Explicit platform overrides and resolved targets retain
precedence; the generic gateway does not acquire that default.
Ungrouped catalog fallback also preserves the accepted `service_tier`:
`priority`/`fast` use the catalog's Fast rates and `flex` uses its Flex rates in
both reservation and settlement.
The exact Messages `anthropic-beta`
Fast token becomes priority only for an OpenAI conversion; a native Anthropic
request or another target platform does not acquire an OpenAI tier. Scoped
policy matching uses reachable accounts and their final normalized upstream
model names, while preserving the public model's selling card and the original
retail group's Free Fast setting. Missing account-pool evidence conservatively
reserves the highest possible tier; unavailable global settings retain the
existing pass behavior and emit a structured warning. Dependency reads have a
two-second budget, bounded by the request deadline. Fast-tier calculation for
ordinary traffic and group-only force/free settings does not enumerate the
account pool. Effort conversion also skips account lookup when the authoritative
public card has no effort factors.
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


## Image-count billing

OpenAI and composite groups can enable `image_count_billing` when image
generation is allowed. It defaults to `false` and applies only to dedicated
image endpoints. Responses, Chat and
WebSocket requests that also produce images keep their token billing path.

For dedicated images, enabling the switch overrides a token price card with
per-image billing. Explicit group or channel image/per-request cards remain
eligible; context-based intervals use the actual input usage to select the tier,
and the selected unit price is multiplied by the returned image count. A token
price alone is not evidence of an image price. Without an explicit group image
price, an identified catalog image price, or an enumerated built-in image price,
the request cannot obtain a priced reservation or write financial usage.
The conservative image reservation uses the highest price across 1K, 2K and
4K. When this switch overrides a token card, every possible size tier must have
an identified image price before a priced reservation; configuring only one size
cannot price an unknown size. An explicitly configured zero is a known free price,
whereas an absent price is not. This check uses the admitted pricing snapshot,
including delayed/asynchronous execution, so later configuration changes cannot
fill a missing price for an already captured request.

Successful dedicated OpenAI image responses use the observed image count.
When the response supplies no count, the enabled option applies a one-image
minimum as a gateway billing policy, not a provider guarantee. Disabling the
option preserves the endpoint's existing requested-count/stream fallback.

The accepted request captures the switch, group/channel prices, public-model
and platform factors, and customer multipliers in its pricing snapshot. Editing
the group later cannot change that request's quote, reservation or settlement.
Durable task snapshots also retain image-price evidence; restoring them never
looks up a current catalog to fill missing evidence. Historical snapshots without
the switch retain the default disabled behavior. Unknown image prices do not
become the historical generic image fallback when this switch forces image
billing.
