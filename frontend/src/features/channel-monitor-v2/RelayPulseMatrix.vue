<template>
  <section
    class="card workspace-shell flex min-h-[360px] flex-col overflow-visible !p-6"
  >
    <div class="card-header mb-4 flex shrink-0 flex-wrap items-start justify-between gap-3 !border-0 !p-0">
      <div class="min-w-0">
        <h2 class="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <span class="inline-flex h-4 w-4 text-emerald-500" aria-hidden="true">
            <Icon name="grid" size="sm" />
          </span>
          {{ t('channelMonitorV2.matrix.title') }}
        </h2>
        <p class="mt-0.5 text-xs surface-muted">
          {{ t('channelMonitorV2.matrix.description') }}
        </p>
      </div>
      <div class="flex w-full min-w-0 flex-wrap items-center justify-end gap-2 text-xs sm:w-auto surface-muted">
        <span class="badge badge-gray shrink-0">{{ bucketLabel }}</span>
        <span class="hidden text-[11px] sm:inline surface-muted">{{ t('channelMonitorV2.matrix.wheelZoomX') }}</span>
        <button
          type="button"
          class="inline-flex shrink-0 items-center rounded-lg border border-gray-200 px-2 py-1 text-[11px] font-semibold hover:surface-hover disabled:opacity-50 dark:border-dark-700 surface-control surface-muted"
          :disabled="!zoomed"
          @click="resetMatrixZoom"
        >
          {{ t('channelMonitorV2.matrix.resetZoom') }}
        </button>
      </div>
    </div>

    <div class="card-body min-h-0 flex-1 !p-0">
      <div
        v-if="rows.length"
        class="matrix-frame max-h-[min(42vh,420px)] max-w-full rounded-2xl surface-inset p-2"
        @wheel="onMatrixWheel"
      >
        <div ref="headerRailRef" class="matrix-header-rail" :style="headerRailStyle" @scroll="onHeaderScroll">
          <div
            ref="headerRef"
            class="matrix-header matrix-row w-full text-[10px] font-semibold uppercase tracking-wide surface-muted"
            :class="showThroughput ? 'matrix-row--with-tps' : ''"
            :style="headerStyle"
          >
            <span>{{ t('channelMonitorV2.matrix.dimension') }}</span>
            <span>{{ t('channelMonitorV2.metrics.successRate') }}</span>
            <span>{{ t('channelMonitorV2.metrics.ttft') }}</span>
            <span v-if="showThroughput">{{ t('channelMonitorV2.metrics.tps') }}</span>
            <span>{{ t('channelMonitorV2.metrics.cacheRate') }}</span>
            <span class="pulse-axis flex justify-between gap-3">
              <i class="not-italic">{{ axisStart }}</i>
              <i class="not-italic">{{ axisEnd }}</i>
            </span>
          </div>
        </div>
        <div
          ref="scrollRef"
          class="matrix-scroll max-w-full overflow-auto"
          :style="scrollStyle"
          @scroll="onScroll"
          @focusout="onFocusOut"
        >
        <div class="matrix-table w-full" :style="tableStyle">
          <div class="matrix-virtual-body relative" :style="{ height: `${bodyHeight}px` }">
          <div
            v-for="entry in visibleRows"
            :key="entry.key"
            :ref="measureRow"
            :data-index="entry.index"
            :data-row-key="entry.key"
            :style="{ transform: `translateY(${entry.start}px)` }"
            class="matrix-data-row matrix-row absolute inset-x-0 top-0 border-b border-gray-100/80 dark:border-dark-700/60"
            :class="showThroughput ? 'matrix-row--with-tps' : ''"
          >
            <div class="dimension-cell flex min-w-0 items-center gap-2 surface-inset" :title="rowLabel(entry.row)">
              <span :class="['status-dot', cellClass(entry.row.health, entry.row.metrics.request_count)]"></span>
              <strong class="truncate text-xs font-semibold text-gray-800 dark:text-gray-100">{{ rowLabel(entry.row) }}</strong>
            </div>
            <strong class="summary-value surface-inset text-xs font-medium tabular-nums surface-muted">
              {{ successRate(entry.row.metrics) }}
            </strong>
            <strong
              class="summary-value surface-inset text-xs font-medium tabular-nums surface-muted"
              :title="latencyPrivacy(entry.row.metrics.ttft)"
            >
              {{ formatMs(entry.row.metrics.ttft.p50_ms) }}
            </strong>
            <strong
              v-if="showThroughput"
              class="summary-value surface-inset text-xs font-medium tabular-nums surface-muted"
              :title="exactTps(entry.row.metrics.tpm)"
            >
              {{ formatTps(entry.row.metrics.tpm) }}
            </strong>
            <strong
              class="summary-value surface-inset text-xs font-medium tabular-nums surface-muted"
            >
              {{ formatPercent(entry.row.metrics.cache_rate) }}
            </strong>
            <div class="pulse-track grid items-stretch" :style="pulseStyle">
              <span
                v-for="(slot, column) in entry.slots"
                :key="slot.start"
                class="pulse-cell relative rounded-sm border-0 p-0 outline-offset-1"
                :class="[
                  slot.bucket ? cellClass(slot.bucket.health, slot.bucket.metrics.request_count) : 'health-unknown',
                  slot.bucket ? 'has-data' : 'is-empty',
                ]"
                :data-column="column"
                tabindex="0"
                role="img"
                :aria-label="slot.bucket ? bucketTooltip(slot.bucket) : t('channelMonitorV2.matrix.noTrafficAt', { time: formatBucketRange(slot.start) })"
                @mouseenter="showTooltip($event, slot)"
                @mousemove="moveTooltip($event)"
                @mouseleave="hideTooltip"
                @focus="onCellFocus($event, slot, entry.index, column)"
                @keydown="onCellKeydown($event, entry.index, column)"
                @blur="hideTooltip"
              />
            </div>
          </div>
          </div>
        </div>
        </div>
      </div>
      <div v-else class="flex min-h-[200px] items-center justify-center py-8">
        <EmptyState
          :title="t('channelMonitorV2.matrix.emptyTitle')"
          :description="t('channelMonitorV2.empty.description')"
        />
      </div>

      <div class="mt-4 flex flex-col gap-2" :aria-label="t('channelMonitorV2.matrix.legendAria')">
        <div class="flex items-center gap-2 text-[11px] surface-muted">
          <span class="shrink-0">{{ t('channelMonitorV2.matrix.bad') }}</span>
          <div class="score-legend h-2.5 flex-1 overflow-hidden rounded-full"></div>
          <span class="shrink-0">{{ t('channelMonitorV2.matrix.good') }}</span>
        </div>
        <div class="flex flex-wrap gap-4 text-[11px] surface-muted">
          <span class="inline-flex items-center gap-1.5"><i class="status-dot health-score10"></i>{{ t('channelMonitorV2.matrix.healthyLegend') }}</span>
          <span class="inline-flex items-center gap-1.5"><i class="status-dot health-score6"></i>{{ t('channelMonitorV2.matrix.warningLegend') }}</span>
          <span class="inline-flex items-center gap-1.5"><i class="status-dot health-score2"></i>{{ t('channelMonitorV2.matrix.criticalLegend') }}</span>
          <span class="inline-flex items-center gap-1.5"><i class="status-dot health-unknown"></i>{{ t('channelMonitorV2.matrix.unknownLegend') }}</span>
        </div>
      </div>
    </div>

    <MatrixTooltip ref="tooltipRef" />
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, nextTick, onBeforeUnmount, onMounted, onUpdated, ref, watch, type ComponentPublicInstance } from 'vue'
import { defaultRangeExtractor, observeElementRect, useVirtualizer } from '@tanstack/vue-virtual'
import MatrixTooltip from './MatrixTooltip.vue'
import type {
  LatencyMetric,
  MonitorCoverage,
  MonitorHealth,
  MonitorMatrixBucket,
  MonitorMatrixRow,
  MonitorMetric,
} from '@/api/channelMonitorV2'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatCompactNumber } from '@/utils/format'
import {
  formatLatencyPrivacy,
  formatMonitorMs,
  monitorIntlLocale,
  tokensPerSecondFromTpm,
  healthModeScore,
  healthScoreClass,
} from '@/features/channel-monitor-v2/monitorFormat'
import {
  applyWheelZoom,
  clientXRatio,
  isZoomed,
  resetZoom,
  sliceByZoom,
  type ZoomState,
  type MatrixViewState,
} from '@/features/channel-monitor-v2/monitorZoom'

