Sub2API Plus v0.2.19+custom.003

## Highlights

- Improve desktop and mobile console layouts across administrator, user, authentication and setup pages.
- Keep dialog actions, verification inputs, record cards and key instructions readable in light and dark themes.
- Paginate administrator live requests while preserving filtering and export across the complete observation window.

## Fixed

- Keep onboarding actions visible on mobile and retain spacing inside narrow two-factor verification dialogs.
- Use shared dialogs for administrator group and redemption-code operations, with responsive layouts and consistent footer actions.
- Restore record-row borders and rounded surfaces around frozen table columns, and keep mobile record labels, long values and actions within their cards.
- Keep separated table headers and body spacing stable when reactive updates rewrite component classes, preventing repeated resize-driven layout jumps.
- Preserve readable copied and selected states with configured accent colors, including API-key instructions and user security settings.
- Keep administrator live-request controls, empty-state text and pagination visible in shorter desktop windows. Browsing later pages holds new insertions behind the existing new-record notice.

## Compatibility and migration

The official baseline remains `v0.2.19` at `dece26153621959ccd7635814f4b2d08304e681a`. This release introduces no SQL migrations, environment-variable changes, provider-protocol changes or API schema changes. Existing account identities, security-audit boundaries, billing rules and provider capability limits continue to apply.

Administrator live requests use the existing page-size preference, with a default of 20 rows per page. Filters and CSV export still cover the complete bounded observation window. User usage history retains its manual Refresh and Reset actions.

## Known issues

Live requests remain a bounded recent observation window, at most 200 records, rather than a durable request archive. The provider capabilities and operational limits documented in `docs/SEVEN_PROVIDER_SUPPORTED_BUSINESS.md` remain applicable.

## Upstream baseline

Official release: v0.2.19
Official commit: dece26153621959ccd7635814f4b2d08304e681a
