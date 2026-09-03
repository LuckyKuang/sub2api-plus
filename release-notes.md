Sub2API Plus v0.2.0+custom.002

## Highlights

- Added client-disconnect lifecycle tracking, ordered streak enforcement, automatic user disabling, and administrator event review.
- Added durable Content Moderation session blocks and persisted redacted moderation input for administrator review.

## Changed

- Prompt Audit now scans the official client-controlled transcript; latest-turn blocking includes the nearest preceding assistant/model output, while Content Moderation remains limited to direct-user content.
- Empty IP last-seen times no longer display as permanent bans for unhit automatic blocks.

## Fixed

- Hardened usage settlement after client disconnects so accepted requests retain billing and lifecycle outcomes without silently dropping queued work.
- Closed session-block and disconnect-risk settlement holes so PostgreSQL remains the session-block source of truth and admitted OpenAI WS turns still settle after disconnect.

## Compatibility and migration

Database migrations 245 through 250 add client-disconnect lifecycle state and events, usage completion metadata, durable Content Moderation session blocks, and persisted moderation input.

## Known issues

None.

## Upstream baseline

Official release: v0.2.0
Official commit: aa236488351eb71e120fc2b6fb32e36b0374c918