type HealthMode = 'overall' | 'success' | 'ttft' | 'cache'
const { t, locale } = useI18n()

const props = withDefaults(
  defineProps<{
    rows: MonitorMatrixRow[]
    coverage: MonitorCoverage
    healthMode: HealthMode
    /** When false, RPM/TPM are omitted from tooltips (user scale privacy). */
    showThroughput?: boolean
    dimensionKey?: string
    viewState?: MatrixViewState
  }>(),
  { showThroughput: true },
)

const emit = defineEmits<{ 'update:viewState': [state: MatrixViewState] }>()
type AlignedSlot = { start: string; bucket?: MonitorMatrixBucket }

const tooltipRef = ref<InstanceType<typeof MatrixTooltip> | null>(null)
const scrollRef = ref<HTMLElement | null>(null)
const headerRailRef = ref<HTMLElement | null>(null)
const headerRef = ref<HTMLElement | null>(null)
const headerHeight = ref(36)
const headerWidth = ref<number | null>(null)
const scrollbarWidth = ref(0)
const viewportWidth = ref(typeof window === 'undefined' ? 1024 : window.innerWidth)
const focusedRow = ref<number | null>(null)
const pendingFocus = ref<{ row: number; column: number; key: string; start: string } | null>(null)
let focusedColumn = 0
let disposed = false
let rowsRevision = 0
let bucketsRevision = 0
let headerObserver: ResizeObserver | undefined
const zoom = ref<ZoomState>(props.viewState?.zoom || resetZoom())
const domainKey = computed(() => JSON.stringify([
  props.dimensionKey || [props.coverage.requested_start, props.coverage.requested_end],
  props.coverage.bucket_seconds,
]))
function saveViewport() {
  const element = scrollRef.value
  const scrollTop = element?.scrollTop ?? props.viewState?.scrollTop ?? 0
  emit('update:viewState', {
    zoom: zoom.value,
    scrollTop,
    scrollLeft: element?.scrollLeft ?? props.viewState?.scrollLeft ?? 0,
    domainKey: domainKey.value,
  })
}
const rowKeys = computed(() => props.rows.map(rowKey))
const rowVirtualizer = useVirtualizer(computed(() => {
  // TanStack memoizes range extraction by callback identity. Capture both pins
  // here so a focus request invalidates that memo before any browser scroll event.
  const focused = focusedRow.value
  const requested = pendingFocus.value?.row
  return {
  count: props.rows.length,
  getScrollElement: () => scrollRef.value,
  getItemKey: (index: number) => rowKeys.value[index],
  estimateSize: () => viewportWidth.value <= 640 ? 48 : 36,
  overscan: 4,
  initialOffset: props.viewState?.scrollTop || 0,
  initialRect: { width: viewportWidth.value, height: Math.max(0, Math.min(420, typeof window === 'undefined' ? 420 : window.innerHeight * 0.42) - headerHeight.value - 16) },
  observeElementRect: (instance: Parameters<typeof observeElementRect<HTMLElement>>[0], callback: Parameters<typeof observeElementRect<HTMLElement>>[1]) =>
    observeElementRect(instance, (rect) => {
      // A temporarily detached/hidden view must not erase the visible range.
      if (rect.height <= 0) return
      viewportWidth.value = rect.width
      callback(rect)
    }),
  rangeExtractor: (range: Parameters<typeof defaultRangeExtractor>[0]) => {
    const indexes = defaultRangeExtractor(range)
    // Retain the source until focus reaches the requested row. A real browser
    // delivers scroll asynchronously, so either row may be outside this range.
    for (const pin of [focused, requested]) {
      if (pin != null && pin < props.rows.length && !indexes.includes(pin)) indexes.push(pin)
    }
    return indexes.sort((a, b) => a - b)
  },
  useAnimationFrameWithResizeObserver: true,
  }
}))
const bodyHeight = computed(() => rowVirtualizer.value.getTotalSize())
const scrollStyle = computed(() => ({
  maxHeight: `max(0px, calc(min(42vh, 420px) - ${headerHeight.value + 16}px))`,
}))
const headerRailStyle = computed(() => ({
  marginRight: `${scrollbarWidth.value}px`,
}))
function syncHeaderGeometry() {
  const viewport = scrollRef.value
  const table = viewport?.querySelector<HTMLElement>('.matrix-table')
  if (!viewport || !table) return
  const width = table.getBoundingClientRect().width
  if (width > 0 && width !== headerWidth.value) headerWidth.value = width
  const gutter = Math.max(0, viewport.offsetWidth - viewport.clientWidth)
  if (gutter !== scrollbarWidth.value) scrollbarWidth.value = gutter
}
function syncHorizontalScroll(source: HTMLElement | null, target: HTMLElement | null) {
  if (source && target && source.scrollLeft !== target.scrollLeft) target.scrollLeft = source.scrollLeft
}
function measureRow(element: Element | ComponentPublicInstance | null) {
  if (element instanceof HTMLElement) rowVirtualizer.value.measureElement(element)
}
function clampScroll() {
  const element = scrollRef.value
  if (!element) return
  const maximum = Math.max(0, rowVirtualizer.value.getTotalSize() - (element.clientHeight || rowVirtualizer.value.scrollRect?.height || 0))
  const top = Math.min(element.scrollTop, maximum)
  if (top !== element.scrollTop) rowVirtualizer.value.scrollToOffset(top)
  saveViewport()
}
function onFocusOut(event: FocusEvent) {
  if (scrollRef.value?.contains(event.relatedTarget as Node | null)) return
  focusedRow.value = null
  pendingFocus.value = null
  hideTooltip()
}
function onScroll() {
  syncHorizontalScroll(scrollRef.value, headerRailRef.value)
  hideTooltip()
  saveViewport()
}
function onHeaderScroll() {
  syncHorizontalScroll(headerRailRef.value, scrollRef.value)
  hideTooltip()
  saveViewport()
}
onMounted(async () => {
  headerObserver = new ResizeObserver(() => {
    const height = headerRef.value?.getBoundingClientRect().height
    if (height && height !== headerHeight.value) headerHeight.value = height
    syncHeaderGeometry()
  })
  if (headerRef.value) headerObserver.observe(headerRef.value)
  if (scrollRef.value) headerObserver.observe(scrollRef.value)
  const table = scrollRef.value?.querySelector<HTMLElement>('.matrix-table')
  if (table) headerObserver.observe(table)
  const height = headerRef.value?.getBoundingClientRect().height
  if (height) headerHeight.value = height
  await nextTick()
  const element = scrollRef.value
  if (element) {
    element.scrollTop = props.viewState?.scrollTop || 0
    element.scrollLeft = props.viewState?.scrollLeft || 0
    rowVirtualizer.value.scrollToOffset(element.scrollTop)
    clampScroll()
    syncHeaderGeometry()
    syncHorizontalScroll(element, headerRailRef.value)
  }
})
// Vue calls function refs with null before removing nodes. Clean after the DOM
// patch so the virtualizer releases detached rows and their ResizeObservers.
onUpdated(() => {
  rowVirtualizer.value.measureElement(null)
  completePendingFocus()
})
onBeforeUnmount(() => {
  disposed = true
  pendingFocus.value = null
  headerObserver?.disconnect()
})
watch(headerRef, (element, previous) => {
  if (previous) headerObserver?.unobserve(previous)
  if (element) {
    headerObserver?.observe(element)
    const height = element.getBoundingClientRect().height
    if (height) headerHeight.value = height
  }
}, { flush: 'post' })
watch(scrollRef, async (element, previous) => {
  if (previous) {
    headerObserver?.unobserve(previous)
    const table = previous.querySelector('.matrix-table')
    if (table) headerObserver?.unobserve(table)
  }
  if (!element) return
  headerObserver?.observe(element)
  const table = element.querySelector('.matrix-table')
  if (table) headerObserver?.observe(table)
  await nextTick()
  element.scrollTop = props.viewState?.scrollTop || 0
  element.scrollLeft = props.viewState?.scrollLeft || 0
  rowVirtualizer.value.scrollToOffset(element.scrollTop)
  clampScroll()
  syncHeaderGeometry()
  syncHorizontalScroll(element, headerRailRef.value)
}, { flush: 'post' })
watch(viewportWidth, async (width, previous) => {
  if (width === previous) return
  rowVirtualizer.value.measure()
  await nextTick()
  // Clearing the cache can follow the same ResizeObserver delivery that measured
  // the new layout. Read mounted rows again; a second resize event is not guaranteed.
  scrollRef.value?.querySelectorAll<HTMLElement>('.matrix-data-row').forEach((element) => {
    const height = element.getBoundingClientRect().height
    // measureElement defers synchronous reads during user scrolling, but layout
    // changes must replace the cleared estimates even if scrolling is still active.
    if (height > 0) rowVirtualizer.value.resizeItem(Number(element.dataset.index), height)
  })
  await nextTick()
  clampScroll()
})
watch(rowKeys, async (keys, previous) => {
  const revision = ++rowsRevision
  hideTooltip()
  const pending = pendingFocus.value
  if (pending) {
    const index = keys.indexOf(pending.key)
    pendingFocus.value = index < 0 ? null : { ...pending, row: index }
  }
  const focusedKey = focusedRow.value == null ? undefined : previous?.[focusedRow.value]
  const hadFocus = scrollRef.value?.contains(document.activeElement)
  if (focusedRow.value != null) {
    const index = focusedKey ? keys.indexOf(focusedKey) : -1
    focusedRow.value = index >= 0 ? index : keys.length ? Math.min(focusedRow.value, keys.length - 1) : null
  }
  const retainedFocusedRow = focusedRow.value
  await nextTick()
  if (disposed || revision !== rowsRevision) return
  clampScroll()
  if (hadFocus && retainedFocusedRow != null && !scrollRef.value?.contains(document.activeElement)) {
    await focusCell(retainedFocusedRow, focusedColumn)
  }
})
const zoomed = computed(() => isZoomed(zoom.value))

