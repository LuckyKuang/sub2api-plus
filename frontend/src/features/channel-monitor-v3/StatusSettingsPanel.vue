<template>
  <form class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800" @submit.prevent="save">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV3.settings.title') }}</h2>
    <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.settings.description') }}</p>
    <fieldset v-if="draft" class="mt-6" :disabled="saving || loading">
      <legend class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV3.settings.platformsTitle') }}</legend>
      <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.settings.platformsHint') }}</p>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div v-for="platform in CONCRETE_PLATFORM_OPTIONS" :key="platform.value" class="flex items-center gap-3">
          <Toggle :model-value="!draft.disabled_platforms.includes(platform.value)" :aria-label="t('channelMonitorV3.settings.platformToggle', { platform: platform.label })" :data-testid="`v3-platform-${platform.value}`" :disabled="saving || loading" @update:model-value="setPlatform(platform.value, $event)" />
          <span class="text-sm font-medium text-gray-900 dark:text-white">{{ platform.label }}</span>
        </div>
      </div>
    </fieldset>
    <div v-if="draft" class="mt-6 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
      <label v-for="field in fields" :key="field.key" class="input-label">
        {{ t(`channelMonitorV3.settings.${field.key}`) }}
        <input v-model.number="draft[field.key]" class="input mt-2" type="number" required :min="field.min" :max="field.max" :step="field.step || 1" />
      </label>
    </div>
    <p v-else class="mt-5 text-sm text-gray-500">{{ loading ? t('common.loading') : t('channelMonitorV3.settings.loadFailed') }}</p>
    <div class="mt-6 flex gap-3"><button class="btn btn-primary" type="submit" :disabled="!draft || saving || loading">{{ t('common.save') }}</button><button class="btn btn-secondary" type="button" :disabled="saving || loading" @click="load">{{ t('common.refresh') }}</button></div>
  </form>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStatusConfig, updateStatusConfig, type StatusConfig } from '@/api/channelMonitorV3'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import Toggle from '@/components/common/Toggle.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
const { t } = useI18n()
const app = useAppStore()
const draft = ref<StatusConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const fields: Array<{ key: Exclude<keyof StatusConfig, 'version' | 'disabled_platforms'>; min: number; max: number; step?: number }> = [
  { key: 'minimum_samples', min: 1, max: 10000 },
  { key: 'warning_error_rate', min: 0.001, max: 0.999, step: 0.001 },
  { key: 'outage_error_rate', min: 0.001, max: 1, step: 0.001 },
  { key: 'warning_ttft_ms', min: 100, max: 300000 },
  { key: 'abnormal_windows', min: 1, max: 10 },
  { key: 'recovery_windows', min: 1, max: 10 },
]
function setPlatform(platform: string, enabled: boolean) {
  if (!draft.value) return
  draft.value.disabled_platforms = enabled
    ? draft.value.disabled_platforms.filter(name => name !== platform)
    : [...draft.value.disabled_platforms, platform]
}
async function load() {
  loading.value = true
  try { draft.value = await getStatusConfig() }
  catch (error) { app.showError(extractApiErrorMessage(error, t('channelMonitorV3.settings.loadFailed'))) }
  finally { loading.value = false }
}
async function save() {
  if (!draft.value) return
  saving.value = true
  try {
    draft.value = await updateStatusConfig({ ...draft.value, disabled_platforms: [...draft.value.disabled_platforms] })
    app.showSuccess(t('channelMonitorV3.settings.saved'))
  } catch (error) {
    app.showError(extractApiErrorMessage(error, t('channelMonitorV3.settings.saveFailed')))
  } finally { saving.value = false }
}
onMounted(load)
</script>
