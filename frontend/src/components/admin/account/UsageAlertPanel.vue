<template>
  <BaseDialog
    :show="show"
    :title="t('admin.usageAlert.title')"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.usageAlert.description') }}
      </p>

      <div v-if="loading" class="py-8 text-center text-sm text-gray-400">
        {{ t('common.loading') }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between gap-2">
          <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t('admin.usageAlert.rulesTitle') }}
            <span class="ml-1 text-xs font-normal text-gray-400">({{ rules.length }}/{{ maxRules }})</span>
          </div>
          <button
            type="button"
            class="btn btn-secondary text-xs"
            :disabled="rules.length >= maxRules"
            @click="addRule"
          >
            {{ t('admin.usageAlert.addRule') }}
          </button>
        </div>

        <div v-if="rules.length === 0" class="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
          {{ t('admin.usageAlert.emptyRules') }}
        </div>

        <div
          v-for="(rule, index) in rules"
          :key="rule._key"
          class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700"
        >
          <div class="flex items-center justify-between gap-3">
            <div class="text-sm font-medium text-gray-800 dark:text-gray-100">
              {{ t('admin.usageAlert.ruleLabel', { n: index + 1 }) }}
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="btn btn-secondary text-xs"
                :disabled="testingIndex === index || saving || !canTest(rule)"
                @click="handleTest(index)"
              >
                {{ testingIndex === index ? t('admin.usageAlert.testing') : t('admin.usageAlert.testSend') }}
              </button>
              <button
                type="button"
                class="text-xs text-red-600 hover:underline dark:text-red-400"
                :disabled="saving || testingIndex !== null"
                @click="removeRule(index)"
              >
                {{ t('common.delete') }}
              </button>
            </div>
          </div>

          <div class="flex items-center justify-between gap-3">
            <div>
              <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.usageAlert.enabled') }}
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.usageAlert.enabledHint') }}
              </div>
            </div>
            <button
              type="button"
              class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="rule.enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'"
              @click="rule.enabled = !rule.enabled"
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="rule.enabled ? 'translate-x-5' : 'translate-x-0'"
              />
            </button>
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageAlert.channel') }}
            </label>
            <select v-model="rule.channel" class="input w-full text-sm">
              <option value="wecom">{{ t('admin.usageAlert.channelWecom') }}</option>
              <option value="feishu">{{ t('admin.usageAlert.channelFeishu') }}</option>
              <option value="custom">{{ t('admin.usageAlert.channelCustom') }}</option>
            </select>
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageAlert.webhookUrl') }}
            </label>
            <input
              v-model="rule.webhook_url"
              type="text"
              class="input w-full text-sm"
              :placeholder="webhookPlaceholder(rule.channel)"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ webhookHint(rule.channel) }}
            </p>
          </div>

          <div>
            <label class="mb-1 flex items-center gap-1 text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageAlert.cronExpression') }}
              <HelpTooltip>
                <template #trigger>
                  <span class="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400">?</span>
                </template>
                <div class="space-y-1 text-xs">
                  <p class="font-medium">{{ t('admin.usageAlert.cronTooltipTitle') }}</p>
                  <p>{{ t('admin.usageAlert.cronTooltipMeaning') }}</p>
                  <p>{{ t('admin.usageAlert.cronTooltipExampleHourly') }}</p>
                  <p>{{ t('admin.usageAlert.cronTooltipExampleDaily') }}</p>
                </div>
              </HelpTooltip>
            </label>
            <input
              v-model="rule.cron_expression"
              type="text"
              class="input w-full text-sm"
              :placeholder="t('admin.usageAlert.cronPlaceholder')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageAlert.cronHelp') }}
            </p>
          </div>

          <div class="flex items-center justify-between gap-3">
            <div>
              <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.usageAlert.thresholdEnabled') }}
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.usageAlert.thresholdEnabledHint') }}
              </div>
            </div>
            <button
              type="button"
              class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="rule.threshold_enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'"
              @click="rule.threshold_enabled = !rule.threshold_enabled"
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="rule.threshold_enabled ? 'translate-x-5' : 'translate-x-0'"
              />
            </button>
          </div>

          <div v-if="rule.threshold_enabled">
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageAlert.thresholdPercent') }}
            </label>
            <input
              v-model.number="rule.threshold_percent"
              type="number"
              min="1"
              max="99"
              step="1"
              class="input w-32 text-sm"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageAlert.thresholdPercentHint') }}
            </p>
          </div>

          <div class="flex items-center justify-between gap-3">
            <div>
              <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.usageAlert.forceProbe') }}
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.usageAlert.forceProbeHint') }}
              </div>
            </div>
            <button
              type="button"
              class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="rule.force_probe ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'"
              @click="rule.force_probe = !rule.force_probe"
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="rule.force_probe ? 'translate-x-5' : 'translate-x-0'"
              />
            </button>
          </div>

          <div
            v-if="rule.next_run_at || rule.last_run_at || rule.last_error"
            class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs dark:border-dark-700 dark:bg-dark-900/40"
          >
            <div v-if="rule.next_run_at" class="text-gray-600 dark:text-gray-300">
              {{ t('admin.usageAlert.nextRun') }}: {{ formatTime(rule.next_run_at) }}
            </div>
            <div v-if="rule.last_run_at" class="mt-1 text-gray-600 dark:text-gray-300">
              {{ t('admin.usageAlert.lastRun') }}: {{ formatTime(rule.last_run_at) }}
            </div>
            <div v-if="rule.last_error" class="mt-1 text-red-600 dark:text-red-400" :title="rule.last_error">
              {{ t('admin.usageAlert.lastError') }}: {{ truncateError(rule.last_error) }}
            </div>
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
            class="btn btn-primary text-sm"
            :disabled="saving || testingIndex !== null"
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
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import {
  getUsageAlert,
  updateUsageAlert,
  testUsageAlert,
  type UsageAlertChannel,
  type UsageAlertConfig,
  type UsageAlertRule
} from '@/api/admin/accounts'

