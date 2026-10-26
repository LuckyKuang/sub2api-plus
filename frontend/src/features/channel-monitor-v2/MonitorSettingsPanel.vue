<template>
  <section class="w-full space-y-5 py-2">
    <header
      class="page-header glass-surface workspace-shell mb-0 flex flex-wrap items-center justify-between gap-3 p-5 sm:p-6"
    >
      <div class="min-w-0">
        <h2 class="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-blue-50/[0.15] text-blue-500 dark:bg-blue-900/30 dark:text-blue-400">
            <Icon name="chart" size="sm" />
          </span>
          {{ t('channelMonitorV2.settings.title') }}
        </h2>
        <p class="page-description mt-1.5 text-xs surface-muted">
          {{ t('channelMonitorV2.settings.description') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="saving || !dirty || saveConflict"
        @click="save"
      >
        <Icon name="check" size="sm" />
        {{ t('channelMonitorV2.settings.save') }}
      </button>
    </header>

    <div
      v-if="!systemModeV2"
      class="rounded-2xl border border-amber-200 bg-amber-50/90 px-4 py-3 text-sm text-amber-900 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-100"
      role="status"
    >
      {{
        t('channelMonitorV2.settings.modeBanner', {
          mode: systemModeLabel,
          modeV2: t('channelMonitorV2.settings.modeV2'),
        })
      }}
      <router-link class="ml-1 font-medium underline" to="/admin/settings">{{ t('admin.settings.tabs.features') }}</router-link>
    </div>

    <div
      v-if="loading"
      class="card workspace-shell flex min-h-[200px] items-center justify-center text-sm surface-muted"
    >
      <span class="animate-pulse">{{ t('channelMonitorV2.settings.loading') }}</span>
    </div>

    <div v-else-if="!draft" class="card workspace-shell p-5">
      <button type="button" class="btn btn-secondary" @click="load">{{ t('channelMonitorV2.settings.retry') }}</button>
    </div>

    <fieldset v-else-if="draft" class="min-w-0 space-y-5" :disabled="saving">
      <div v-if="saveConflict" class="rounded-2xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-100" role="alert">
        <p>{{ t('channelMonitorV2.settings.saveConflict') }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-3" data-testid="reload-conflict" @click="load">{{ t('channelMonitorV2.settings.reloadConfig') }}</button>
      </div>

      <div class="card workspace-shell">
        <div class="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div>
            <strong class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.settings.enableTitle') }}</strong>
            <p class="mt-0.5 text-xs surface-muted">
              {{ t('channelMonitorV2.settings.enableHint') }}
            </p>
          </div>
          <Toggle v-model="draft.enabled" />
        </div>
        <div class="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div>
            <strong class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.settings.refreshTitle') }}</strong>
            <p class="mt-0.5 text-xs surface-muted">{{ t('channelMonitorV2.settings.refreshHint') }}</p>
          </div>
          <div class="tabs inline-flex w-auto" role="group" :aria-label="t('channelMonitorV2.settings.refreshAria')">
            <button
              type="button"
              class="tab"
              :class="draft.refresh_interval_seconds === 60 ? 'tab-active' : ''"
              @click="draft.refresh_interval_seconds = 60"
            >
              1 min
            </button>
            <button
              type="button"
              class="tab"
              :class="draft.refresh_interval_seconds === 300 ? 'tab-active' : ''"
              @click="draft.refresh_interval_seconds = 300"
            >
              5 min
            </button>
          </div>
        </div>
      </div>

      <MonitorPlatformManager
        :key="configRevision"
        v-model="draft.platforms"
        :group-ids="draft.group_ids"
        :disabled="saving"
      />

      <details class="card settings-collapsible overflow-hidden">
        <summary class="card-header cursor-pointer list-none flex flex-wrap items-center justify-between gap-2 !py-3">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.settings.groupsTitle') }}</h3>
            <p class="mt-0.5 text-xs surface-muted">
              {{
                draft.group_ids.length
                  ? t('channelMonitorV2.settings.groupsSelected', { count: draft.group_ids.length })
                  : t('channelMonitorV2.settings.groupsAll')
              }}
            </p>
          </div>
          <button
            v-if="draft.group_ids.length"
            type="button"
            class="btn btn-ghost btn-sm"
            @click.stop.prevent="draft.group_ids = []"
          >
            {{ t('channelMonitorV2.settings.groupsAll') }}
          </button>
        </summary>
        <div class="px-5 pt-3">
          <input v-model="groupQuery" class="input" type="search" :aria-label="t('channelMonitorV2.settings.searchGroups')" :placeholder="t('channelMonitorV2.settings.searchGroups')" />
        </div>
        <div class="max-h-[min(40vh,280px)] overflow-y-auto px-3 py-2 sm:px-4">
          <div class="grid grid-cols-1 gap-1 sm:grid-cols-2">
            <label
              v-for="group in filteredGroups"
              :key="group.id"
              class="flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition hover:surface-hover"
            >
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500/40"
                :checked="draft.group_ids.includes(group.id)"
                @change="toggleGroup(group.id)"
              />
              <span class="min-w-0 flex-1 truncate font-medium text-gray-800 dark:text-gray-100">{{ group.name }}</span>
              <small class="shrink-0 text-xs surface-muted">{{ platformLabel(group.platform) }} · #{{ group.id }}</small>
            </label>
          </div>
          <p v-if="filteredGroups.length === 0" class="empty-state py-8 text-sm surface-muted">{{ t('channelMonitorV2.settings.groupsEmpty') }}</p>
        </div>
      </details>

      <details class="card settings-collapsible overflow-hidden">
        <summary class="card-header cursor-pointer list-none !py-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.settings.errorsTitle') }}</h3>
          <p class="mt-0.5 text-xs surface-muted">
            {{ t('channelMonitorV2.settings.errorsHint') }}
          </p>
        </summary>
        <div class="max-h-[min(40vh,320px)] overflow-y-auto px-3 py-2 sm:px-4">
          <div class="grid grid-cols-1 gap-1 sm:grid-cols-2">
            <label
              v-for="category in errorCategories"
              :key="category"
              class="flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition hover:surface-hover"
            >
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500/40"
                :checked="isCategoryIgnored(category)"
                @change="toggleIgnoredCategory(category)"
              />
              <span class="min-w-0 flex-1 truncate font-medium text-gray-800 dark:text-gray-100">
                {{ categoryLabel(category) }}
              </span>
              <small class="shrink-0 font-mono text-[10px] surface-muted">{{ category }}</small>
            </label>
          </div>
        </div>
        <div class="border-t border-gray-100 px-5 py-3 text-xs dark:border-dark-700 surface-muted">
          {{
            t('channelMonitorV2.settings.ignoredSummary', {
              ignored: draft.ignored_error_categories?.length || 0,
              counted: countedErrorCategoryCount,
            })
          }}
        </div>
      </details>

      <details class="card settings-collapsible overflow-hidden">
        <summary class="card-header cursor-pointer list-none !py-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.settings.healthTitle') }}</h3>
          <p class="mt-0.5 text-xs surface-muted">
            {{ t('channelMonitorV2.settings.healthHint') }}
          </p>
        </summary>
        <div class="grid grid-cols-1 gap-4 px-5 py-4 sm:grid-cols-2 lg:grid-cols-4">
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.minimumSample') }}</span>
            <input v-model.number="draft.health_thresholds.minimum_sample" class="input" type="number" min="1" max="10000" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.warningError') }}</span>
            <input v-model.number="warningErrorPercent" class="input" type="number" min="0" max="100" step="0.1" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.criticalError') }}</span>
            <input v-model.number="criticalErrorPercent" class="input" type="number" min="0" max="100" step="0.1" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.targetTtft') }}</span>
            <input v-model.number="draft.health_thresholds.target_ttft_ms" class="input" type="number" min="1" step="100" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.warningTtft') }}</span>
            <input v-model.number="draft.health_thresholds.warning_ttft_ms" class="input" type="number" min="1" step="100" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.criticalTtft') }}</span>
            <input v-model.number="draft.health_thresholds.critical_ttft_ms" class="input" type="number" min="1" step="100" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.warningCache') }}</span>
            <input v-model.number="warningCachePercent" class="input" type="number" min="0" max="100" step="0.1" />
          </label>
          <label class="block">
            <span class="input-label">{{ t('channelMonitorV2.settings.fields.criticalCache') }}</span>
            <input v-model.number="criticalCachePercent" class="input" type="number" min="0" max="100" step="0.1" />
          </label>
        </div>
      </details>

      <div class="space-y-2">
        <div class="rounded-2xl border border-gray-200 surface-inset px-4 py-3 text-xs dark:border-dark-600 surface-muted">
          <p class="font-medium text-gray-800 dark:text-gray-100">{{ t('channelMonitorV2.settings.userContractTitle') }}</p>
          <ul class="mt-1.5 list-disc space-y-0.5 pl-4">
            <li>{{ t('channelMonitorV2.settings.userContract.health') }}</li>
            <li>{{ t('channelMonitorV2.settings.userContract.trend') }}</li>
            <li>{{ t('channelMonitorV2.settings.userContract.latency') }}</li>
            <li>{{ t('channelMonitorV2.settings.userContract.models') }}</li>
          </ul>
        </div>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-gray-200 px-4 py-3 text-xs dark:border-dark-600 surface-muted">
        <span>{{ t(dirty ? 'channelMonitorV2.settings.unsavedChanges' : 'channelMonitorV2.settings.noChanges') }}</span>
        <button type="button" class="btn btn-ghost btn-sm" :disabled="!dirty || saving" data-testid="reset-draft" @click="resetDraft">{{ t('channelMonitorV2.settings.resetChanges') }}</button>
      </div>
    </fieldset>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { platformLabel as getPlatformLabel } from '@/utils/platformColors'
import MonitorPlatformManager from './MonitorPlatformManager.vue'
import { normalizePlatform } from './monitorModelSettings'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getChannelMonitorMode, isChannelMonitorV2Mode } from '@/utils/featureFlags'
import {
  getConfig,
  updateConfig,
  MONITOR_ERROR_CATEGORIES,
  type MonitorConfig,
} from '@/api/channelMonitorV2'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'

const { t, te } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const draft = ref<MonitorConfig | null>(null)
const original = ref('')
const groups = ref<AdminGroup[]>([])
const groupQuery = ref('')
const configRevision = ref(0)
const saveConflict = ref(false)
const filteredGroups = computed(() => groups.value.filter(group =>
  `${group.name} ${getPlatformLabel(group.platform)} ${group.id}`.toLowerCase().includes(groupQuery.value.trim().toLowerCase())
))
const dirty = computed(() => (draft.value ? JSON.stringify(draft.value) !== original.value : false))
const errorCategories = MONITOR_ERROR_CATEGORIES
const countedErrorCategoryCount = computed(
  () => errorCategories.length - (draft.value?.ignored_error_categories?.length || 0)
)
/** System settings mode must be v2 for aggregation to run; config remains editable for prep. */
const systemModeV2 = computed(() => isChannelMonitorV2Mode())
const systemModeLabel = computed(() => {
  if (!appStore.cachedPublicSettings?.channel_monitor_enabled) {
    return t('channelMonitorV2.settings.modeClosed')
  }
  const mode = getChannelMonitorMode()
  if (mode === 'v3') return t('channelMonitorV3.modeV3')
  return mode === 'v1'
    ? t('channelMonitorV2.settings.modeV1')
    : t('channelMonitorV2.settings.modeV2')
})
const defaultThresholds = {
  minimum_sample: 50,
  warning_error_rate: 0.05,
  critical_error_rate: 0.20,
  target_ttft_ms: 3000,
  warning_ttft_ms: 3000,
  critical_ttft_ms: 10000,
  // Higher is better: below 85% watch, below 60% critical.
  warning_cache_rate: 0.85,
  critical_cache_rate: 0.60,
  error_weight: 0.60,
  ttft_weight: 0.20,
  cache_weight: 0.20,
}

/** Factory ignored categories (matches backend DefaultChannelMonitorV2IgnoredErrorCategories). */
const defaultIgnoredErrorCategories = [
  'authentication',
  'client_cancelled',
  'content_policy',
  'context_limit',
  'group_access',
  'model_unsupported',
  'not_found',
  'quota_or_balance',
] as const
function percentModel(key: 'warning_error_rate' | 'critical_error_rate' | 'warning_cache_rate' | 'critical_cache_rate') {
  return computed({
    get: () => ((draft.value?.health_thresholds?.[key] ?? defaultThresholds[key]) * 100),
    set: (value: number) => {
      if (!draft.value) return
      draft.value.health_thresholds[key] = Math.max(0, Math.min(100, Number(value) || 0)) / 100
    },
  })
}
const warningErrorPercent = percentModel('warning_error_rate')
const criticalErrorPercent = percentModel('critical_error_rate')
const warningCachePercent = percentModel('warning_cache_rate')
const criticalCachePercent = percentModel('critical_cache_rate')

function toggleGroup(id: number) {
  if (!draft.value) return
  draft.value.group_ids = draft.value.group_ids.includes(id)
    ? draft.value.group_ids.filter((value) => value !== id)
    : [...draft.value.group_ids, id].sort((a, b) => a - b)
}

function isCategoryIgnored(category: string): boolean {
  return Boolean(draft.value?.ignored_error_categories?.includes(category))
}

function toggleIgnoredCategory(category: string) {
  if (!draft.value) return
  const current = new Set(draft.value.ignored_error_categories || [])
  if (current.has(category)) current.delete(category)
  else current.add(category)
  draft.value.ignored_error_categories = [...current].sort()
}

function categoryLabel(category: string) {
  const key = `channelMonitorV2.errorCategories.${category}`
  return te(key) ? t(key) : category
}

const platformLabel = getPlatformLabel

function normalizeConfig(value: MonitorConfig): MonitorConfig {
  const ignored = value.ignored_error_categories
  return {
    ...value,
    platforms: value.platforms.map(normalizePlatform),
    group_ids: [...value.group_ids],
    health_thresholds: { ...defaultThresholds, ...(value.health_thresholds || {}) },
    // Preserve explicit empty arrays from the server (operator cleared all).
    ignored_error_categories: [
      ...(ignored == null ? [...defaultIgnoredErrorCategories] : ignored),
    ].sort(),
  }
}

function cloneConfig(value: MonitorConfig): MonitorConfig {
  return JSON.parse(JSON.stringify(value)) as MonitorConfig
}

function resetDraft() {
  if (!original.value || saving.value) return
  draft.value = JSON.parse(original.value) as MonitorConfig
  configRevision.value++
}

async function load() {
  loading.value = true
  try {
    const [value, groupRows] = await Promise.all([getConfig(), adminAPI.groups.getAllIncludingInactive()])
    const normalized = normalizeConfig(value)
    draft.value = cloneConfig(normalized)
    groups.value = groupRows
    original.value = JSON.stringify(normalized)
    saveConflict.value = false
    configRevision.value++
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('channelMonitorV2.settings.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!draft.value || saving.value || saveConflict.value) return
  saving.value = true
  try {
    const payload = normalizeConfig(draft.value)
    const value = await updateConfig(payload)
    const normalized = normalizeConfig(value)
    draft.value = cloneConfig(normalized)
    original.value = JSON.stringify(normalized)
    saveConflict.value = false
    configRevision.value++
    appStore.showSuccess(t('channelMonitorV2.settings.saveSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('channelMonitorV2.settings.saveFailed')))
    const status = (error as { status?: number; response?: { status?: number } })?.status ?? (error as { response?: { status?: number } })?.response?.status
    saveConflict.value = status === 409
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.settings-collapsible > summary { position: relative; padding-right: 42px; }
.settings-collapsible > summary::after { position: absolute; top: 18px; right: 20px; content: '⌄'; color: #94a3b8; transition: transform .15s; }
.settings-collapsible[open] > summary::after { transform: rotate(180deg); }
.settings-collapsible > summary::-webkit-details-marker { display: none; }
</style>
