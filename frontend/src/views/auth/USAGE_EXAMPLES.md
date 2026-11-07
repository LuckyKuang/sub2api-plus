# Authentication Views Usage Examples

Use the existing [LoginView](LoginView.vue) and [RegisterView](RegisterView.vue)
for complete forms, settings, CAPTCHA handling and redirects. This guide maps
their flows rather than defining a second authentication implementation.

## Password login

1. Open `/login` and enter email and password.
2. Complete the configured CAPTCHA when required.
3. Submit through `authStore.login`, which calls `POST /api/v1/auth/login`.
4. If `isTotp2FARequired(response)` is true, collect the TOTP code and complete
   `authStore.login2FA(response.temp_token, code)`. The initial challenge does
   not establish an authenticated session.
5. Follow LoginView's redirect handling after authentication completes.

Passkey and OAuth controls are enabled by settings. Their handlers use the
existing API and pending-session flows; do not route them through a copied
password-login form.

## Registration

1. Open `/register` and enter email, password and matching confirmation.
2. Supply invitation, promotion, referral and CAPTCHA data when applicable.
3. When email verification is enabled, RegisterView saves pending registration
   data and navigates to `/email-verify`; registration is completed there.
4. Otherwise, RegisterView calls `authStore.register`, backed by
   `POST /api/v1/auth/register`, and follows its success/error handling.

There is no registration username field. Consult RegisterView's
`validateForm` for current input rules and public-setting conditions.

## Imports and ownership

```typescript
import { LoginView, RegisterView } from '@/views/auth'
import { useAuthStore } from '@/stores'
import { isTotp2FARequired } from '@/api/auth'
import type { LoginRequest, RegisterRequest } from '@/types'
```

Keep route additions in [router/index.ts](../../router/index.ts), session
operations in [stores/auth.ts](../../stores/auth.ts), and HTTP calls in
[api/auth.ts](../../api/auth.ts). The router already restores auth state on its
first navigation; see [the store guide](../../stores/README.md#auth-store).