type EditableRule = UsageAlertRule & { _key: string }

const props = defineProps<{
  show: boolean
  accountId: number | null
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const maxRules = 20

const loading = ref(false)
const saving = ref(false)
const testingIndex = ref<number | null>(null)
const error = ref<string | null>(null)
const successMessage = ref<string | null>(null)
const rules = ref<EditableRule[]>([])

let keySeq = 0
const nextKey = () => {
  keySeq += 1
  return `local-${keySeq}`
}

const emptyRule = (): EditableRule => ({
  _key: nextKey(),
  id: '',
  enabled: false,
  channel: 'wecom',
  webhook_url: '',
  cron_expression: '0 * * * *',
  force_probe: false,
  threshold_enabled: false,
  threshold_percent: 80,
  next_run_at: null,
  last_run_at: null,
  last_error: ''
})

const toEditable = (rule: UsageAlertRule): EditableRule => ({
  _key: rule.id || nextKey(),
  id: rule.id || '',
  enabled: !!rule.enabled,
  channel: (rule.channel || 'wecom') as UsageAlertChannel,
  webhook_url: rule.webhook_url || '',
  cron_expression: rule.cron_expression || '0 * * * *',
  force_probe: !!rule.force_probe,
  threshold_enabled: !!rule.threshold_enabled,
  threshold_percent: rule.threshold_percent || 80,
  next_run_at: rule.next_run_at || null,
  last_run_at: rule.last_run_at || null,
  last_error: rule.last_error || ''
})

const applyConfig = (cfg: UsageAlertConfig) => {
  rules.value = (cfg.rules || []).map(toEditable)
}

const payloadRules = (): UsageAlertRule[] =>
  rules.value.map((rule) => ({
    id: rule.id || undefined,
    enabled: !!rule.enabled,
    channel: rule.channel,
    webhook_url: rule.webhook_url.trim(),
    cron_expression: rule.cron_expression.trim() || '0 * * * *',
    force_probe: !!rule.force_probe,
    threshold_enabled: !!rule.threshold_enabled,
    threshold_percent: rule.threshold_enabled ? Number(rule.threshold_percent) || 0 : 0
  }))

const canTest = (rule: EditableRule) => rule.webhook_url.trim().length > 0

const webhookPlaceholder = (channel: UsageAlertChannel) => {
  if (channel === 'feishu') return t('admin.usageAlert.webhookPlaceholderFeishu')
  if (channel === 'custom') return t('admin.usageAlert.webhookPlaceholderCustom')
  return t('admin.usageAlert.webhookPlaceholderWecom')
}

const webhookHint = (channel: UsageAlertChannel) => {
  if (channel === 'feishu') return t('admin.usageAlert.webhookHintFeishu')
  if (channel === 'custom') return t('admin.usageAlert.webhookHintCustom')
  return t('admin.usageAlert.webhookHintWecom')
}

const truncateError = (value: string) => (value.length > 120 ? `${value.slice(0, 120)}…` : value)

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

const addRule = () => {
  if (rules.value.length >= maxRules) return
  rules.value.push(emptyRule())
}

const removeRule = (index: number) => {
  rules.value.splice(index, 1)
}

const loadConfig = async () => {
  if (!props.accountId) return
  loading.value = true
  error.value = null
  successMessage.value = null
  try {
    const cfg = await getUsageAlert(props.accountId)
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
    const cfg = await updateUsageAlert(props.accountId, { rules: payloadRules() })
    applyConfig(cfg)
    successMessage.value = t('admin.usageAlert.saveSuccess')
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    saving.value = false
  }
}

const handleTest = async (index: number) => {
  if (!props.accountId || testingIndex.value !== null) return
  const rule = rules.value[index]
  if (!rule || !canTest(rule)) return
  testingIndex.value = index
  error.value = null
  successMessage.value = null
  try {
    const draft = payloadRules()[index]
    const cfg = await testUsageAlert(props.accountId, {
      rule_id: draft.id || '',
      rule: draft
    })
    if (draft.id) {
      const updated = (cfg.rules || []).find((r) => r.id === draft.id)
      if (updated) {
        const key = rules.value[index]._key
        rules.value[index] = { ...toEditable(updated), _key: key }
      }
    }
    successMessage.value = t('admin.usageAlert.testSuccess')
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    testingIndex.value = null
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
