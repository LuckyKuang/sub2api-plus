<template>
  <BaseDialog
    :show="show"
    :title="t('admin.wecomUsageAlert.title')"
    width="normal"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.wecomUsageAlert.description') }}
      </p>

      <div v-if="loading" class="py-8 text-center text-sm text-gray-400">
        {{ t('common.loading') }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t('admin.wecomUsageAlert.enabled') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.wecomUsageAlert.enabledHint') }}
            </div>
          </div>
          <button
            type="button"
            class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
            :class="form.enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'"
            @click="form.enabled = !form.enabled"
          >
            <span
              class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
              :class="form.enabled ? 'translate-x-5' : 'translate-x-0'"
            />
          </button>
        </div>

        <div>
          <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
            {{ t('admin.wecomUsageAlert.webhookUrl') }}
          </label>
          <input
            v-model="form.webhook_url"
            type="text"
            class="input w-full text-sm"
            :placeholder="t('admin.wecomUsageAlert.webhookUrlPlaceholder')"
          />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.wecomUsageAlert.webhookUrlHint') }}
          </p>
        </div>

        <div>
          <label class="mb-1 flex items-center gap-1 text-xs font-medium text-gray-600 dark:text-gray-400">
            {{ t('admin.wecomUsageAlert.cronExpression') }}
            <HelpTooltip>
              <template #trigger>
                <span class="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400">?</span>
              </template>
              <div class="space-y-1 text-xs">
                <p class="font-medium">{{ t('admin.wecomUsageAlert.cronTooltipTitle') }}</p>
                <p>{{ t('admin.wecomUsageAlert.cronTooltipMeaning') }}</p>
                <p>{{ t('admin.wecomUsageAlert.cronTooltipExampleHourly') }}</p>
                <p>{{ t('admin.wecomUsageAlert.cronTooltipExampleDaily') }}</p>
              </div>
            </HelpTooltip>
          </label>
          <input
            v-model="form.cron_expression"
            type="text"
            class="input w-full text-sm"
            :placeholder="t('admin.wecomUsageAlert.cronPlaceholder')"
          />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.wecomUsageAlert.cronHelp') }}
          </p>
        </div>

        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t('admin.wecomUsageAlert.forceProbe') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.wecomUsageAlert.forceProbeHint') }}
            </div>
          </div>
          <button
            type="button"
            class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
            :class="form.force_probe ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'"
            @click="form.force_probe = !form.force_probe"
          >
            <span
              class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
              :class="form.force_probe ? 'translate-x-5' : 'translate-x-0'"
            />
          </button>
        </div>

        <div v-if="meta.next_run_at || meta.last_run_at || meta.last_error" class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs dark:border-dark-700 dark:bg-dark-900/40">
          <div v-if="meta.next_run_at" class="text-gray-600 dark:text-gray-300">
            {{ t('admin.wecomUsageAlert.nextRun') }}: {{ formatTime(meta.next_run_at) }}
          </div>
          <div v-if="meta.last_run_at" class="mt-1 text-gray-600 dark:text-gray-300">
            {{ t('admin.wecomUsageAlert.lastRun') }}: {{ formatTime(meta.last_run_at) }}
          </div>
          <div v-if="meta.last_error" class="mt-1 text-red-600 dark:text-red-400" :title="meta.last_error">
            {{ t('admin.wecomUsageAlert.lastError') }}: {{ truncatedError }}
          </div>
        </div>

        <div v-if="error" class="text-sm text-red-600 dark:text-red-400">
          {{ error }}
        </div>
        <div v-else-if="successMessage" class="text-sm text-emerald-600 dark:text-emerald-400">
          {{ successMessage }}
        </div>

        <div class="flex flex-wrap justify-end gap-2 pt-2">
          <button
            type="button"
            class="btn btn-secondary text-sm"
            :disabled="testing || saving || !canTest"
            @click="handleTest"
          >
            {{ testing ? t('admin.wecomUsageAlert.testing') : t('admin.wecomUsageAlert.testSend') }}
          </button>
          <button
            type="button"
            class="btn btn-primary text-sm"
            :disabled="saving || testing"
            @click="handleSave"
          >
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import {
  getWeComUsageAlert,
  updateWeComUsageAlert,
  testWeComUsageAlert,
  type WeComUsageAlertConfig
} from '@/api/admin/accounts'

const props = defineProps<{
  show: boolean
  accountId: number | null
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const error = ref<string | null>(null)
const successMessage = ref<string | null>(null)

const form = reactive({
  enabled: false,
  webhook_url: '',
  cron_expression: '0 * * * *',
  force_probe: false
})

const meta = reactive({
  next_run_at: '' as string,
  last_run_at: '' as string,
  last_error: '' as string
})

const canTest = computed(() => form.webhook_url.trim().length > 0)
const truncatedError = computed(() => {
  if (!meta.last_error) return ''
  return meta.last_error.length > 120 ? `${meta.last_error.slice(0, 120)}…` : meta.last_error
})

const applyConfig = (cfg: WeComUsageAlertConfig) => {
  form.enabled = !!cfg.enabled
  form.webhook_url = cfg.webhook_url || ''
  form.cron_expression = cfg.cron_expression || '0 * * * *'
  form.force_probe = !!cfg.force_probe
  meta.next_run_at = cfg.next_run_at || ''
  meta.last_run_at = cfg.last_run_at || ''
  meta.last_error = cfg.last_error || ''
}

const extractErrorMessage = (e: unknown): string => {
  const err = e as { message?: string; reason?: string }
  return err?.message || err?.reason || t('common.error')
}

const formatTime = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).format(date)
}

const loadConfig = async () => {
  if (!props.accountId) return
  loading.value = true
  error.value = null
  successMessage.value = null
  try {
    const cfg = await getWeComUsageAlert(props.accountId)
    applyConfig(cfg)
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  if (!props.accountId || saving.value) return
  saving.value = true
  error.value = null
  successMessage.value = null
  try {
    const cfg = await updateWeComUsageAlert(props.accountId, {
      enabled: form.enabled,
      webhook_url: form.webhook_url.trim(),
      cron_expression: form.cron_expression.trim(),
      force_probe: form.force_probe
    })
    applyConfig(cfg)
    successMessage.value = t('admin.wecomUsageAlert.saveSuccess')
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    saving.value = false
  }
}

const handleTest = async () => {
  if (!props.accountId || testing.value || !canTest.value) return
  testing.value = true
  error.value = null
  successMessage.value = null
  try {
    // Persist current form first so test uses the same webhook/cron fields the user sees.
    const saved = await updateWeComUsageAlert(props.accountId, {
      enabled: form.enabled,
      webhook_url: form.webhook_url.trim(),
      cron_expression: form.cron_expression.trim() || '0 * * * *',
      force_probe: form.force_probe
    })
    applyConfig(saved)
    const cfg = await testWeComUsageAlert(props.accountId)
    applyConfig(cfg)
    successMessage.value = t('admin.wecomUsageAlert.testSuccess')
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    testing.value = false
  }
}

watch(
  () => [props.show, props.accountId] as const,
  ([show, accountId]) => {
    if (show && accountId) {
      loadConfig()
    }
  },
  { immediate: true }
)
</script>
