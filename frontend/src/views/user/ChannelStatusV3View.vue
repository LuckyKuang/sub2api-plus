<template>
  <AppLayout>
    <div class="service-status mx-auto w-full max-w-6xl space-y-5 pb-8">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV3.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.subtitle') }}</p>
        </div>
        <div class="flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
          <span>{{ t('channelMonitorV3.observed') }} {{ time(snapshot?.data_through) }}</span>
          <button class="btn btn-secondary px-3 py-2" :disabled="loading" :aria-label="t('common.refresh')" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </header>

      <p v-if="error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300" role="alert">{{ error }}</p>
      <template v-if="snapshot">
        <section class="overview flex gap-4 rounded-xl border p-5" :class="`status-${summary.status}`" aria-live="polite">
          <span class="status-ink mt-0.5"><Icon :name="summary.status === 'normal' ? 'check' : 'exclamationTriangle'" size="lg" /></span>
          <div class="min-w-0 flex-1">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t(`channelMonitorV3.overview.${summary.status}`) }}</h2>
            <p class="mt-1 text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ t(`channelMonitorV3.overviewDescription.${summary.status}`) }}</p>
            <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-gray-500 dark:text-gray-400">
              <span class="status-ink font-medium">{{ t('channelMonitorV3.activeEvents', { count: summary.active_events }) }}</span>
              <span>{{ t('channelMonitorV3.counts', summary) }}</span>
              <button class="status-ink inline-flex items-center gap-1 font-medium" data-testid="view-events" @click="showEvents()">{{ t('channelMonitorV3.viewEvents') }}<Icon name="chevronRight" size="xs" /></button>
            </div>
          </div>
        </section>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="tabs" role="tablist" :aria-label="t('channelMonitorV3.title')">
            <button id="v3-platform-tab" class="tab" :class="tab === 'platforms' ? 'tab-active' : ''" role="tab" :aria-selected="tab === 'platforms'" aria-controls="v3-platform-panel" @click="tab = 'platforms'">{{ t('channelMonitorV3.platforms') }}</button>
            <button id="v3-events-tab" ref="eventsTab" class="tab" :class="tab === 'events' ? 'tab-active' : ''" role="tab" :aria-selected="tab === 'events'" aria-controls="v3-events-panel" @click="tab = 'events'">{{ t('channelMonitorV3.events') }} <span class="ml-1 text-xs">{{ summary.active_events || '' }}</span></button>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="platform" class="input w-auto text-xs" :aria-label="t('channelMonitorV3.filter')">
              <option value="">{{ t('channelMonitorV3.allPlatforms') }}</option>
              <option v-for="item in snapshot.platforms" :key="item.platform" :value="item.platform">{{ platformLabel(item.platform) }}</option>
            </select>
            <div class="flex rounded-lg border border-gray-200 p-1 dark:border-dark-700" role="group" :aria-label="t('channelMonitorV3.range')">
              <button v-for="value in ranges" :key="value" class="rounded-md px-3 py-1.5 text-xs" :class="range === value ? 'bg-gray-100 font-medium text-gray-900 dark:bg-dark-700 dark:text-white' : 'text-gray-500 dark:text-gray-400'" :aria-pressed="range === value" @click="range = value">{{ t(`channelMonitorV3.ranges.${value}`) }}</button>
            </div>
          </div>
        </div>

        <section v-if="tab === 'platforms'" id="v3-platform-panel" role="tabpanel" aria-labelledby="v3-platform-tab">
          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <button v-for="item in platforms" :key="item.platform" class="platform-card rounded-xl border border-gray-200 bg-white p-5 text-left transition hover:border-gray-400 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-dark-500" @click="detailPlatform = item.platform">
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-2.5">
                  <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300"><ProviderIcon :provider="item.platform" :size="20" /></span>
                  <h3 class="text-base font-semibold" :class="platformTextClass(item.platform)">{{ platformLabel(item.platform) }}</h3>
                </div>
                <span class="status-badge" :class="`status-${item.status}`">{{ statusLabel(item.status) }}</span>
              </div>
              <p class="mt-4 min-h-10 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t(`channelMonitorV3.description.${item.status}`) }}</p>
              <div class="mt-4 grid grid-cols-2 gap-3">
                <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.successRate') }}</p><p class="mt-1 font-mono text-xl font-medium text-gray-900 dark:text-white">{{ percent(item.success_rate) }}</p></div>
                <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.ttft') }}</p><p class="mt-1 font-mono text-xl font-medium text-gray-900 dark:text-white">{{ latency(item.ttft_p50_ms) }}</p></div>
              </div>
              <div class="mt-5 flex h-6 items-end gap-1" :aria-label="t('channelMonitorV3.history')">
                <span v-for="point in item.timeline" :key="point.at" class="history-bar flex-1" :class="`status-${point.status}`" :title="`${time(point.at)} · ${statusLabel(point.status)}`" />
              </div>
              <div class="mt-2 flex justify-between text-[10px] text-gray-400"><span>{{ t(`channelMonitorV3.ranges.${range}`) }}</span><span>{{ t('channelMonitorV3.now') }}</span></div>
              <div class="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400"><span>{{ t('channelMonitorV3.lastRequest') }} {{ time(item.last_request_at) }}</span><Icon name="chevronRight" size="sm" /></div>
            </button>
          </div>
          <p v-if="!platforms.length" class="py-12 text-center text-sm text-gray-500">{{ t('channelMonitorV3.emptyPlatforms') }}</p>
          <div class="mt-4 flex flex-wrap gap-4 text-[11px] text-gray-500 dark:text-gray-400">
            <span v-for="status in legend" :key="status" class="inline-flex items-center gap-1.5"><span class="legend-dot" :class="`status-${status}`" />{{ statusLabel(status) }}</span>
          </div>
        </section>

        <section v-else id="v3-events-panel" class="space-y-3" role="tabpanel" aria-labelledby="v3-events-tab">
          <div class="flex justify-between text-xs text-gray-500 dark:text-gray-400"><span>{{ t('channelMonitorV3.events') }}</span><span>{{ t('channelMonitorV3.recent30Days') }}</span></div>
          <details v-for="event in incidents" :key="event.id" class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800" :open="!event.resolved_at">
            <summary class="cursor-pointer p-5">
              <span class="font-medium text-gray-900 dark:text-white">{{ t(`channelMonitorV3.eventTitle.${event.severity}`, { platform: platformLabel(event.platform) }) }}</span>
              <span class="status-badge ml-3" :class="`status-${event.resolved_at ? 'normal' : event.severity}`">{{ t(`channelMonitorV3.phase.${event.phase}`) }}</span>
              <span class="mt-2 block text-xs text-gray-500 dark:text-gray-400">{{ time(event.started_at) }} · {{ t('channelMonitorV3.autoDetected') }}</span>
            </summary>
            <div class="space-y-4 border-t border-gray-100 px-5 py-4 text-xs dark:border-dark-700">
              <p class="leading-5 text-gray-600 dark:text-gray-400">{{ t(`channelMonitorV3.phaseDescription.${event.phase}`) }}</p>
              <p class="text-gray-500 dark:text-gray-400">{{ event.group_name }} · {{ event.model }}</p>
              <ol class="space-y-3 border-l border-gray-200 pl-4 dark:border-dark-600">
                <li v-for="(update, i) in event.updates" :key="i"><span class="font-medium text-gray-800 dark:text-gray-200">{{ t(`channelMonitorV3.phase.${update.phase}`) }}</span><span class="ml-3 text-gray-400">{{ time(update.at) }}</span><p class="mt-1 text-gray-500 dark:text-gray-400">{{ t(`channelMonitorV3.phaseDescription.${update.phase}`) }}</p></li>
              </ol>
            </div>
          </details>
          <p v-if="!incidents.length" class="py-12 text-center text-sm text-gray-500">{{ t('channelMonitorV3.emptyEvents') }}</p>
        </section>
        <p class="text-xs text-gray-400 dark:text-gray-500">{{ t('channelMonitorV3.note') }}</p>
      </template>
      <p v-else-if="loading" class="py-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
    </div>
    <BaseDialog :show="!!detail" :title="detail ? platformLabel(detail.platform) : ''" width="wide" @close="detailPlatform = ''">
      <template v-if="detail">
        <div class="mb-5 flex flex-wrap items-center gap-3 text-xs text-gray-500 dark:text-gray-400"><span class="status-badge" :class="`status-${detail.status}`">{{ statusLabel(detail.status) }}</span>{{ t('channelMonitorV3.lastRequest') }} {{ time(detail.last_request_at) }}</div>
        <p class="mb-5 text-sm text-gray-500 dark:text-gray-400">{{ t(`channelMonitorV3.description.${detail.status}`) }}</p>
        <h3 class="mb-3 text-sm font-medium text-gray-900 dark:text-white">{{ t('channelMonitorV3.modelStatus') }}</h3>
        <div class="overflow-x-auto"><table class="w-full text-left text-xs">
          <thead class="text-gray-500"><tr><th class="pb-3">{{ t('channelMonitorV3.model') }}</th><th class="pb-3">{{ t('channelMonitorV3.group') }}</th><th class="pb-3">{{ t('channelMonitorV3.recentStatus') }}</th></tr></thead>
          <tbody><tr v-for="model in detail.models" :key="`${model.group_id}:${model.model}`" class="border-t border-gray-100 text-gray-700 dark:border-dark-700 dark:text-gray-300"><td class="py-3 pr-3">{{ model.model }}</td><td class="py-3 pr-3">{{ model.group_name }}</td><td class="py-3"><span class="status-badge" :class="`status-${model.status}`">{{ statusLabel(model.status) }}</span></td></tr></tbody>
        </table></div>
        <p v-if="!detail.models.length" class="py-6 text-sm text-gray-500">{{ t('channelMonitorV3.emptyModels') }}</p>
      </template>
      <template #footer><button class="btn btn-secondary" @click="showEvents(detailPlatform)">{{ t('channelMonitorV3.viewEvents') }}</button></template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStatusSnapshot, type ServiceStatus, type StatusRange, type StatusSnapshot } from '@/api/channelMonitorV3'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { platformLabel, platformTextClass } from '@/utils/platformColors'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

