# Vue Router Configuration

[index.ts](index.ts) owns route definitions and navigation guards.
[meta.d.ts](meta.d.ts) defines supported metadata;
[title.ts](title.ts) resolves translated document titles.

## Current entrypoints

This table is representative; use the route array for the complete set.

| Path | Behavior |
| --- | --- |
| `/` | Redirects to public `/home`. |
| `/setup` | Installation flow, guarded by backend setup status. |
| `/login`, `/register`, `/email-verify` | Authentication and registration continuation. |
| `/forgot-password`, `/reset-password` | Password recovery. |
| `/dashboard`, `/keys`, `/usage`, `/profile` | User console routes requiring authentication. |
| `/admin` | Redirects to `/admin/dashboard`. |
| `/admin/redeem` | Administrator redeem-code management. |
| `/admin/support/users/:user_id/…` | Explicit administrator support resource routes. |
| `/:pathMatch(.*)*` | NotFound view. |

## Guards and metadata

The guard restores auth state on first navigation.
`requiresAuth` defaults to true unless explicitly false; `requiresAdmin`
requires an administrator. Unauthenticated protected navigation preserves the
destination in the login redirect query. Already authenticated login/register
visits normally go to the appropriate admin/user dashboard, subject to backend
mode behavior.

Access also depends on setup/backend/simple modes, pending authentication,
support context and feature metadata. Public route metadata alone does not
override those conditions. Preserve these checks when adding routes.

Use the current `RouteMeta` definition for feature requirements, support
resources, `titleKey` and `descriptionKey`. Route guards supplement the backend's
authorization checks.

## Navigation and rendering

```typescript
import { useRouter } from 'vue-router'

const router = useRouter()
await router.push({ name: 'Keys' })
```

Routes use lazy component imports. The existing scroll behavior restores a
saved browser position, otherwise scrolls to the top. Component-instance
caching requires explicit application/component support; it is not provided
automatically by route navigation.

## Changes and verification

Add routes in index.ts, use the existing metadata and translated title helpers,
and update navigation where appropriate. Keep tests tied to access behavior,
including permitted and denied transitions, feature gates, support context and
redirects; see [__tests__](__tests__).

Run checks inside the platform validation container described in
[CONTRIBUTING.md](../../../CONTRIBUTING.md#development-checks).
