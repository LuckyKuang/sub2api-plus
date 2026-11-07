# Layout Components

The layout components share the
[console interaction contract](../../../../docs/UI_INTERACTION.md). Use its
surface tokens, breakpoints, sticky offsets and mobile focus behavior.

## Components

### 1. AppLayout.vue

[AppLayout.vue](AppLayout.vue) renders the sidebar, header, support-context banner
when applicable, and a default slot for page content. Its props are `workspace`
and `appearance` (`user` or `admin`, default `admin`).

The component has no `title` slot. Titles come from route metadata and the
header's title resolution; a heading inside page content is separate.

### 2. AppSidebar.vue

[AppSidebar.vue](AppSidebar.vue) owns role/settings-aware navigation, active
states, collapse/mobile controls and theme controls. It renders SVG icon
components. Consult its current menu definitions and the
[router](../../router/index.ts) when adding an entry.

### 3. AppHeader.vue

[AppHeader.vue](AppHeader.vue) owns the mobile menu button, route-derived title,
user controls and administrator support selection where applicable. AppLayout
renders it automatically. Use the existing title/i18n helpers when changing
route labels.

### 4. AuthLayout.vue

[AuthLayout.vue](AuthLayout.vue) supplies public-settings branding, a centered
form card, default and optional `footer` slots, and copyright. It uses shared
appearance styles, including dark-mode support.

### TablePageLayout.vue

[TablePageLayout.vue](TablePageLayout.vue) provides `actions`, `filters`,
`table` and `pagination` slots. It measures available desktop height and
switches to the mobile layout below 1024px. Import it directly; it is not
exported by [index.ts](index.ts).

## Integration

See [examples](EXAMPLES.md) and [integration guidance](INTEGRATION.md).
The existing [router](../../router/index.ts), [app store](../../stores/app.ts)
and [auth store](../../stores/auth.ts) already provide navigation and session
setup; preserve those owners when adding pages.

Existing regressions live in [__tests__](__tests__). Run checks inside the
platform validation container described in
[CONTRIBUTING.md](../../../../CONTRIBUTING.md#development-checks).
