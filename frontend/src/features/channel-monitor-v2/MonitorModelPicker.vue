<template>
  <Teleport to="body">
    <div
      v-if="open"
      ref="panel"
      class="monitor-model-picker card"
      :style="position"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
      data-testid="model-picker"
    >
      <div class="flex items-center justify-between gap-3">
        <h4 class="text-sm font-semibold">{{ title }}</h4>
        <button type="button" class="btn btn-ghost btn-sm" :aria-label="t('common.close')" @click="close"><Icon name="x" size="sm" /></button>
      </div>
      <div class="tabs mt-3" role="group" :aria-label="t('channelMonitorV2.settings.picker.inputMode')">
        <button type="button" class="tab" :class="mode === 'catalog' && 'tab-active'" :aria-pressed="mode === 'catalog'" @click="mode = 'catalog'">{{ t('channelMonitorV2.settings.picker.candidates') }}</button>
        <button type="button" class="tab" :class="mode === 'bulk' && 'tab-active'" :aria-pressed="mode === 'bulk'" @click="mode = 'bulk'">{{ t('channelMonitorV2.settings.picker.bulk') }}</button>
      </div>
      <template v-if="mode === 'catalog'">
        <input v-model="query" class="input mt-3" type="search" :aria-label="t('channelMonitorV2.settings.picker.search')" :placeholder="t('channelMonitorV2.settings.picker.search')" />
        <div class="monitor-picker-options mt-2">
          <label v-for="candidate in filtered" :key="candidate.model" class="flex cursor-pointer items-center gap-3 rounded-xl p-3 hover:surface-hover">
            <input type="checkbox" class="rounded" :checked="selected.includes(candidate.model)" @change="toggle(candidate.model)" />
            <span class="min-w-0 flex-1 break-all text-sm">{{ candidate.model }}</span>
            <span class="badge badge-gray">{{ t(`channelMonitorV2.settings.picker.${candidate.source}`) }}</span>
          </label>
          <p v-if="!filtered.length" class="p-6 text-center text-sm surface-muted">{{ t('channelMonitorV2.settings.picker.empty') }}</p>
        </div>
        <p class="mt-2 text-xs surface-muted">{{ t('channelMonitorV2.settings.picker.candidateHint') }}</p>
      </template>
      <template v-else>
        <p class="my-3 text-xs surface-muted">{{ t('channelMonitorV2.settings.picker.bulkHint') }}</p>
        <textarea v-model="bulk" class="input min-h-36 resize-y" :aria-label="t('channelMonitorV2.settings.picker.bulkLabel')" :placeholder="t('channelMonitorV2.settings.picker.bulkPlaceholder')" />
        <p class="mt-2 text-xs surface-muted">{{ t('channelMonitorV2.settings.picker.bulkCount', { count: pending.length }) }}</p>
      </template>
      <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-3 dark:border-dark-700">
        <span class="text-xs surface-muted">{{ t('channelMonitorV2.settings.picker.pending', { count: pending.length }) }}</span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-ghost btn-sm" @click="close">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="!pending.length" data-testid="picker-apply" @click="apply">{{ t('channelMonitorV2.settings.picker.apply', { count: pending.length }) }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useDialogFocus } from '@/composables/useDialogFocus'
import { parseModelIds } from './monitorModelSettings'

const props = defineProps<{
  open: boolean
  anchor: HTMLElement | null
  title: string
  candidates: Array<{ model: string; source: 'preset' | 'observed' | 'configured' | 'excluded' }>
  unavailable: string[]
}>()
const emit = defineEmits<{ close: []; apply: [models: string[]] }>()
const { t } = useI18n()
const panel = ref<HTMLElement | null>(null)
const mode = ref<'catalog' | 'bulk'>('catalog')
const query = ref('')
const bulk = ref('')
const selected = ref<string[]>([])
const position = ref({ left: '16px', top: '16px' })
let resizeObserver: ResizeObserver | undefined
const available = computed(() => props.candidates.filter(item => !props.unavailable.includes(item.model)))
const filtered = computed(() => available.value.filter(item => item.model.toLowerCase().includes(query.value.trim().toLowerCase())))
const pending = computed(() => (mode.value === 'bulk' ? parseModelIds(bulk.value) : selected.value).filter(model => !props.unavailable.includes(model)))
function toggle(model: string) {
  selected.value = selected.value.includes(model) ? selected.value.filter(value => value !== model) : [...selected.value, model]
}
function close() { emit('close') }
function apply() { if (pending.value.length) { emit('apply', [...pending.value]); close() } }
function reposition() {
  if (!props.open || !panel.value || !props.anchor) return
  const anchor = props.anchor.getBoundingClientRect()
  const rect = panel.value.getBoundingClientRect()
  const left = Math.max(16, Math.min(anchor.right - rect.width, window.innerWidth - rect.width - 16))
  const preferredTop = anchor.bottom + 8 + rect.height <= window.innerHeight - 16 ? anchor.bottom + 8 : anchor.top - rect.height - 8
  const top = Math.max(16, Math.min(preferredTop, window.innerHeight - rect.height - 16))
  const next = { left: `${left}px`, top: `${top}px` }
  if (next.left !== position.value.left || next.top !== position.value.top) position.value = next
}
function outside(event: PointerEvent) {
  if (props.open && event.target instanceof Node && !panel.value?.contains(event.target) && !props.anchor?.contains(event.target)) close()
}
watch(() => props.open, async open => {
  if (!open) return
  mode.value = 'catalog'; query.value = ''; bulk.value = ''; selected.value = []
  await nextTick(); reposition()
})
watch([mode, () => filtered.value.length], async () => { await nextTick(); reposition() })
watch(panel, element => {
  resizeObserver?.disconnect()
  if (!element || typeof ResizeObserver === 'undefined') return
  resizeObserver = new ResizeObserver(reposition)
  resizeObserver.observe(element)
})
useDialogFocus(() => props.open, panel, close, () => true, { lockScroll: false })
onMounted(() => {
  document.addEventListener('pointerdown', outside, true)
  window.addEventListener('resize', reposition)
  window.addEventListener('scroll', reposition, true)
})
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  document.removeEventListener('pointerdown', outside, true)
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
})
</script>

<style scoped>
.monitor-model-picker { position: fixed; z-index: 1100; width: min(440px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; padding: 18px; border-radius: 16px; box-shadow: 0 20px 60px rgb(0 0 0 / 18%); }
.monitor-picker-options { max-height: min(280px, 35dvh); overflow-y: auto; }
</style>
