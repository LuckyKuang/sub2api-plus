<template>
  <div class="auth-layout relative flex min-h-screen flex-col p-[var(--shell-gutter)]">
    <!-- Top Utility Bar -->
    <div
      class="shell-surface relative z-10 mx-auto flex w-full max-w-5xl items-center justify-between gap-3 px-4 py-2.5 sm:px-5"
    >
      <router-link
        to="/home"
        class="inline-flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-700 transition-colors hover:surface-hover hover:text-gray-900 dark:text-dark-200 dark:hover:text-white"
      >
        <Icon name="arrowLeft" size="sm" />
        <span>{{ t('errors.backToHome') }}</span>
      </router-link>

      <div class="flex items-center gap-2">
        <LocaleSwitcher />
        <button
          type="button"
          class="theme-toggle flex h-9 w-9 items-center justify-center rounded-lg surface-control transition-colors"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>
      </div>
    </div>

    <!-- Content Container -->
    <div class="relative z-10 m-auto flex w-full max-w-md flex-col gap-5 py-6">
      <!-- Card Container -->
      <div class="card auth-card p-6 sm:p-8">
        <!-- Logo/Brand -->
        <div
          v-if="settingsLoaded"
          class="auth-brand mb-6 flex flex-col items-center border-b border-[var(--surface-edge)] pb-6 text-center"
        >
          <div
            class="mb-3 inline-flex h-12 w-12 items-center justify-center overflow-hidden rounded-xl border border-[var(--surface-edge)] surface-inset p-1.5 shadow-sm"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="break-words text-xl font-semibold tracking-tight text-gray-900 dark:text-white">
            {{ siteName }}
          </h1>
          <p class="mt-1 text-xs surface-muted">
            {{ siteSubtitle }}
          </p>
        </div>

        <slot />

        <!-- Footer Links -->
        <div
          v-if="$slots.footer"
          class="mt-6 border-t border-[var(--surface-edge)] pt-5 text-center text-sm surface-muted"
        >
          <slot name="footer" />
        </div>
      </div>

      <!-- Copyright -->
      <div class="text-center text-xs surface-muted">
        <span class="shell-surface inline-block px-3 py-1.5">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import { useProductAppearance } from '@/composables/useProductAppearance'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

useProductAppearance()

const { t } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API Plus')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())
const isDark = ref(document.documentElement.classList.contains('dark'))

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

onMounted(() => {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia?.('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
  appStore.fetchPublicSettings()
})
</script>