const { t, locale } = useI18n()
const snapshot = ref<StatusSnapshot | null>(null)
const loading = ref(false)
const error = ref('')
const tab = ref<'platforms' | 'events'>('platforms')
const range = ref<StatusRange>('24h')
const platform = ref('')
const detailPlatform = ref('')
const eventsTab = ref<HTMLButtonElement>()
const ranges: StatusRange[] = ['24h', '7d', '30d']
const legend: ServiceStatus[] = ['normal', 'degraded', 'partial', 'outage', 'unknown']
const platforms = computed(() => (snapshot.value?.platforms || []).filter(item => !platform.value || item.platform === platform.value))
const incidents = computed(() => (snapshot.value?.incidents || []).filter(item => !platform.value || item.platform === platform.value))
const detail = computed(() => snapshot.value?.platforms.find(item => item.platform === detailPlatform.value))
const summary = computed(() => {
  const original = snapshot.value?.summary
  if (!platform.value || !original) return original!
  const status = platforms.value[0]?.status || 'unknown'
  return {
    status,
    normal: status === 'normal' ? 1 : 0,
    affected: ['partial', 'outage', 'degraded'].includes(status) ? 1 : 0,
    unknown: ['unknown', 'insufficient'].includes(status) ? 1 : 0,
    recovering: status === 'recovering' ? 1 : 0,
    active_events: incidents.value.filter(item => !item.resolved_at).length,
  }
})
const statusLabel = (status: ServiceStatus) => t(`channelMonitorV3.status.${status}`)
const percent = (value: number | null) => value == null ? '—' : `${(value * 100).toFixed(2)}%`
const latency = (value: number | null) => value == null ? '—' : `≈ ${(value / 1000).toFixed(2)} s`
function time(value?: string | null) {
  return value ? new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) : '—'
}
async function showEvents(filter = '') {
  if (filter) platform.value = filter
  detailPlatform.value = ''
  tab.value = 'events'
  await nextTick()
  eventsTab.value?.focus()
}
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const value = await getStatusSnapshot(range.value, request.signal)
    if (request.signal.aborted) return
    snapshot.value = value
    if (platform.value && !value.platforms.some(item => item.platform === platform.value)) platform.value = ''
    error.value = ''
  } catch {
    if (!request.signal.aborted) {
      snapshot.value = null
      error.value = t('channelMonitorV3.loadFailed')
    }
  } finally {
    if (controller === request) loading.value = false
  }
}
const autoRefresh = useAutoRefresh({ storageKey: 'service-status-v3-refresh', intervals: [60], defaultInterval: 60, onRefresh: load, shouldPause: () => document.hidden || loading.value })
function onVisibilityChange() {
  if (!document.hidden) void load()
}
watch(range, load)
onMounted(() => {
  void load()
  autoRefresh.setEnabled(true)
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onBeforeUnmount(() => {
  controller?.abort()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.service-status { --status-color: #737373; --status-tint: #7373730a; }
.status-normal { --status-color: #16a34a; --status-tint: #16a34a0a; }
.status-degraded { --status-color: #ca8a04; --status-tint: #ca8a040a; }
.status-partial { --status-color: #ea580c; --status-tint: #ea580c0a; }
.status-outage { --status-color: #dc2626; --status-tint: #dc26260a; }
.status-recovering { --status-color: #3b82f6; --status-tint: #3b82f60a; }
.status-unknown, .status-insufficient { --status-color: #737373; --status-tint: #7373730a; }
:global(.dark) .status-normal { --status-color: #4ade80; }
:global(.dark) .status-degraded { --status-color: #facc15; }
:global(.dark) .status-partial { --status-color: #fb923c; }
:global(.dark) .status-outage { --status-color: #f87171; }
:global(.dark) .status-unknown, :global(.dark) .status-insufficient { --status-color: #a3a3a3; }
.overview { background: var(--status-tint); border-color: color-mix(in srgb, var(--status-color) 25%, transparent); }
.status-ink { color: var(--status-color); }
.status-badge { display: inline-flex; align-items: center; white-space: nowrap; border-radius: 5px; padding: 3px 7px; font-size: 10px; color: var(--status-color); background: var(--status-tint); }
.history-bar { display: block; border-radius: 2px; background: var(--status-color); height: 22px; }
.history-bar.status-degraded, .history-bar.status-recovering { height: 17px; }
.history-bar.status-partial { height: 13px; }
.history-bar.status-outage { height: 8px; }
.history-bar.status-unknown, .history-bar.status-insufficient { height: 5px; background: #a3a3a34d; }
.legend-dot { display: block; width: 8px; height: 8px; border-radius: 2px; background: var(--status-color); }
.legend-dot.status-unknown { background: #a3a3a34d; }
</style>