const allBucketStarts = computed(() => {
  // X-axis always spans the UI-selected range [requested_start, requested_end).
  // Partial backfill leaves empty cells until coverage_start/data_through fill in.
  const step = Math.max(60, props.coverage.bucket_seconds) * 1000
  const requestedStart = new Date(props.coverage.requested_start).getTime()
  const requestedEndRaw = props.coverage.requested_end
    ? new Date(props.coverage.requested_end).getTime()
    : NaN
  // Fallback for older payloads without requested_end.
  const dataThrough = new Date(props.coverage.data_through).getTime()
  const end = Number.isFinite(requestedEndRaw) && requestedEndRaw > requestedStart
    ? requestedEndRaw
    : dataThrough
  if (![requestedStart, end].every(Number.isFinite) || requestedStart >= end) return []
  const starts: string[] = []
  for (let cursor = Math.floor(requestedStart / step) * step; cursor < end; cursor += step) {
    starts.push(new Date(cursor).toISOString())
  }
  return starts
})
/** Visible bucket window after X zoom (cursor-centered), not always the tail. */
const bucketStarts = computed(() => sliceByZoom(allBucketStarts.value, zoom.value))
watch(bucketStarts, async (starts, previous) => {
  const revision = ++bucketsRevision
  const pending = pendingFocus.value
  if (pending) {
    const index = starts.indexOf(pending.start)
    const column = index >= 0 ? index : Math.min(pending.column, Math.max(0, starts.length - 1))
    pendingFocus.value = starts.length ? { ...pending, column, start: starts[column] } : null
  }
  if (focusedRow.value == null || !scrollRef.value?.contains(document.activeElement)) return
  const key = rowKeys.value[focusedRow.value]
  const start = previous[focusedColumn]
  const column = starts.indexOf(start)
  focusedColumn = column >= 0 ? column : Math.min(focusedColumn, Math.max(0, starts.length - 1))
  await nextTick()
  if (disposed || revision !== bucketsRevision) return
  const row = rowKeys.value.indexOf(key)
  if (row >= 0 && !pendingFocus.value && !scrollRef.value?.contains(document.activeElement)) await focusCell(row, focusedColumn)
})
const tableStyle = computed(() => ({
  '--bucket-count': String(Math.max(1, bucketStarts.value.length)),
  minWidth: zoomed.value ? `calc(260px + ${pulseMinWidth.value})` : '0',
}))
const headerStyle = computed(() => ({
  ...tableStyle.value,
  ...(headerWidth.value == null ? {} : { width: `${headerWidth.value}px` }),
}))
const pulseMinWidth = computed(() => {
  const count = Math.max(1, bucketStarts.value.length)
  if (!zoomed.value) return '0px'
  // Zoom in = fewer columns + wider min cell (span shrinks → intensity grows).
  const intensity = Math.min(12, Math.round((1 - zoom.value.span) / 0.08))
  const width = 6 + intensity * 4
  const gap = intensity >= 4 ? 3 : 2
  return `${count * width + Math.max(0, count - 1) * gap}px`
})
const pulseStyle = computed(() => {
  const count = Math.max(1, bucketStarts.value.length)
  const intensity = zoomed.value ? Math.min(12, Math.round((1 - zoom.value.span) / 0.08)) : 0
  const gapPx = !zoomed.value ? (count > 24 ? 1 : 2) : intensity >= 4 ? 3 : 2
  const heightPx = 16
  // Unzoomed: equal flex fractions. Zoomed: enforce growing min width so blocks lengthen.
  const minCell = !zoomed.value ? '0' : `${6 + intensity * 4}px`
  return {
    gridTemplateColumns: `repeat(${count}, minmax(${minCell}, 1fr))`,
    gap: `${gapPx}px`,
    height: `${heightPx}px`,
    minWidth: pulseMinWidth.value,
  }
})
const axisStart = computed(() =>
  bucketStarts.value.length ? formatAxisTime(bucketStarts.value[0]) : '时间脉冲'
)
const axisEnd = computed(() =>
  bucketStarts.value.length ? formatAxisTime(bucketStarts.value[bucketStarts.value.length - 1]) : ''
)
const bucketLabel = computed(() => {
  const minutes = props.coverage.bucket_seconds / 60
  if (minutes < 60) return t('channelMonitorV2.bucket.minutes', { count: minutes })
  const hours = minutes / 60
  if (hours < 24) return t('channelMonitorV2.bucket.hours', { count: hours })
  return t('channelMonitorV2.bucket.days', { count: hours / 24 })
})

