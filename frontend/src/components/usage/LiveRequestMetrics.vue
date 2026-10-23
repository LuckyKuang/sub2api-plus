<template>
  <div class="space-y-0.5 text-xs tabular-nums">
    <div v-if="performanceColumn">{{ t('usage.latencyFirstToken') }} <span class="font-medium">{{ usageMilliseconds(liveFirstToken(row)) }}</span></div>
    <div v-else>{{ t('usage.latencyFirstByte') }} <span class="font-medium">{{ usageMilliseconds(row.first_byte_ms) }}</span></div>
    <div v-if="performanceColumn" class="font-medium text-cyan-700 dark:text-cyan-400">{{ liveRate(row) }}</div>
    <div v-else class="surface-muted">
      <template v-if="liveActive(row) || (row.status === 'unknown' && row.duration_ms == null)">{{ t('inspection.elapsed') }} <LiveElapsed :row="row" :clock="clock" /></template>
      <template v-else>{{ t('usage.latencyDuration') }} {{ usageMilliseconds(row.duration_ms) }}</template>
    </div>
    <button v-if="!performanceColumn" type="button" class="rounded text-primary-600 underline decoration-dotted underline-offset-2 surface-control" :aria-label="t('usage.latencyDetails')" :aria-expanded="open" @mouseenter="show" @mouseleave="open = false" @focus="show" @blur="open = false" @click="show" @keydown.escape="open = false">{{ t('usage.latencyDetails') }}</button>
  </div>
  <Teleport to="body">
    <div v-if="open" role="tooltip" class="pointer-events-none fixed z-[9999] w-64 max-w-[calc(100vw-24px)] rounded-xl border border-gray-700 p-3 text-xs text-white shadow-xl surface-panel" :style="{ left: position.x + 'px', top: position.y + 'px' }">
      <dl class="space-y-2"><div v-for="item in details" :key="item[0]" class="flex justify-between gap-3"><dt class="text-gray-400">{{ item[0] }}</dt><dd class="text-right tabular-nums">{{ item[1] }}</dd></div></dl>
    </div>
  </Teleport>
</template>
<script setup lang="ts">
import { computed, ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LiveRequest } from '@/api/liveRequests'
import { usageMilliseconds } from '@/utils/usageInspection'
import { liveActive, liveFirstToken, liveRate } from '@/utils/liveRequestPresentation'
import LiveElapsed from './LiveElapsed.vue'
const props = defineProps<{ row: LiveRequest; performanceColumn?: boolean; clock: { now: Ref<number>; receivedAt: Ref<number>; serverNow: Ref<number>; healthy: Readonly<Ref<boolean>> } }>()
const { t } = useI18n()
const open = ref(false)
const position = ref({ x: 0, y: 0 })
function show(event: Event) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  position.value = { x: Math.max(12, Math.min(rect.left, window.innerWidth - 268)), y: Math.max(12, Math.min(rect.bottom + 6, window.innerHeight - 300)) }
  open.value = true
}
const details = computed(() => [
  [t('usage.latencyFirstByte'), usageMilliseconds(props.row.first_byte_ms)],
  [t('usage.latencyFirstToken'), usageMilliseconds(liveFirstToken(props.row))],
  [t('usage.latencyLastToken'), usageMilliseconds(props.row.last_token_ms)],
  [t('usage.latencyFirstOutputKind'), props.row.first_output_kind ?? '—'],
  [t('requestList.firstOutput'), usageMilliseconds(props.row.first_output_ms)],
  [t('usage.latencyDuration'), usageMilliseconds(props.row.duration_ms)],
  [t('usage.latencyTps'), liveRate(props.row)],
])
</script>
