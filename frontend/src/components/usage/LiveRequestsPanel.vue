<template>
  <div class="space-y-4 p-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="text-xs text-gray-500"><p>{{ t('inspection.liveHint') }}</p><p>{{ t('inspection.liveWindow', { limit: snapshot?.limit ?? 200 }) }}</p></div>
      <RefreshControl :state="refresh" manual />
    </div>
    <p v-if="snapshot?.truncated" role="status" class="text-xs text-amber-600">{{ t('inspection.liveTruncated') }}</p>
    <button v-if="pending" type="button" class="btn btn-secondary" @click="showLatest">{{ t('inspection.newRecords') }}</button>
    <DetailWorkspace :show="selected != null" :title="t('inspection.title')" @close="selected = null">
      <DataTable :columns="columns" :data="rows" :loading="!snapshot && refresh.fetching.value" clickable-rows :active-row-key="selected?.id" @rowClick="select">
        <template #cell-started_at="{ row }"><button type="button" class="text-left text-xs text-primary-600 underline decoration-dotted" :aria-label="t('inspection.view')" @click.stop="select(row)">{{ formatDateTime(row.started_at) }}</button></template>
        <template #cell-model="{ row }"><div class="max-w-[200px] truncate" :title="row.model">{{ row.model }}</div><div class="text-xs text-gray-500">{{ row.request_type }}<span v-if="row.turn"> · #{{ row.turn }}</span></div></template>
        <template #cell-key_name="{ row }"><div class="max-w-[160px] truncate">{{ row.key_name }}</div><div class="text-xs text-gray-500">{{ row.group_name }}</div></template>
        <template #cell-user_id="{ row }">#{{ row.user_id }}<div class="text-xs text-gray-500">{{ row.key_name }}</div></template>
        <template #cell-status="{ row }"><span class="badge" :class="row.status === 'processing' ? 'badge-info' : ['failed', 'incomplete', 'client_disconnected'].includes(row.status) ? 'badge-warning' : 'badge-gray'">{{ t(`inspection.${row.status}`) }}</span></template>
        <template #cell-elapsed_ms="{ row }"><LiveElapsed :row="row" :clock="observationClock" /></template>
        <template #cell-tokens="{ row }"><span class="text-xs tabular-nums">{{ row.input_tokens ?? '—' }} → {{ row.output_tokens ?? '—' }}</span></template>
        <template #cell-rate="{ row }"><span class="text-xs tabular-nums" :title="t('usage.latencyTpsHint')">{{ rate(row) }}</span></template>
      </DataTable>
      <template #detail>
        <div v-if="selected" class="space-y-4 text-sm">
          <dl class="space-y-3">
            <div class="grid grid-cols-[1fr_2fr] gap-3"><dt class="text-gray-500">{{ t('inspection.elapsed') }}</dt><dd class="text-right"><LiveElapsed :row="selected" :clock="observationClock" /></dd></div>
            <div v-for="field in detailFields" :key="field.label" class="grid grid-cols-[1fr_2fr] gap-3">
              <dt class="text-gray-500">{{ field.label }}</dt><dd class="min-w-0 break-all text-right tabular-nums">{{ field.value }}</dd>
            </div>
          </dl>
          <p class="text-xs text-gray-500">{{ t('inspection.liveTimingHint') }}</p>
          <p class="text-xs text-gray-500">{{ t('inspection.settlementUnknown') }} · {{ t('inspection.liveHint') }}</p>
        </div>
      </template>
    </DetailWorkspace>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLiveRequests, type LiveRequest, type LiveRequestSnapshot } from '@/api/liveRequests'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useColumnPreferenceKey } from '@/composables/useColumnPreferenceKey'