/** Shared ISO start → column index for the visible window (rebuilt when zoom/coverage changes). */
const bucketStartIndex = computed(() => {
  const map = new Map<string, number>()
  bucketStarts.value.forEach((start, index) => map.set(start, index))
  return map
})

/** Align only mounted rows; a refresh never constructs thousands of cell subtrees. */
const visibleRows = computed(() => rowVirtualizer.value.getVirtualItems().map((item) => {
  const row = props.rows[item.index]
  const starts = bucketStarts.value
  const slots: AlignedSlot[] = starts.map((start) => ({ start }))
  for (const bucket of row.buckets || []) {
    const index = bucketStartIndex.value.get(new Date(bucket.bucket_start).toISOString())
    if (index != null) slots[index] = { start: starts[index], bucket }
  }
  return { row, slots, key: rowKeys.value[item.index], index: item.index, start: item.start }
}))

function onMatrixWheel(event: WheelEvent) {
  // Ordinary wheel input belongs to the scroll viewport. Ctrl explicitly zooms time.
  if (!event.ctrlKey) return
  event.preventDefault()
  const pulse = (event.target as HTMLElement | null)?.closest<HTMLElement>('.pulse-track')
  const next = applyWheelZoom(zoom.value, event, clientXRatio(event.clientX, pulse || scrollRef.value))
  if (next === zoom.value) return
  hideTooltip()
  zoom.value = next
  saveViewport()
}
function resetMatrixZoom() {
  if (!zoomed.value) return
  zoom.value = resetZoom()
  hideTooltip()
  saveViewport()
}
watch(domainKey, (key, previous) => {
  if (key !== (previous || props.viewState?.domainKey)) zoom.value = resetZoom()
  hideTooltip()
  saveViewport()
}, { immediate: true })

