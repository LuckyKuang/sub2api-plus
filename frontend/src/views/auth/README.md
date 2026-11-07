# Authentication Views

Authentication pages share [AuthLayout](../../components/layout/AuthLayout.vue).
Route registration and access guards live in [the router](../../router/index.ts);
the barrel [index.ts](index.ts) exports only LoginView and RegisterView.

## Current flows

| Entry | Behavior and source |
| --- | --- |
| [LoginView.vue](LoginView.vue) | Email/password login; conditional CAPTCHA, TOTP challenge, passkey and OAuth entrypoints. There is no “Remember me” checkbox. |
| [RegisterView.vue](RegisterView.vue) | Email, password and confirmation, with settings-driven invitation/promotion/referral fields and CAPTCHA. Email verification can continue through `/email-verify` before registration completes. |
| [EmailVerifyView.vue](EmailVerifyView.vue) | Completes the email verification flow. |
| [ForgotPasswordView.vue](ForgotPasswordView.vue), [ResetPasswordView.vue](ResetPasswordView.vue) | Request and consume password reset tokens when enabled. |
| OAuth callback views in this directory | Complete provider callbacks and any required pending-session/account completion flow; see [the route definitions](../../router/index.ts). |

Use the validation functions in LoginView and RegisterView as the current UI
reference. Login requires a valid email and a nonempty password; registration
currently checks a minimum password length of six and matching confirmation,
with additional checks controlled by public settings. Backend validation remains
authoritative.

## State and API

- [Auth store](../../stores/README.md#auth-store): session restoration, user state,
  token refresh, pending authentication and logout.
- [Authentication API](../../api/auth.ts): login, TOTP completion, registration,
  settings and token operations; the shared client uses the `/api/v1` prefix.
- [Request and response types](../../types/index.ts): `LoginRequest` uses
  `email` and `password`; `RegisterRequest` also carries optional verification,
  CAPTCHA, invitation, promotion and referral data.

A successful password request can return a TOTP challenge. Check
`isTotp2FARequired` before treating the session as authenticated, and complete
`authStore.login2FA` when required. Follow the real LoginView handlers for
CAPTCHA proofs, redirects, pending-session preservation and error handling.

## UI and verification

Reuse shared input/button/surface styles and the existing light/dark appearance
handling. See [the visual guide](VISUAL_GUIDE.md), [flow examples](USAGE_EXAMPLES.md),
[the UI interaction contract](../../../../docs/UI_INTERACTION.md) and
[authentication behavior](../../../../docs/AUTHENTICATION.md).

The [view tests](__tests__/LoginView.spec.ts) and
[auth store tests](../../stores/__tests__/auth.spec.ts) cover authentication
behavior. Run checks through the platform validation container described in
[CONTRIBUTING.md](../../../../CONTRIBUTING.md#development-checks).
