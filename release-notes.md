Sub2API Plus v0.2.8+custom.002

## Highlights

Second iteration on the official `v0.2.8` baseline. Channel, group, and
account-statistics pricing now accepts a billing multiplier per reasoning
effort level, so none/minimal/low/medium/high/xhigh/max are priced
independently and no model bills above 1x unless an operator configures it.
Billing follows the effort that is actually forwarded upstream across token,
per-request, image, audio, and video paths.

## Changed

- Pricing entries replace the single `max_reasoning_effort_multiplier` with a
  per-effort `reasoning_effort_multipliers` map on channel pricing, account
  statistics pricing rules, and group pricing.
- The built-in 3x Claude Fable 5.1 max-effort default is removed; configure the
  multiplier explicitly to keep the previous cost.
- The Chat Completions, Responses, and OpenAI-native Anthropic paths bill and
  record the forwarded effort (for example OpenAI `xhigh` is forwarded to
  Anthropic as `output_config.effort=max`), and audio, image, and video
  requests now honor the configured level.
- Admin pricing and model plaza surfaces expose one input and one badge per
  effort level, in both English and Chinese.

## Compatibility and migration

Migration 271 is forward-only: it adds the `reasoning_effort_multipliers`
JSONB columns, backfills a configured max multiplier into the `max` entry,
and rewrites group `model_pricing`. It is replay-safe and preserves maps that
were deliberately cleared. Back up the database before upgrade. The legacy
`max_reasoning_effort_multiplier` column is retained as migration input and no
longer read by billing.

## Known issues

None.

## Upstream baseline

Official release: v0.2.8
Official commit: fd80b08c90b55edcad5b00171b53f08721d30da1