function completePendingFocus() {
  const pending = pendingFocus.value
  if (disposed || !pending) return
  if (rowKeys.value[pending.row] !== pending.key || bucketStarts.value[pending.column] !== pending.start) {
    pendingFocus.value = null
    return
  }
  const cell = scrollRef.value?.querySelector<HTMLElement>(`[data-index="${pending.row}"] [data-column="${pending.column}"]`)
  if (!cell) return
  cell.focus({ preventScroll: true })
  if (document.activeElement !== cell) return
  focusedRow.value = pending.row
  focusedColumn = pending.column
  pendingFocus.value = null
  // Transfer focus before moving the scroll viewport; the source stays mounted
  // until this point, and the destination stays pinned through delayed scrolls.
  rowVirtualizer.value.scrollToIndex(pending.row, { align: 'auto' })
}
async function focusCell(row: number, column: number) {
  if (disposed || !props.rows.length || !bucketStarts.value.length) return
  const nextRow = Math.max(0, Math.min(row, props.rows.length - 1))
  const nextColumn = Math.max(0, Math.min(column, bucketStarts.value.length - 1))
  pendingFocus.value = { row: nextRow, column: nextColumn, key: rowKeys.value[nextRow], start: bucketStarts.value[nextColumn] }
  await nextTick()
  // The update hook also completes this request if the virtualizer needs a later
  // Vue patch. No synchronous scroll event or fixed frame count is required.
  completePendingFocus()
}
function onCellFocus(event: FocusEvent, slot: AlignedSlot, row: number, column: number) {
  focusedRow.value = row
  focusedColumn = column
  showTooltip(event, slot)
}
function onCellKeydown(event: KeyboardEvent, row: number, column: number) {
  const columns = bucketStarts.value.length
  let nextRow = row
  let nextColumn = column
  if (event.key === 'ArrowDown') nextRow++
  else if (event.key === 'ArrowUp') nextRow--
  else if (event.key === 'ArrowRight') nextColumn++
  else if (event.key === 'ArrowLeft') nextColumn--
  else if (event.key === 'Home') {
    nextColumn = 0
    if (event.ctrlKey) nextRow = 0
  } else if (event.key === 'End') {
    nextColumn = columns - 1
    if (event.ctrlKey) nextRow = props.rows.length - 1
  } else if (event.key === 'PageDown' || event.key === 'PageUp') {
    const page = Math.max(1, Math.floor((scrollRef.value?.clientHeight || 360) / (viewportWidth.value <= 640 ? 48 : 36)) - 1)
    nextRow += event.key === 'PageDown' ? page : -page
  } else if (event.key === 'Tab') {
    const flat = row * columns + column + (event.shiftKey ? -1 : 1)
    // At the actual matrix boundaries native Tab continues to the surrounding UI.
    if (flat < 0 || flat >= props.rows.length * columns) {
      focusedRow.value = null
      return
    }
    nextRow = Math.floor(flat / columns)
    nextColumn = flat % columns
  } else if (event.key === 'Escape') {
    hideTooltip()
    return
  } else return
  event.preventDefault()
  void focusCell(Math.max(0, Math.min(nextRow, props.rows.length - 1)), Math.max(0, Math.min(nextColumn, columns - 1)))
}

