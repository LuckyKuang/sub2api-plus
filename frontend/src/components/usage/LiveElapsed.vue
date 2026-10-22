<template><span class="tabular-nums">{{ seconds }} s</span></template>
<script setup lang="ts">
import { computed, type Ref } from 'vue'
import type { LiveRequest } from '@/api/liveRequests'
const props = defineProps<{
  row: LiveRequest
  clock: { now: Ref<number>; receivedAt: Ref<number>; serverNow: Ref<number>; healthy: Readonly<Ref<boolean>> }
}>()
// Only active cells subscribe to the shared display clock, not the entire table.
const display = computed<{ id: string; elapsed: number }>(previous => {
  const { row, clock } = props
  let elapsed = Math.max(0, row.elapsed_ms)
  if (row.status === 'processing' || row.status === 'transmitting' || row.status === 'unknown') {
    if (previous?.id === row.id) elapsed = Math.max(elapsed, previous.elapsed)
    if ((row.status === 'processing' || row.status === 'transmitting') && clock.healthy.value && clock.now.value - clock.receivedAt.value < 15000) {
      elapsed = Math.max(elapsed, clock.serverNow.value - Date.parse(row.started_at) + clock.now.value - clock.receivedAt.value)
    }
  }
  return { id: row.id, elapsed }
})
const seconds = computed(() => (display.value.elapsed / 1000).toFixed(1))
</script>
