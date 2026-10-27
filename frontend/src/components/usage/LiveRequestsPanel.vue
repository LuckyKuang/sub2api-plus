<template>
  <div ref="panel" class="min-w-0 p-3 sm:p-4" :class="workspace ? 'workspace-body' : 'space-y-4'" @pointerdown="markReading" @focusin="markReading" @wheel.passive="markReading" @touchstart.passive="markReading">
    <div class="flex flex-wrap items-center justify-between gap-3" :class="{ 'workspace-fixed': workspace }">
      <div class="text-xs surface-muted"><p>{{ t('inspection.liveHint') }}</p><p>{{ t('inspection.liveWindow', { limit: snapshot?.limit ?? 200 }) }}</p></div>
      <RefreshControl :state="refresh" switch-visible manual />
    </div>
    <div class="flex flex-wrap items-end gap-2" :class="{ 'workspace-fixed': workspace }">
      <div class="segmented-control" role="group" :aria-label="t('requestList.result')">
        <button v-for="item in ['all', 'success', 'abnormal']" :key="item" type="button" class="segmented-option" :aria-pressed="resultFilter === item" @click="resultFilter = item">{{ t(`requestList.${item}`) }}</button>
      </div>
      <Select v-model="modelFilter" class="min-w-0 w-44 max-w-full" :options="modelOptions" :aria-label="t('usage.model')" :searchable="false" />
      <Select v-model="groupFilter" class="min-w-0 w-40 max-w-full" :options="groupOptions" :aria-label="t('admin.usage.group')" :searchable="false" />
      <Select v-if="mode === 'admin'" v-model="userFilter" class="min-w-0 w-36 max-w-full" :options="userOptions" :aria-label="t('admin.usage.user')" :searchable="false" />
      <div class="ml-auto flex gap-2">
        <button type="button" class="btn btn-secondary text-xs" :disabled="!filteredRows.length" @click="exportWindow">{{ t('requestList.exportWindow') }}</button>
        <details class="relative">
          <summary class="btn btn-secondary cursor-pointer list-none text-xs">{{ t('requestList.columns') }}</summary>
          <div class="absolute right-0 z-30 mt-2 w-52 rounded-xl border border-gray-200 p-2 shadow-xl dark:border-dark-600 surface-panel">
            <label v-for="column in allColumns" :key="column.key" class="flex min-h-9 cursor-pointer items-center gap-2 rounded px-2 text-xs hover:surface-hover">
              <input type="checkbox" :checked="!hiddenColumns.includes(column.key)" :disabled="fixedColumns.includes(column.key)" @change="toggleColumn(column.key)">{{ column.label }}
            </label>
            <button type="button" class="min-h-9 w-full border-t px-2 text-left text-xs text-primary-600 surface-control" @click="resetColumns">{{ t('inspection.resetColumns') }}</button>
          </div>
        </details>
      </div>
    </div>
    <p class="text-xs surface-muted" :class="{ 'workspace-fixed': workspace }">{{ t('requestList.windowFilters') }}</p>
    <p v-if="snapshot?.truncated" role="status" class="text-xs text-amber-600" :class="{ 'workspace-fixed': workspace }">{{ t('inspection.liveTruncated') }}</p>
    <button v-if="pending" type="button" class="btn btn-secondary" :class="{ 'workspace-fixed': workspace }" @click="showLatest">{{ t('inspection.newRecords') }}</button>
    <div v-if="desktop" :class="workspace ? 'workspace-body !gap-0' : 'overflow-x-auto'">
      <DataTable :class="{ 'workspace-table-viewport': workspace }" :columns="columns" :data="filteredRows" :loading="!snapshot && refresh.fetching.value">
        <template #cell-started_at="{ row }"><time class="whitespace-nowrap text-xs tabular-nums" :datetime="row.started_at" :title="formatDateTime(row.started_at)">{{ shortTime(row.started_at) }}</time></template>
        <template #cell-model="{ row }"><div class="max-w-52 break-words text-sm font-medium" :title="row.model">{{ row.model }}</div><div v-if="row.reasoning_effort" class="text-xs surface-muted">{{ row.reasoning_effort }}</div></template>
        <template #cell-user_id="{ row }"><span class="text-xs">#{{ row.user_id }}</span><div class="max-w-40 break-all text-xs surface-muted">{{ row.key_name }}</div></template>
        <template #cell-request_type="{ row }"><span class="badge badge-gray">{{ typeLabel(row) }}</span><div v-if="row.turn" class="text-xs surface-muted">#{{ row.turn }}</div></template>
        <template #cell-group_name="{ row }"><span class="block max-w-36 break-words text-xs">{{ row.group_name || '—' }}</span></template>
        <template #cell-status="{ row }"><LiveRequestResult :row="row" /></template>
        <template #cell-speed="{ row }"><LiveRequestMetrics :row="row" :clock="observationClock" /></template>
        <template #cell-performance="{ row }"><LiveRequestMetrics :row="row" :clock="observationClock" performance-column /></template>
        <template #cell-tokens="{ row }"><LiveRequestTokens :row="row" /></template>
        <template #cell-actual_cost="{ row }"><span class="whitespace-nowrap text-xs font-medium tabular-nums" :class="row.actual_cost != null ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500'">{{ cost(row) }}</span></template>
        <template #cell-client="{ row }"><button type="button" class="max-w-44 text-left text-xs surface-control" :title="row.user_agent" @click="copyClient(row)"><span class="block truncate">{{ clientName(row) }}</span><span class="block break-all surface-muted">{{ row.ip_address || '—' }}</span></button></template>
      </DataTable>
    </div>
    <div v-else class="space-y-2" :aria-busy="refresh.fetching.value">
      <p v-if="!snapshot && refresh.fetching.value" class="p-6 text-center text-sm surface-muted">{{ t('common.loading') }}</p>
      <p v-else-if="!filteredRows.length" class="p-6 text-center text-sm surface-muted">{{ t('empty.noData') }}</p>
      <article v-for="row in filteredRows" :key="row.id" class="min-w-0 rounded-xl border border-gray-200 surface-inset p-3 dark:border-dark-700">
        <div class="flex items-start justify-between gap-2"><div class="min-w-0 break-all text-sm font-medium">{{ row.model }} <span class="text-[11px] font-normal surface-muted">{{ typeLabel(row) }}</span></div><span class="shrink-0 text-xs tabular-nums">{{ cost(row) }}</span></div>
        <div class="mt-1 flex flex-wrap items-center justify-between gap-1"><div><LiveRequestResult :row="row" /></div><time class="text-xs text-gray-500" :datetime="row.started_at">{{ shortTime(row.started_at) }}</time></div>
        <div class="mt-1 flex items-start justify-between gap-2"><div class="min-w-0"><LiveRequestTokens :row="row" /></div><span class="min-w-0 max-w-[40%] break-words text-right text-xs surface-muted">{{ row.group_name || '—' }}</span></div>
        <button type="button" class="mt-1 min-h-9 text-xs text-primary-600 surface-control" :aria-expanded="expanded.has(row.id)" :aria-controls="`live-fields-${row.id}`" @click="toggleExpanded(row.id)">{{ t(expanded.has(row.id) ? 'requestList.less' : 'requestList.more') }}</button>
        <dl v-if="expanded.has(row.id)" :id="`live-fields-${row.id}`" class="space-y-3 border-t border-gray-200 pt-3 text-xs dark:border-dark-700">
          <div v-if="mode === 'admin'"><dt class="text-gray-500">{{ t('admin.usage.user') }}</dt><dd>#{{ row.user_id }}</dd></div>
          <div><dt class="text-gray-500">{{ t('usage.apiKeyFilter') }}</dt><dd class="break-all">{{ row.key_name || '—' }}</dd></div>
          <div><dt class="text-gray-500">{{ t('usage.time') }}</dt><dd>{{ formatDateTime(row.started_at) }}</dd></div>
          <div><dt class="text-gray-500">{{ t('admin.usage.group') }}</dt><dd class="break-all">{{ row.group_name || '—' }}</dd></div>
          <div><dt class="text-gray-500">{{ t('requestList.speed') }}</dt><dd><LiveRequestMetrics :row="row" :clock="observationClock" /></dd></div>
          <div><dt class="text-gray-500">{{ t('requestList.performance') }}</dt><dd><LiveRequestMetrics :row="row" :clock="observationClock" performance-column /></dd></div>
          <div><dt class="text-gray-500">{{ t('usage.userAgent') }}</dt><dd class="select-text break-all">{{ row.user_agent || '—' }}</dd></div>
          <div><dt class="text-gray-500">IP</dt><dd class="select-text break-all">{{ row.ip_address || '—' }}</dd></div>
          <button type="button" class="btn btn-secondary text-xs" @click="copyClient(row)">{{ t('inspection.copy') }}</button>
        </dl>
      </article>
    </div>
    <p v-if="copied" role="status" class="text-xs text-primary-600" :class="{ 'workspace-fixed': workspace }">{{ t('inspection.copied') }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { getLiveRequests, type LiveRequest, type LiveRequestSnapshot } from '@/api/liveRequests'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useColumnPreferenceKey } from '@/composables/useColumnPreferenceKey'