function cellClass(health: MonitorHealth, requestCount: number): string {
  return healthScoreClass(health, props.healthMode, requestCount)
}

function rowLabel(row: MonitorMatrixRow): string {
  const parts = [row.platform]
  if (row.group_name || row.group_id) parts.push(row.group_name || `#${row.group_id}`)
  if (row.model) parts.push(row.model === '__other__' ? t('channelMonitorV2.otherModels') : row.model)
  return parts.join(' / ')
}

function rowKey(row: MonitorMatrixRow): string {
  return [row.platform, row.group_id || 0, row.model || ''].join(':')
}

function successRate(metrics: MonitorMetric): string {
  // Empty traffic: no request count and no throughput signal.
  // When throughput is hidden for privacy, still show success from error_rate.
  const noCount = metrics.request_count <= 0
  const noTP = (metrics.rpm || 0) <= 0 && (metrics.tpm || 0) <= 0
  if (noCount && noTP && props.showThroughput) return '-'
  return formatPercent(1 - (metrics.error_rate || 0))
}

function formatScore(health: MonitorHealth): string {
  const score = healthModeScore(health, props.healthMode)
  if (score == null) return '—'
  return `${Math.round(score)}`
}

function bucketTooltip(bucket: MonitorMatrixBucket): string {
  return bucketTooltipLines(bucket).join('\n')
}

