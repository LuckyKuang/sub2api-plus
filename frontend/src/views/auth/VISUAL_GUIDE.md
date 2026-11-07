# Authentication Views Visual Guide

The current visual sources are [AuthLayout.vue](../../components/layout/AuthLayout.vue),
[LoginView.vue](LoginView.vue), [RegisterView.vue](RegisterView.vue) and
[the shared styles](../../styles). Use these with the
[UI interaction contract](../../../../docs/UI_INTERACTION.md) when changing
spacing, surface colors, focus behavior or responsive layout.

## Layout and appearance

AuthLayout supplies a centered responsive container, public-settings branding,
the form card, a default content slot, an optional footer slot and copyright.
The layout already supports the application's appearance system and dark-mode
styles through [useProductAppearance](../../composables/useProductAppearance.ts).
Preserve the shared surface and control tokens across appearances.

## Visible states

| Flow | States to review |
| --- | --- |
| Login | Email/password, password visibility, configured CAPTCHA, loading/error feedback, TOTP challenge, enabled passkey/OAuth controls. |
| Registration | Email/password/confirmation, optional invitation/promotion/referral controls, CAPTCHA, legal consent when configured, email verification continuation. |
| Shared layout | Loaded/custom branding, long translations, footer links, narrow viewport, keyboard focus, light and dark appearances. |

Use real settings and component state when reviewing these screens. The former
username/“Remember me” wireframes and fixed indigo-only palette do not describe
the implemented forms. Bundle sizes, contrast ratios, browser support and
performance numbers require measurements; this guide does not claim results
that have not been verified.

See [the flow guide](README.md) and [usage examples](USAGE_EXAMPLES.md) for the
behavior behind each state. Component regressions belong in [the view tests](__tests__).
