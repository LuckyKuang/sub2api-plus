<template>
  <header
    class="shell-surface sticky top-[var(--shell-inset-top)] z-30"
  >
    <div class="mx-auto flex max-w-7xl items-center justify-between gap-2 px-3 py-2 sm:px-4">
      <!-- 左:站点 logo + 名称 -->
      <RouterLink to="/home" class="flex min-w-0 items-center gap-2">
        <template v-if="settings">
          <span
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center overflow-hidden rounded-lg border border-[var(--surface-edge)] surface-inset p-1"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-[15px] font-semibold text-gray-950 dark:text-white">
            {{ siteName }}
          </span>
        </template>
        <template v-else>
          <span class="h-8 w-8 flex-shrink-0 animate-pulse rounded-lg surface-inset" aria-hidden="true"></span>
          <span class="h-4 w-24 animate-pulse rounded surface-inset" aria-hidden="true"></span>
        </template>
      </RouterLink>

      <!-- 右:工具栏 + 登录 / 回到后台 -->
      <div class="flex shrink-0 items-center gap-1.5">
        <LocaleSwitcher />
        <button
          type="button"
          class="theme-toggle flex h-8 w-8 items-center justify-center rounded-lg surface-control transition-colors"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="sm" />
          <Icon v-else name="moon" size="sm" />
        </button>
        <RouterLink
          v-if="isAuthenticated"
          :to="backTarget"
          class="btn btn-primary shrink-0"
        >
          {{ t('modelPlaza.nav.backToDashboard') }}
        </RouterLink>
        <RouterLink
          v-else
          :to="{ path: '/login', query: { redirect: '/model-plaza' } }"
          class="btn btn-primary shrink-0"
        >
          {{ t('modelPlaza.nav.login') }}
        </RouterLink>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name || appStore.siteName || 'Sub2API Plus')
const siteLogo = computed(() =>
  sanitizeUrl(settings.value?.site_logo || '', { allowRelative: true, allowDataUrl: true })
)
const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
const isDark = ref(document.documentElement.classList.contains('dark'))

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
</script>
