# Pinia Stores Documentation

[The store barrel](index.ts) exports the frontend's shared Pinia stores.
Read each store's returned API before adding a caller; internal refresh state
and helper functions are not necessarily public actions.

## Auth Store

[auth.ts](auth.ts) owns `user`, `token`, read-only `runMode` and
`pendingAuthSession`, plus authentication/admin/simple-mode computed state.
`isAuthenticated` requires both a token and a user.

- `login(LoginRequest)` accepts email/password and returns either an auth
  response or a TOTP challenge. A challenge does not set authenticated state.
  Use [isTotp2FARequired](../api/auth.ts) and then `login2FA(tempToken, code)`.
- `loginWithPasskey` uses the passkey API and accepts a CAPTCHA proof when required.
- `register(RegisterRequest)` accepts email/password and optional verification,
  CAPTCHA, invitation, promotion and referral fields. See the
  [registration view](../views/auth/RegisterView.vue) for its settings-driven flow.
- `checkAuth()` restores saved state, refreshes user data and schedules token
  refresh. The router invokes it on first navigation.
- `setToken`, `refreshUser`, `logout`, `setPendingAuthSession` and
  `clearPendingAuthSession` handle the other session transitions.

Use [LoginView](../views/auth/LoginView.vue) for a complete login example.
Request types live in [types/index.ts](../types/index.ts). Do not assume that a
resolved `login()` promise means authentication is complete.

The store persists session and refresh data in localStorage using constants
declared in auth.ts, including pending authentication where needed. Use store
actions rather than manually rewriting those keys. See
[authentication documentation](../../../docs/AUTHENTICATION.md) for the owning
security behavior.

## App Store

[app.ts](app.ts) owns sidebar/mobile navigation, loading/toasts, cached public
settings, appearance and administrator version information.

```typescript
import { useAppStore } from '@/stores'

const appStore = useAppStore()
appStore.toggleSidebar()
appStore.setMobileOpen(false)
appStore.showSuccess('Saved')

const toastId = appStore.showToast('info', 'Waiting for completion')
// Omitting duration leaves this toast visible until explicitly dismissed.
appStore.hideToast(toastId)
```

The store also provides loading helpers and cache invalidation actions.
Check its returned API and the relevant callers before resetting shared state.

## Other stores

| Source | Responsibility |
| --- | --- |
| [adminSettings.ts](adminSettings.ts) | Administrator settings and feature configuration. |
| [adminSupportView.ts](adminSupportView.ts) | Explicit administrator support context. |
| [adminCompliance.ts](adminCompliance.ts) | Compliance-related administrator state. |
| [announcements.ts](announcements.ts) | Announcements and read state. |
| [onboarding.ts](onboarding.ts) | Onboarding tour state. |
| [payment.ts](payment.ts) | Payment configuration. |
| [subscriptions.ts](subscriptions.ts) | Subscription state. |

## Verification

See [the store tests](__tests__) for observable session, cache and reset behavior.
Run checks inside the platform validation container from
[CONTRIBUTING.md](../../../CONTRIBUTING.md#development-checks).