import { usePollingBudget } from '@/composables/usePollingBudget'
import { adminSupportContext, supportRequestGeneration } from '@/utils/adminSupportContext'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/format'
import { liveAbnormal, liveActive, liveCSVValue } from '@/utils/liveRequestPresentation'
import { usageMoney } from '@/utils/usageInspection'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import RefreshControl from '@/components/common/RefreshControl.vue'
import LiveRequestResult from './LiveRequestResult.vue'
import LiveRequestMetrics from './LiveRequestMetrics.vue'
import LiveRequestTokens from './LiveRequestTokens.vue'
const props = defineProps<{ mode: 'user' | 'admin'; workspace?: boolean }>()
const { t } = useI18n()
const auth = useAuthStore()
const desktop = useMediaQuery('(min-width: 768px)')
const storageKey = useColumnPreferenceKey(`${props.mode}-live-requests-v2`)
const columnKey = useColumnPreferenceKey(`${props.mode}-live-columns-v2`)
const peers = usePollingBudget(`live-requests:${auth.user?.id ?? 'anonymous'}`)
const panel = ref<HTMLElement>()
const rows = ref<LiveRequest[]>([])
const snapshot = ref<LiveRequestSnapshot | null>(null)
const pending = ref<LiveRequest[] | null>(null)
const expanded = ref(new Set<string>())
const resultFilter = ref('all'), modelFilter = ref(''), groupFilter = ref(''), userFilter = ref('')
const hiddenColumns = ref<string[]>(['client'])
const fixedColumns = ['started_at', 'model', 'status']
const copied = ref(false)
const clock = ref(performance.now()), receivedAt = ref(performance.now()), serverNow = ref(0)
let controller: AbortController | undefined
let timer: ReturnType<typeof setInterval> | undefined
let readingUntil = 0
const allColumns = computed(() => [
  { key: 'started_at', label: t('usage.time') },
  ...(props.mode === 'admin' ? [{ key: 'user_id', label: t('admin.usage.user') }] : []),
  { key: 'model', label: t('usage.model') }, { key: 'request_type', label: t('usage.type') },
  { key: 'group_name', label: t('admin.usage.group') }, { key: 'status', label: t('requestList.result') },
  { key: 'speed', label: t('requestList.speed') }, { key: 'performance', label: t('requestList.performance') },
  { key: 'tokens', label: t('requestList.tokens') }, { key: 'actual_cost', label: t('usage.cost') },
  { key: 'client', label: t('requestList.client') },
])
const columns = computed(() => allColumns.value.filter(column => !hiddenColumns.value.includes(column.key)))
try { const saved: unknown = JSON.parse(localStorage.getItem(columnKey) ?? 'null'); if (Array.isArray(saved)) hiddenColumns.value = saved.filter(key => typeof key === 'string' && !fixedColumns.includes(key)) } catch { /* optional display preference */ }
function saveColumns() { try { localStorage.setItem(columnKey, JSON.stringify(hiddenColumns.value)) } catch { /* optional display preference */ } }
function toggleColumn(key: string) { if (fixedColumns.includes(key)) return; hiddenColumns.value = hiddenColumns.value.includes(key) ? hiddenColumns.value.filter(item => item !== key) : [...hiddenColumns.value, key]; saveColumns() }
function resetColumns() { hiddenColumns.value = ['client']; saveColumns() }
function markReading() { readingUntil = performance.now() + 5000 }
function isReading() { return expanded.value.size > 0 || performance.now() < readingUntil || window.scrollY > 160 || !!window.getSelection()?.toString() || !!panel.value?.contains(document.activeElement) }
const refresh = useAutoRefresh({
  storageKey, intervals: [2, 5, 15, 30], defaultInterval: 2, defaultEnabled: true, temporaryPause: true,
  minimumIntervalSeconds: () => Math.max(snapshot.value?.poll_after_seconds ?? 2, snapshot.value?.items.length === 0 ? 5 : 2, peers.value * 2),
  onRefresh: async () => {
    controller?.abort(); controller = new AbortController()
    const signal = controller.signal, scope = supportRequestGeneration(), owner = auth.user?.id
    const data = await getLiveRequests(props.mode, signal)
    if (signal.aborted || scope !== supportRequestGeneration() || owner !== auth.user?.id) return
    serverNow.value = Date.parse(data.server_now); receivedAt.value = performance.now(); clock.value = receivedAt.value
    snapshot.value = data
    if (rows.value.length && isReading()) {
      const latest = new Map(data.items.map(item => [item.id, item]))
      rows.value = rows.value.map(item => latest.get(item.id) ?? (liveActive(item) ? { ...item, status: 'unknown' as const } : item))
      pending.value = data.items.some(item => !rows.value.some(old => old.id === item.id)) ? data.items : null
    } else { rows.value = data.items; pending.value = null }
  },
})
const observationClock = { now: clock, receivedAt, serverNow, healthy: computed(() => refresh.enabled.value && !refresh.error.value && !refresh.paused.value) }
function showLatest() { if (pending.value) rows.value = pending.value; pending.value = null; expanded.value = new Set() }
function toggleExpanded(id: string) { const next = new Set(expanded.value); if (next.has(id)) next.delete(id); else next.add(id); expanded.value = next }
const filteredRows = computed(() => rows.value.filter(row =>
  (resultFilter.value === 'all' || (resultFilter.value === 'success' ? row.status === 'completed' : liveAbnormal(row))) &&
  (!modelFilter.value || row.model === modelFilter.value) && (!groupFilter.value || row.group_name === groupFilter.value) && (!userFilter.value || String(row.user_id) === userFilter.value)))
