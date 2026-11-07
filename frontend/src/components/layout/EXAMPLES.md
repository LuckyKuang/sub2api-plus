# Layout Component Examples

Use the existing layouts and shared surface classes. These examples illustrate
layout composition; complete authentication forms remain in
[the auth views](../../views/auth/README.md).

## Application page

```vue
<script setup lang="ts">
import { AppLayout } from '@/components/layout'
</script>

<template>
  <AppLayout appearance="user">
    <section class="card p-6">
      <h1 class="text-2xl font-semibold">Page content</h1>
    </section>
  </AppLayout>
</template>
```

Set the header's translated title through route metadata in
[router/index.ts](../../router/index.ts). AppLayout exposes only its default
content slot.

## Authentication card

```vue
<script setup lang="ts">
import { AuthLayout } from '@/components/layout'
</script>

<template>
  <AuthLayout>
    <p class="surface-muted">Authentication page content</p>
    <template #footer>
      <router-link to="/login">Sign in</router-link>
    </template>
  </AuthLayout>
</template>
```

For email/password, TOTP, passkey, OAuth and CAPTCHA behavior, follow
[LoginView.vue](../../views/auth/LoginView.vue) rather than creating a second
login implementation.

## Table page

Import [TablePageLayout.vue](TablePageLayout.vue) directly, and place the existing
table controls in its `actions`, `filters`, `table` and `pagination` slots.
Use [Select.vue](../common/Select.vue) for plain single selection, keeping
search/checkbox interaction inside the shared dropdown when present.
