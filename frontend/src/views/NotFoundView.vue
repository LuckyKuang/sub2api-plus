<template>
  <div
    class="auth-layout relative flex min-h-screen items-center justify-center overflow-hidden p-[var(--shell-gutter)]"
  >
    <div class="card relative z-10 w-full max-w-md p-6 text-center sm:p-8">
      <!-- 404 Display -->
      <div class="mb-6">
        <div class="relative inline-flex items-center justify-center">
          <span
            class="select-none text-[7.5rem] font-bold leading-none tracking-tight text-gray-200/80 dark:text-dark-700/70 sm:text-[9.5rem]"
          >
            404
          </span>
          <div class="absolute inset-0 flex items-center justify-center">
            <div
              class="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 shadow-lg shadow-primary-500/25"
            >
              <Icon name="exclamationTriangle" size="xl" class="text-white" />
            </div>
          </div>
        </div>
      </div>

      <!-- Text Content -->
      <div class="mb-7">
        <h1 class="mb-2 text-2xl font-bold tracking-tight text-gray-900 dark:text-white">
          {{ t('errors.pageNotFound') }}
        </h1>
        <p class="text-sm leading-relaxed surface-muted">
          {{ t('errors.pageNotFoundDescription') }}
        </p>
      </div>

      <!-- Action Buttons -->
      <div class="flex flex-col justify-center gap-3 sm:flex-row">
        <button type="button" @click="goBack" class="btn btn-secondary">
          <Icon name="arrowLeft" size="sm" class="mr-1.5" />
          {{ t('errors.goBack') }}
        </button>
        <router-link to="/home" class="btn btn-primary">
          <Icon name="home" size="sm" class="mr-1.5" />
          {{ t('errors.backToHome') }}
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useProductAppearance } from '@/composables/useProductAppearance'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'

useProductAppearance()

const { t } = useI18n()
const router = useRouter()

function goBack(): void {
  router.back()
}
</script>