const options = (label: string, values: string[]) => [{ value: '', label }, ...Array.from(new Set(values.filter(Boolean))).sort().map(value => ({ value, label: value }))]
const modelOptions = computed(() => options(t('requestList.allModels'), rows.value.map(row => row.model)))
const groupOptions = computed(() => options(t('requestList.allGroups'), rows.value.map(row => row.group_name ?? '')))
const userOptions = computed(() => options(t('requestList.allUsers'), rows.value.map(row => String(row.user_id))))
function typeLabel(row: LiveRequest) {
  if (row.native_compaction_v2) return t('usage.compactionOnly')
  return ({ sync: t('requestList.sync'), stream: t('requestList.stream'), ws_v2: 'WS', live: 'Live', cyber: 'Cyber', unknown: '—' })[row.request_type] ?? '—'
}
function cost(row: LiveRequest) { return row.actual_cost != null ? usageMoney(row.actual_cost) : t(liveActive(row) ? 'inspection.pending' : 'requestList.unsettled') }
function shortTime(value: string) { const date = new Date(value); return date.toDateString() === new Date().toDateString() ? date.toLocaleTimeString() : date.toLocaleString() }
function clientName(row: LiveRequest) { return row.user_agent?.split(/[ /]/)[0] || '—' }
async function copyClient(row: LiveRequest) { try { await navigator.clipboard.writeText(`${row.user_agent || '—'}\n${row.ip_address || '—'}`); copied.value = true } catch { copied.value = false } }
function exportWindow() {
  const header = ['time', ...(props.mode === 'admin' ? ['user_id', 'key_name'] : []), 'model', 'type', 'group', 'result', 'reason', 'first_byte_ms', 'duration_ms', 'first_token_ms', 'input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'actual_cost', 'user_agent', 'ip_address']
  const body = filteredRows.value.map(row => [row.started_at, ...(props.mode === 'admin' ? [row.user_id, row.key_name] : []), row.model, row.request_type, row.group_name, row.status, row.reason, row.first_byte_ms, row.duration_ms, row.first_token_ms, row.input_tokens, row.output_tokens, row.cache_read_tokens, row.cache_creation_tokens, row.actual_cost, row.user_agent, row.ip_address])
  const url = URL.createObjectURL(new Blob(['\uFEFF' + [header, ...body].map(row => row.map(liveCSVValue).join(',')).join('\r\n')], { type: 'text/csv;charset=utf-8;' }))
  const link = document.createElement('a'); link.href = url; link.download = `requests-window-${Date.now()}.csv`; link.click(); URL.revokeObjectURL(url)
}
onMounted(() => { void refresh.refresh(); timer = setInterval(() => { if (!document.hidden) clock.value = performance.now() }, 250) })
watch([() => adminSupportContext.value?.userId, () => auth.user?.id], () => {
  controller?.abort(); rows.value = []; snapshot.value = null; pending.value = null; expanded.value = new Set()
})
onBeforeUnmount(() => { controller?.abort(); if (timer) clearInterval(timer) })
</script>