function bucketTooltipLines(bucket: MonitorMatrixBucket): string[] {
  const metrics = bucket.metrics
  const lines = [
    formatBucketRange(bucket.bucket_start),
    t('channelMonitorV2.matrix.scoreLine', { score: formatScore(bucket.health) }),
    t('channelMonitorV2.metrics.successRateValue', { value: successRate(metrics) }),
    t('channelMonitorV2.metrics.ttftValue', { value: latencyPrivacy(metrics.ttft) }),
  ]
  if (props.showThroughput) {
    lines.push(t('channelMonitorV2.metrics.tpsValue', { value: formatTps(metrics.tpm) }))
  }
  lines.push(
    t('channelMonitorV2.metrics.cacheRateValue', { value: formatPercent(metrics.cache_rate) }),
    t('channelMonitorV2.metrics.errorRateValue', { value: formatPercent(metrics.error_rate) }),
  )
  if (props.showThroughput) {
    lines.push(t('channelMonitorV2.metrics.rpmValue', { value: formatRate(metrics.rpm) }))
  }
  lines.push(t('channelMonitorV2.metrics.durationValue', { value: latencyPrivacy(metrics.duration) }))
  return lines
}
function emptyTooltipLines(start: string): string[] {
  return [formatBucketRange(start), t('channelMonitorV2.matrix.noTraffic')]
}

function showTooltip(event: MouseEvent | FocusEvent, slot: AlignedSlot) {
  tooltipRef.value?.show(event, slot.bucket ? bucketTooltipLines(slot.bucket) : emptyTooltipLines(slot.start))
}
function moveTooltip(event: MouseEvent) {
  tooltipRef.value?.move(event)
}
function hideTooltip() {
  tooltipRef.value?.hide()
}

function latencyPrivacy(metric: LatencyMetric) {
  return formatLatencyPrivacy(metric.p50_ms, metric.p90_ms, metric.avg_ms, metric.p95_ms)
}

function formatPercent(value: number) {
  return `${percentFormatters.value[value < 0.01 ? 1 : 0].format((value || 0) * 100)}%`
}

function formatRate(value: number) {
  if (value == null || Number.isNaN(Number(value))) return '0'
  return Math.abs(value) < 1000 ? rateFormatter.value.format(value) : formatCompactNumber(value)
}

function formatTps(tpm: number | null | undefined) {
  return formatRate(tokensPerSecondFromTpm(tpm))
}

function exactTps(tpm: number | null | undefined) {
  const tps = tokensPerSecondFromTpm(tpm)
  return numberFormatter.value.format(tps)
}

