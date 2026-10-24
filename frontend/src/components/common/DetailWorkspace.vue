<template>
  <div ref="root" class="detail-workspace" :class="{ 'detail-workspace-open': show && docked, 'detail-workspace-contained': workspace }">
    <div class="detail-workspace-content min-w-0"><slot /></div>
    <aside v-if="show && docked" ref="panel" tabindex="-1" class="detail-workspace-panel card focus:outline-none" :aria-label="title">
      <div class="detail-workspace-header flex items-center justify-between gap-3 border-b border-gray-200 p-4 dark:border-dark-700">
        <h2 class="font-semibold">{{ title }}</h2>
        <button type="button" class="btn btn-ghost btn-icon" :aria-label="t('inspection.close')" @click="emit('close')"><Icon name="x" size="md" /></button>
      </div>
      <div class="detail-workspace-detail min-h-0 overflow-y-auto overscroll-contain p-4"><slot name="detail" /></div>
    </aside>
    <BaseDialog :show="show && !docked" :title="title" placement="right" @close="emit('close')"><slot name="detail" /></BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
const props = defineProps<{ show: boolean; title: string; workspace?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const docked = ref(false)
const panel = ref<HTMLElement | null>(null)
let origin: HTMLElement | null = null
let observer: ResizeObserver | undefined
watch(() => props.show, async show => {
  if (show) origin = document.activeElement as HTMLElement
  else { await nextTick(); if (origin?.isConnected) origin.focus({ preventScroll: true }) }
})
watch([() => props.show, docked], async ([show, isDocked]) => {
  if (show && isDocked) { await nextTick(); panel.value?.focus({ preventScroll: true }) }
})
function onKey(event: KeyboardEvent) {
  if (props.show && docked.value && event.key === 'Escape' && !document.querySelector('[aria-modal="true"]')) emit('close')
}
onMounted(() => {
  document.addEventListener('keydown', onKey)
  if (!root.value || typeof ResizeObserver === 'undefined') return
  // Fits the 1,044px admin table + 440px detail + 24px gap, including card insets.
  observer = new ResizeObserver(([entry]) => { docked.value = entry.contentRect.width >= 1520 })
  observer.observe(root.value)
})
onBeforeUnmount(() => { observer?.disconnect(); document.removeEventListener('keydown', onKey) })
</script>

<style scoped>
.detail-workspace { display: grid; min-width: 0; gap: var(--shell-gap, 24px); align-items: start; }
.detail-workspace-open { grid-template-columns: minmax(0, 1fr) 440px; }
.detail-workspace-panel { position: sticky; top: var(--shell-sticky-offset, 112px); max-height: calc(100dvh - var(--shell-sticky-offset, 112px) - var(--shell-inset-bottom, 24px)); display: flex; flex-direction: column; overflow: hidden; }
@media (min-width: 1024px) {
  .detail-workspace-contained { flex: 1 1 0%; min-height: 0; grid-template-rows: minmax(0, 1fr); align-items: stretch; gap: 12px; }
  .detail-workspace-contained > .detail-workspace-content { display: flex; min-height: 0; flex-direction: column; }
  .detail-workspace-contained > .detail-workspace-panel { position: relative; top: auto; min-height: 0; max-height: 100%; }
  .detail-workspace-contained .detail-workspace-header { flex: none; }
  .detail-workspace-contained .detail-workspace-detail { flex: 1 1 0%; }
}
</style>
