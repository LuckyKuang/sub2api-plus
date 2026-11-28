<template>
  <AppLayout appearance="user" workspace>
    <div class="service-status workspace-page w-full">
      <header class="card workspace-fixed status-heading flex flex-wrap items-center justify-between gap-2.5">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white sm:text-xl">{{ t('channelMonitorV3.title') }}</h1>
          <p class="mt-0.5 text-xs surface-muted">{{ t('channelMonitorV3.subtitle') }}</p>
        </div>
        <div class="flex items-center gap-2.5 text-xs surface-muted">
          <span data-testid="current-through">{{ t('channelMonitorV3.observed') }} {{ time(snapshot?.current_through) }}</span>
          <button class="btn btn-secondary px-2.5" :disabled="loading" :aria-label="t('common.refresh')" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </header>

      <p v-if="error" class="rounded-xl border border-red-200 bg-red-50/[0.15] p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300" role="alert">{{ error }}</p>
      <template v-if="snapshot">
        <section class="overview workspace-fixed flex gap-3 rounded-xl border p-3.5 sm:p-4" :class="`status-${summary.status}`" aria-live="polite">
          <span class="status-ink mt-0.5"><Icon :name="summary.status === 'normal' ? 'check' : 'exclamationTriangle'" size="md" /></span>
          <div class="min-w-0 flex-1">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white sm:text-base">{{ t(snapshot.monitoring_enabled ? `channelMonitorV3.overview.${summary.status}` : 'channelMonitorV3.monitoringDisabled') }}</h2>
            <p class="mt-0.5 text-xs leading-relaxed surface-muted">{{ t(snapshot.monitoring_enabled ? `channelMonitorV3.overviewDescription.${summary.status}` : 'channelMonitorV3.monitoringDisabledHint') }}</p>
            <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs surface-muted">
              <span class="status-ink font-medium">{{ t('channelMonitorV3.activeEvents', { count: summary.active_events }) }}</span>
              <span>{{ t('channelMonitorV3.counts', summary) }}</span>
              <button class="status-ink inline-flex items-center gap-1 font-medium hover:underline" data-testid="view-events" @click="showEvents()">{{ t('channelMonitorV3.viewEvents') }}<Icon name="chevronRight" size="xs" /></button>
            </div>
          </div>
        </section>

        <div class="card workspace-fixed status-toolbar flex flex-wrap items-center justify-between gap-2.5">
          <div class="tabs" role="tablist" :aria-label="t('channelMonitorV3.title')">
            <button id="v3-platform-tab" class="tab" :class="tab === 'platforms' ? 'tab-active' : ''" role="tab" :aria-selected="tab === 'platforms'" aria-controls="v3-platform-panel" @click="tab = 'platforms'">{{ t('channelMonitorV3.platforms') }}</button>
            <button id="v3-events-tab" ref="eventsTab" class="tab" :class="tab === 'events' ? 'tab-active' : ''" role="tab" :aria-selected="tab === 'events'" aria-controls="v3-events-panel" @click="tab = 'events'">{{ t('channelMonitorV3.events') }} <span class="ml-1 text-xs">{{ summary.active_events || '' }}</span></button>
          </div>
          <div class="filter-bar">
            <div class="filter-field">
              <Select
                v-model="platform"
                trigger-class="text-xs"
                :aria-label="t('channelMonitorV3.filter')"
                :searchable="false"
                :options="[{ value: '', label: t('channelMonitorV3.allPlatforms') }, ...snapshot.platforms.map(item => ({ value: item.platform, label: platformLabel(item.platform) }))]"
              />
            </div>
            <div class="segmented-control" role="group" :aria-label="t('channelMonitorV3.range')">
              <button v-for="value in ranges" :key="value" type="button" class="segmented-option" :aria-pressed="range === value" @click="range = value">{{ t(`channelMonitorV3.ranges.${value}`) }}</button>
            </div>
          </div>
        </div>

        <section v-if="tab === 'platforms'" id="v3-platform-panel" class="status-platform-panel" role="tabpanel" aria-labelledby="v3-platform-tab">
          <div class="status-card-grid grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4" tabindex="0">
            <button v-for="item in platforms" :key="item.platform" class="platform-card glass-surface p-3.5 text-left transition-all duration-200 hover:-translate-y-0.5 hover:border-primary-300 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:border-primary-500/40" @click="openDetail(item.platform)">
              <div class="flex items-center justify-between gap-2">
                <div class="flex items-center gap-2">
                  <span class="flex h-8 w-8 items-center justify-center rounded-lg surface-inset surface-muted"><ProviderIcon :provider="item.platform" :size="18" /></span>
                  <h3 class="text-sm font-semibold" :class="platformTextClass(item.platform)">{{ platformLabel(item.platform) }}</h3>
                </div>
                <span class="status-badge" :class="`status-${item.status}`">{{ statusLabel(item.status) }}</span>
              </div>
              <p class="mt-2.5 min-h-8 text-xs leading-4 surface-muted">{{ t(`channelMonitorV3.description.${item.status}`) }}</p>
              <div class="mt-2.5 grid grid-cols-2 gap-2">
                <div class="surface-subcard px-2.5 py-2"><p class="text-[11px] surface-muted">{{ t('channelMonitorV3.successRate') }}</p><p class="mt-0.5 font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-white">{{ percent(item.success_rate) }}</p></div>
                <div class="surface-subcard px-2.5 py-2"><p class="text-[11px] surface-muted">{{ t('channelMonitorV3.ttft') }}</p><p class="mt-0.5 font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-white">{{ latency(item.ttft_p50_ms) }}</p></div>
              </div>
              <div class="mt-3 flex h-5 items-end gap-0.5" :aria-label="t('channelMonitorV3.history')">
                <span v-for="point in item.timeline" :key="point.at" class="history-bar flex-1" :class="`status-${point.status}`" :title="`${time(point.at)} · ${statusLabel(point.status)}`" />
              </div>
              <div class="mt-1.5 flex justify-between text-[10px] surface-muted"><span>{{ t(`channelMonitorV3.ranges.${range}`) }}</span><span>{{ t('channelMonitorV3.now') }}</span></div>
              <div class="mt-3 flex items-center justify-between border-t border-[var(--surface-edge)] pt-2.5 text-xs surface-muted"><span>{{ t('channelMonitorV3.lastRequest') }} {{ time(item.last_request_at) }}</span><Icon name="chevronRight" size="sm" /></div>
            </button>
          </div>
          <p v-if="!platforms.length" class="py-10 text-center text-sm surface-muted">{{ t(snapshot.monitoring_enabled ? 'channelMonitorV3.emptyPlatforms' : 'channelMonitorV3.monitoringDisabled') }}</p>
        </section>

        <section v-else id="v3-events-panel" class="status-events-panel" role="tabpanel" aria-labelledby="v3-events-tab">
          <div class="workspace-fixed flex justify-between text-xs surface-muted"><span>{{ t('channelMonitorV3.events') }}</span><span>{{ t('channelMonitorV3.recent30Days') }}</span></div>
          <div class="status-event-list workspace-scroll" tabindex="0">
          <details v-for="event in incidents" :key="event.id" class="glass-surface" :open="!event.resolved_at">
            <summary class="cursor-pointer p-3.5">
              <span class="font-medium text-gray-900 dark:text-white">{{ t(`channelMonitorV3.eventTitle.${event.severity}`, { platform: platformLabel(event.platform) }) }}</span>
              <span class="status-badge ml-2.5" :class="`status-${event.resolved_at ? 'normal' : event.severity}`">{{ t(`channelMonitorV3.phase.${event.phase}`) }}</span>
              <span class="mt-1.5 block text-xs surface-muted">{{ time(event.started_at) }} · {{ t('channelMonitorV3.autoDetected') }}</span>
            </summary>
            <div class="space-y-3 border-t border-[var(--surface-edge)] px-3.5 py-3 text-xs">
              <p class="leading-5 surface-muted">{{ t(`channelMonitorV3.phaseDescription.${event.phase}`) }}</p>
              <p class="surface-muted">{{ t(event.source_kind === 'probe' ? 'featureExtensions.probeSource' : 'featureExtensions.requestSource') }} · {{ event.model }}</p>
              <ol class="space-y-2 border-l border-[var(--surface-edge)] pl-3">
                <li v-for="(update, i) in event.updates" :key="i"><span class="font-medium text-gray-800 dark:text-gray-200">{{ t(`channelMonitorV3.phase.${update.phase}`) }}</span><span class="ml-2.5 surface-muted">{{ time(update.at) }}</span><p class="mt-0.5 surface-muted">{{ t(`channelMonitorV3.phaseDescription.${update.phase}`) }}</p></li>
              </ol>
            </div>
          </details>
          <p v-if="!incidents.length" class="py-10 text-center text-sm surface-muted">{{ t('channelMonitorV3.emptyEvents') }}</p>
          </div>
        </section>
        <footer class="card workspace-fixed status-footer">
          <div v-if="tab === 'platforms'" class="flex flex-wrap gap-3 text-[11px] surface-muted">
            <span v-for="status in legend" :key="status" class="inline-flex items-center gap-1.5"><span class="legend-dot" :class="`status-${status}`" />{{ statusLabel(status) }}</span>
          </div>
          <p class="text-xs surface-muted">{{ t('channelMonitorV3.note') }}</p>
        </footer>
      </template>
      <p v-else-if="loading" class="py-10 text-center text-sm surface-muted">{{ t('common.loading') }}</p>
    </div>
    <BaseDialog :show="!!detail" :title="detail ? platformLabel(detail.platform) : ''" width="wide" @close="detailPlatform = ''">
      <template v-if="detail">
        <div class="mb-3 flex flex-wrap items-center gap-2.5 text-xs surface-muted"><span class="status-badge" :class="`status-${detail.status}`">{{ statusLabel(detail.status) }}</span>{{ t('channelMonitorV3.lastRequest') }} {{ time(detail.last_request_at) }}</div>
        <p class="mb-3 text-sm surface-muted">{{ t(`channelMonitorV3.description.${detail.status}`) }}</p>
        <h3 class="mb-2 text-sm font-medium text-gray-900 dark:text-white">{{ t('channelMonitorV3.modelStatus') }}</h3>
        <p v-if="snapshot?.current_window_start && snapshot?.current_window_end" data-testid="current-window" class="mb-2 text-xs surface-muted">{{ t('channelMonitorV3.currentWindow') }} {{ time(snapshot.current_window_start) }} – {{ time(snapshot.current_window_end) }}</p>
        <p class="mb-2.5 text-xs surface-muted">{{ t('channelMonitorV3.currentNote') }}</p>
        <div class="record-table-scroll overflow-x-auto"><table class="record-table w-full text-left text-xs">
          <thead class="surface-muted"><tr><th class="pb-2">{{ t('channelMonitorV3.model') }}</th><th class="pb-2">{{ t('channelMonitorV3.recentStatus') }}</th><th class="pb-2">{{ t('channelMonitorV3.lastRequest') }}</th><th class="pb-2">{{ t('channelMonitorV3.eventProgress') }}</th></tr></thead>
          <tbody><tr v-for="model in detailModels" :key="model.model" class="border-t border-[var(--surface-edge)] surface-muted"><td class="py-2 pr-3">{{ model.model }}<span class="mt-0.5 flex flex-wrap gap-x-2 surface-muted"><span v-for="source in model.sources" :key="source">{{ t(source === 'probe' ? 'featureExtensions.probeSource' : 'featureExtensions.requestSource') }}</span></span></td><td class="py-2"><span class="status-badge" :class="`status-${model.status}`">{{ statusLabel(model.status) }}</span><span v-if="model.reason !== 'observed'" class="mt-0.5 block text-xs surface-muted">{{ t(`channelMonitorV3.observation.${model.reason}`) }}</span></td><td class="py-2 text-xs">{{ time(model.lastObserved) }}</td><td class="py-2 text-xs">{{ model.phases.length ? model.phases.map(phase => t(`channelMonitorV3.phase.${phase}`)).join(' · ') : '—' }}</td></tr></tbody>
        </table></div>
        <p v-if="!detailModels.length" class="py-4 text-sm surface-muted">{{ t('channelMonitorV3.emptyModels') }}</p>
      </template>
      <template #footer><button class="btn btn-secondary" @click="showEvents(detailPlatform)">{{ t('channelMonitorV3.viewEvents') }}</button></template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import Select from '@/components/common/Select.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStatusSnapshot, type ServiceStatus, type StatusPlatform, type StatusRange, type StatusSnapshot } from '@/api/channelMonitorV3'
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
const detailModels = computed(() => {
  const models = new Map<string, StatusPlatform['models']>()
  for (const observation of detail.value?.models || []) {
    const observations = models.get(observation.model) || []
    observations.push(observation)
    models.set(observation.model, observations)
  }
  return Array.from(models, ([model, observations]) => ({
    model,
    status: modelStatus(observations),
    lastObserved: observations.map(item => item.last_observed_at).filter((value): value is string => !!value).sort((a, b) => Date.parse(a) - Date.parse(b)).at(-1),
    reason: observationReason(observations),
    phases: [...new Set(observations.map(item => item.incident_phase).filter((value): value is NonNullable<typeof value> => !!value))],
    sources: (['request', 'probe'] as const).filter(source => observations.some(item => (item.source_kind || 'request') === source)),
  })).sort((a, b) => a.model.localeCompare(b.model))
})
// Use the documented joint-observation rules for each model, without exposing
// the internal request-group or probe-monitor scopes in the user interface.
function modelStatus(observations: StatusPlatform['models']): ServiceStatus {
  const statuses = observations.map(item => item.status)
  const known = statuses.filter(status => status !== 'unknown' && status !== 'insufficient')
  if (!known.length) return statuses.includes('insufficient') ? 'insufficient' : 'unknown'
  if (known.every(status => status === 'outage')) return 'outage'
  if (known.some(status => status === 'outage' || status === 'partial')) return 'partial'
  if (known.includes('degraded')) return 'degraded'
  return known.includes('recovering') ? 'recovering' : 'normal'
}
function observationReason(observations: StatusPlatform['models']): string {
  if (observations.some(item => item.observation_reason === 'stale')) return 'stale'
  if (observations.some(item => item.observation_reason === 'observed' || !['unknown', 'insufficient'].includes(item.status))) return 'observed'
  return observations.some(item => item.status === 'insufficient') ? 'insufficient' : 'no_observations'
}
async function openDetail(name: string) {
  detailPlatform.value = name
  await load()
}
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
  return value ? new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—'
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
    if (detailPlatform.value && !value.platforms.some(item => item.platform === detailPlatform.value)) detailPlatform.value = ''
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
const autoRefresh = useAutoRefresh({ storageKey: 'service-status-v3-refresh', intervals: [15], defaultInterval: 15, onRefresh: load, shouldPause: () => document.hidden || loading.value })
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
.status-heading, .status-footer { padding: 12px 14px; }
.status-toolbar { padding: 10px 12px; }
.status-footer { display: flex; flex-direction: column; gap: 8px; }
.status-card-grid { grid-auto-rows: max-content; align-content: start; }
.status-events-panel { display: flex; flex-direction: column; gap: 10px; }
@media (min-width: 1024px) {
  .status-platform-panel, .status-events-panel { display: flex; flex: 1 1 0%; flex-direction: column; gap: 10px; min-height: 0; overflow: hidden; }
  .status-card-grid { flex: 1 1 0%; min-height: 0; overflow: auto; overscroll-behavior: contain; scrollbar-gutter: stable; border-radius: var(--surface-radius); }
}
.status-normal { --status-color: #16a34a; --status-tint: #16a34a0a; }
.status-degraded { --status-color: #ca8a04; --status-tint: #ca8a040a; }
.status-partial { --status-color: #ea580c; --status-tint: #ea580c0a; }
.status-outage { --status-color: #dc2626; --status-tint: #dc26260a; }
.status-recovering { --status-color: #3b82f6; --status-tint: #3b82f60a; }
.status-unknown, .status-insufficient { --status-color: #737373; --status-tint: #7373730a; }
.dark .status-normal { --status-color: #4ade80; }
.dark .status-degraded { --status-color: #facc15; }
.dark .status-partial { --status-color: #fb923c; }
.dark .status-outage { --status-color: #f87171; }
.dark .status-unknown, .dark .status-insufficient { --status-color: var(--surface-muted); }
.overview { background: linear-gradient(var(--status-tint), var(--status-tint)), var(--surface-fill); border-color: color-mix(in srgb, var(--status-color) 28%, var(--surface-edge)); box-shadow: var(--surface-shadow); }
.status-ink { color: var(--status-color); }
.status-badge { display: inline-flex; align-items: center; white-space: nowrap; border-radius: var(--surface-radius-tag); padding: 3px 7px; font-size: 10px; color: var(--status-color); background: var(--status-tint); }
.history-bar { display: block; border-radius: var(--surface-radius-xs); background: var(--status-color); height: 18px; }
.history-bar.status-degraded, .history-bar.status-recovering { height: 14px; }
.history-bar.status-partial { height: 10px; }
.history-bar.status-outage { height: 7px; }
.history-bar.status-unknown, .history-bar.status-insufficient { height: 4px; background: #a3a3a34d; }
.legend-dot { display: block; width: 8px; height: 8px; border-radius: var(--surface-radius-xs); background: var(--status-color); }
.legend-dot.status-unknown { background: #a3a3a34d; }
</style>