function formatMs(value: number | null) {
  return formatMonitorMs(value)
}

const displayLocale = computed(() => { void locale.value; return monitorIntlLocale() })
const percentFormatters = computed(() => [1, 2].map((digits) => new Intl.NumberFormat(displayLocale.value, {
  minimumFractionDigits: digits, maximumFractionDigits: digits,
})))
const rateFormatter = computed(() => new Intl.NumberFormat(displayLocale.value, { maximumFractionDigits: 1 }))
// Construct formatters once per locale and ranges once per time window.
const axisFormatter = computed(() => new Intl.DateTimeFormat(locale.value || undefined, {
  month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
}))
const timeFormatter = computed(() => new Intl.DateTimeFormat(locale.value || undefined, { hour: '2-digit', minute: '2-digit' }))
const numberFormatter = computed(() => new Intl.NumberFormat(locale.value || undefined, { maximumFractionDigits: 3 }))
const bucketRanges = computed(() => new Map(allBucketStarts.value.map((start) => [start,
  `${formatAxisTime(start)} - ${timeFormatter.value.format(new Date(new Date(start).getTime() + props.coverage.bucket_seconds * 1000))}`,
])))
function formatAxisTime(value: string) {
  return axisFormatter.value.format(new Date(value))
}
function formatBucketRange(value: string) {
  return bucketRanges.value.get(new Date(value).toISOString()) || ''
}
</script>

<style scoped>
/* dimension | success | ttft | cache | pulse */
.matrix-row {
  display: grid;
  grid-template-columns:
    minmax(120px, 1.2fr)
    minmax(52px, 0.34fr)
    minmax(58px, 0.36fr)
    minmax(52px, 0.34fr)
    minmax(120px, 2.8fr);
  align-items: center;
  gap: 0.5rem clamp(0.25rem, 0.8vw, 0.625rem);
  min-height: 2.25rem;
}
/* + tokens/s column when throughput visible */
.matrix-row--with-tps {
  grid-template-columns:
    minmax(110px, 1.15fr)
    minmax(48px, 0.3fr)
    minmax(54px, 0.32fr)
    minmax(58px, 0.36fr)
    minmax(48px, 0.3fr)
    minmax(120px, 2.6fr);
}
.matrix-table,
.pulse-track {
  min-width: 0;
}
.matrix-frame {
  overflow: hidden;
}
.matrix-header-rail {
  overflow: hidden;
  min-width: 0;
}
.matrix-scroll {
  min-height: 0;
  scrollbar-gutter: stable;
}
.status-dot {
  display: inline-block;
  height: 0.5rem;
  width: 0.5rem;
  flex: none;
  border-radius: 9999px;
}

/* Multi-stop green → yellow → red (score10 best … score0 worst) */
.health-score10 { background: #16a34a; }
.health-score9  { background: #22c55e; }
.health-score8  { background: #4ade80; }
.health-score7  { background: #a3e635; }
.health-score6  { background: #facc15; }
.health-score5  { background: #fbbf24; }
.health-score4  { background: #f59e0b; }
.health-score3  { background: #f97316; }
.health-score2  { background: #fb7185; }
.health-score1  { background: #f87171; }
.health-score0  { background: rgb(239, 67, 67); }
/* Coarse fallbacks (older payloads without score) */
.health-healthy  { background: #22c55e; }
.health-warning  { background: #f59e0b; }
.health-critical { background: #ef4444; }
.health-unknown  { background: #9ca3af; }

.score-legend {
  background: linear-gradient(
    90deg,
    rgb(239, 67, 67) 0%,
    #f87171 15%,
    #f97316 30%,
    #f59e0b 45%,
    #facc15 55%,
    #a3e635 70%,
    #22c55e 85%,
    #16a34a 100%
  );
}

.pulse-cell {
  position: relative;
  min-width: 0;
}
.pulse-cell.has-data {
  cursor: help;
}
.pulse-cell.is-empty {
  opacity: 0.55;
  cursor: default;
}
.pulse-cell.has-data:hover,
.pulse-cell.has-data:focus-visible {
  outline: 2px solid rgb(var(--console-accent-500, 20 184 166) / 0.55);
  outline-offset: 1px;
  z-index: 5;
}

@media (max-width: 640px) {
  .matrix-row {
    grid-template-columns: minmax(88px, 1fr) minmax(48px, 0.45fr) minmax(54px, 0.5fr) minmax(96px, 2.6fr);
    gap: 0.35rem;
  }
  .matrix-row > :nth-child(2) {
    left: 0;
  }
  .matrix-row > :nth-child(3) {
    left: 0;
  }
}
</style>
