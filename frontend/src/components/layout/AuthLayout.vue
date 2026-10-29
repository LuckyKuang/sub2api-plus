<template>
  <div class="auth-layout relative flex min-h-screen items-center justify-center px-4 py-10 sm:px-6">

    <!-- Content Container -->
    <div class="relative z-10 flex w-full max-w-md flex-col gap-6">
      <!-- Logo/Brand -->
      <div class="auth-brand px-4 pb-2 text-center">
        <!-- Custom Logo or Default Logo -->
        <template v-if="settingsLoaded">
          <div
            class="mb-4 inline-flex h-14 w-14 items-center justify-center overflow-hidden rounded-2xl"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="mb-2 break-words text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
            {{ siteName }}
          </h1>
          <p class="text-sm surface-muted">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- Card Container -->
      <div class="card auth-card p-6 sm:p-8">
        <slot />
      </div>

      <!-- Footer Links -->
      <div v-if="$slots.footer" class="px-2 text-center text-sm surface-muted">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="text-center text-xs surface-muted">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import { useProductAppearance } from '@/composables/useProductAppearance'

useProductAppearance()

const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API Plus')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

