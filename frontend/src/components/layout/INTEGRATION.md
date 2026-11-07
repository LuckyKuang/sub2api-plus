# Layout Components Integration Guide

Add pages to the existing application. The bootstrap, router and stores already
exist; keep their initialization and navigation guards in their owning files.

## Owners

| Source | Responsibility |
| --- | --- |
| [main.ts](../../main.ts) and [App.vue](../../App.vue) | Application bootstrap and root UI. |
| [router/index.ts](../../router/index.ts) | Route registration, first-navigation auth restoration and access/feature guards. |
| [router/meta.d.ts](../../router/meta.d.ts) and [router/title.ts](../../router/title.ts) | Supported metadata and translated document titles. |
| [AppLayout.vue](AppLayout.vue) | Authenticated page shell and support-context presentation. |
| [AuthLayout.vue](AuthLayout.vue) | Authentication card and optional footer. |
| [AppSidebar.vue](AppSidebar.vue), [AppHeader.vue](AppHeader.vue) | Navigation, title and user controls. |

## Adding a page

1. Follow an existing view under [views](../../views), choosing AppLayout with
   the appropriate `appearance` or AuthLayout.
2. Add a lazy route to the maintained route array. Use the existing access
   metadata and feature checks rather than replacing the guard with a copied
   minimal example.
3. Set `titleKey` and, where applicable, `descriptionKey` using existing locale
   conventions. Add navigation only where the feature should be visible.
4. Reuse shared styles and controls under the
   [UI interaction contract](../../../../docs/UI_INTERACTION.md).

Current user API-key navigation is `/keys`; administrator redeem management is
`/admin/redeem`. View imports live under their current `views/user` or
`views/admin` directories; consult the router before copying a route.

See [layout examples](EXAMPLES.md) for supported slots and
[the store guide](../../stores/README.md) for session/UI state. Component and
route regressions live in [layout tests](__tests__) and
[router tests](../../router/__tests__). Run validation in the platform container
specified by [CONTRIBUTING.md](../../../../CONTRIBUTING.md#development-checks).