import { usePollingBudget } from '@/composables/usePollingBudget'
import { adminSupportContext, supportRequestGeneration } from '@/utils/adminSupportContext'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/format'
import { averageTps, formatTpsNumber } from '@/utils/usageTiming'
import { usageMilliseconds } from '@/utils/usageInspection'
import DataTable from '@/components/common/DataTable.vue'
import DetailWorkspace from '@/components/common/DetailWorkspace.vue'
import RefreshControl from '@/components/common/RefreshControl.vue'
import LiveElapsed from './LiveElapsed.vue'
const props = defineProps<{ mode: 'user' | 'admin' }>()
const { t } = useI18n()
const auth = useAuthStore()
const storageKey = useColumnPreferenceKey(`${props.mode}-live-requests`)
const peers = usePollingBudget(`live-requests:${auth.user?.id ?? 'anonymous'}`)
const rows = ref<LiveRequest[]>([])
const selected = ref<LiveRequest | null>(null)
const snapshot = ref<LiveRequestSnapshot | null>(null)
const pending = ref<LiveRequest[] | null>(null)
const clock = ref(performance.now())
const receivedAt = ref(performance.now())
const serverNow = ref(0)
let controller: AbortController | undefined
let timer: ReturnType<typeof setInterval> | undefined
const columns = computed(() => [
  { key: 'started_at', label: t('usage.time') },
  ...(props.mode === 'admin' ? [{ key: 'user_id', label: t('inspection.owner') }] : []),
  { key: 'model', label: t('usage.model') }, { key: 'key_name', label: t('inspection.source') },
  { key: 'status', label: t('inspection.status') }, { key: 'elapsed_ms', label: t('inspection.elapsed') },
  { key: 'tokens', label: t('usage.tokens') }, { key: 'rate', label: t('usage.latencyTps') },
])
const refresh = useAutoRefresh({
  storageKey, intervals: [2, 5, 15, 30], defaultInterval: 2, defaultEnabled: true,
  minimumIntervalSeconds: () => Math.max(snapshot.value?.poll_after_seconds ?? 2, snapshot.value?.items.length === 0 ? 5 : 2, peers.value * 2),
  onRefresh: async () => {
    controller?.abort(); controller = new AbortController()
    const signal = controller.signal
    const scope = supportRequestGeneration()
    const owner = auth.user?.id
    const data = await getLiveRequests(props.mode, signal)
    if (signal.aborted || scope !== supportRequestGeneration() || owner !== auth.user?.id) return
    const previous = new Map(rows.value.map(row => [row.id, row]))
    data.items = data.items.map(item => {
      const old = previous.get(item.id)
      return old?.version === item.version && old.status === item.status ? old : item
    })
    serverNow.value = Date.parse(data.server_now); receivedAt.value = performance.now(); clock.value = receivedAt.value
    snapshot.value = data
    if (selected.value) {
      selected.value = data.items.find(item => item.id === selected.value?.id) ?? { ...selected.value, status: selected.value.status === 'processing' ? 'unknown' : selected.value.status }
      const latest = new Map(data.items.map(item => [item.id, item]))
      rows.value = rows.value.map(item => latest.get(item.id) ?? (item.status === 'processing' ? { ...item, status: 'unknown' } : item))
      if (data.items.some(item => !rows.value.some(old => old.id === item.id))) pending.value = data.items
    } else { rows.value = data.items; pending.value = null }
  },
})
const observationClock = { now: clock, receivedAt, serverNow,
  healthy: computed(() => refresh.enabled.value && !refresh.error.value && !refresh.paused.value),
}
function rate(row: LiveRequest): string {
  if (row.output_tokens == null || !row.finished_at) return '—'
  const value = averageTps({ ...row, output_tokens: row.output_tokens, timing_version: 1, first_token_ms: row.first_token_ms ?? null,
    duration_ms: row.duration_ms ?? null, first_output_kind: row.first_output_kind ?? null, image_output_tokens: row.image_output_tokens ?? 0, audio_output_tokens: row.audio_output_tokens ?? 0, native_compaction_v2: false, stream: row.request_type !== 'sync' })
  return value == null ? '—' : `${formatTpsNumber(value)} tok/s`
}
function select(row: LiveRequest) { selected.value = row }
function showLatest() { if (pending.value) rows.value = pending.value; pending.value = null; selected.value = null }
const detailFields = computed(() => {
  const r = selected.value
  if (!r) return []
  return [
    ['usage.model', r.model], ['inspection.status', t(`inspection.${r.status}`)], ['usage.time', formatDateTime(r.started_at)],
    ['usage.latencyFirstByte', usageMilliseconds(r.first_byte_ms)],
    ['usage.latencyFirstToken', usageMilliseconds(r.first_token_ms)], ['usage.latencyDuration', usageMilliseconds(r.duration_ms)],
    ['usage.latencyTps', rate(r)], ['usage.apiKeyFilter', r.key_name], ['admin.usage.group', r.group_name ?? '—'],
    ['usage.endpoint', r.endpoint], ['inspection.requestId', r.request_id], ['inspection.observedAt', formatDateTime(r.updated_at)],
    ...(props.mode === 'admin' ? [['admin.usage.account', r.account_id ? String(r.account_id) : '—']] : []),
  ].map(([key, value]) => ({ label: t(key), value }))
})
onMounted(() => { void refresh.refresh(); timer = setInterval(() => { if (!document.hidden) clock.value = performance.now() }, 250) })
watch([() => adminSupportContext.value?.userId, () => auth.user?.id], () => {
  controller?.abort(); rows.value = []; snapshot.value = null; selected.value = null; pending.value = null
})
onBeforeUnmount(() => { controller?.abort(); if (timer) clearInterval(timer) })
</script>
