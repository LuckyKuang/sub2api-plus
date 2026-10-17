<template>
  <header class="sticky top-0 z-30 border-b border-gray-200/70 bg-white/90 backdrop-blur dark:border-dark-700/70 dark:bg-dark-950/90">
    <div class="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-3.5 sm:px-6">
      <RouterLink to="/docs" class="flex min-w-0 items-center gap-3">
        <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
          <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
        </span>
        <span class="min-w-0">
          <span class="block truncate text-base font-semibold text-gray-950 dark:text-white">{{ siteName }}</span>
          <span class="block truncate text-xs text-gray-500 dark:text-dark-400">{{ t('docs.title') }}</span>
        </span>
      </RouterLink>

      <RouterLink
        v-if="isAuthenticated"
        :to="backTarget"
        class="inline-flex flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-r from-primary-500 to-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-primary-500/25 transition-all duration-200 hover:from-primary-600 hover:to-primary-700"
      >
        {{ t('docs.nav.backToDashboard') }}
      </RouterLink>
      <RouterLink
        v-else
        :to="{ path: '/login', query: { redirect: route.fullPath } }"
        class="inline-flex flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-r from-primary-500 to-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-primary-500/25 transition-all duration-200 hover:from-primary-600 hover:to-primary-700"
      >
        {{ t('docs.nav.login') }}
      </RouterLink>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API Plus')
const siteLogo = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true,
  }),
)
const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
</script>
