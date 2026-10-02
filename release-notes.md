Sub2API Plus v0.2.11+custom.002

## Highlights

- Add administrator read-only user assistance: the ordinary user pages, menus, feature gates and GET handlers are reused for the selected user, every assisted read writes an `admin.support.read` audit entry, and all mutations plus image submission, redemption and authentication binding stay disabled.
- Add the request-driven Channel Monitor V3 service status mode. It derives authorized platform and incident states from real user request outcomes in `usage_logs` and `ops_error_logs`, sends no probes, and leaves the existing V1 probe and V2 aggregation modes and their saved history untouched.
- Add the authorized available-channel model catalog: models are aggregated per platform and model ID with effective price ranges and per-group quote details, including context, media, service-tier and dynamic pricing conditions, resolved through the same billing path that preserves explicit zero rates.
- Change usage throughput to request-average TPS over total duration, excluding image and audio output tokens while keeping the first-token, total-duration and TPS labels and explaining unavailable or incomplete results.
- Restore integrated searchable, checkbox and rich-option dropdown controls while keeping native selects for plain choices, and repair monitor mode switching, saved-setting synchronization and support-page key status translations.

## Changed

- `GET /api/v1/channels/available` keeps its legacy channel array; `view=catalog` selects the new grouped catalog under the same authentication, feature gate and success envelope.
- Channel monitor keeps V1, V2 and V3 as separate pages with independent configuration; the saved mode is reloaded on entry, on focus and on visibility change, and V1/V2 user visibility and authorization are preserved.
- Administrator assistance reads are registered explicitly as GET routes under `/api/v1/admin/support/users/:user_id`. Assisted keys, profile, usage, subscriptions, payments, redeem history, images and monitors reuse the user handlers and DTOs.
- The assisted `/usage/dashboard/api-keys-usage` read uses GET query parameters instead of a business POST, and continued ownership checks for the target user's keys.
- Locale validation now also checks dynamic API enum values and interpolated UI labels for both shipped locales.

## Fixed

- Restore missing API key status translations and previously untranslated user-page labels.
- Stop stale cached public settings from overwriting the just-saved monitor mode, restore the original V1/V2/V3 mode buttons, and fix key-group layout, monitor status labels and Escape handling.
- Preserve the administrator's own JWT, refresh token and persisted login profile while assisting a user, and keep internal authentication material out of assisted responses.

## Compatibility and migration

Apply SQL migration `272_channel_monitor_v3.sql` before starting the new image. It only creates the `channel_monitor_v3_config`, `channel_monitor_v3_facts`, `channel_monitor_v3_states`, `channel_monitor_v3_incidents` and `channel_monitor_v3_watermark` tables plus their indexes, and is safe for an existing database; no existing table, column or row is modified. Back up persistent data before upgrading.

The channel monitor feature keeps its previous default mode, so an upgrade does not change monitoring behavior by itself. Selecting V3 is an explicit operator action; V3 does not backfill history from periods when it was disabled, and incident silence never resolves an incident.

Usage tables and exports still read the same stored timestamps, but the throughput column is now a request-average TPS over total duration. Numbers therefore change after the upgrade for the same historical rows; image and audio output tokens no longer count toward it. No stored usage data is rewritten.

Assisted administrator reads are read-only by design: editing dialogs open for inspection with disabled submits, and no endpoint accepts an assisted write. Other gateway, user and administrator APIs, the security-audit boundary and compiled outbound identity fingerprints are unchanged.

## Known issues

Channel Monitor V3 only shows terminal outcomes of real requests, so a group without recent traffic, and every model below the configured minimum sample count, remains unknown rather than healthy. Requests whose final outcome was not recorded with a shared request ID are counted per log row instead of being deduplicated.

Completing an assisted read of external embedded sites does not create a user session or forward the administrator's JWT, so those sites keep their own external login state. Assisted images are downloaded with the target user's key ownership checks and are not marked as downloaded.

## Upstream baseline

Official release: v0.2.11
Official commit: 96f4c115c9749078f90cbf210a01d39baf3f53b6
