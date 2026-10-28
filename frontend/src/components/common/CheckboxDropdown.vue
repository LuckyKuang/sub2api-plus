<template>
  <div>
    <button ref="trigger" type="button" class="select-trigger flex items-center gap-2" :class="open && 'select-trigger-open'" :aria-label="label" :aria-expanded="open" :aria-controls="open ? panelID : undefined" :disabled="disabled" @click="toggleOpen" @keydown.down.prevent="openAndFocus()" @keydown.up.prevent="openAndFocus(true)" @keydown.esc.stop.prevent="close">
      <span class="min-w-0 flex-1 truncate text-left">{{ selectedLabel || label }}</span>
      <span class="shrink-0 surface-muted" aria-hidden="true">⌄</span>
    </button>
    <Teleport to="body">
      <div v-if="open" :id="panelID" ref="panel" class="select-dropdown-portal" :style="panelStyle" @keydown="onPanelKeydown">
        <div class="select-options">
          <label v-for="option in options" :key="option.value" class="select-option hover:surface-hover" :class="modelValue.includes(option.value) && 'select-option-selected'">
            <input class="shrink-0" type="checkbox" :value="option.value" :checked="modelValue.includes(option.value)" :disabled="disabled" @change="toggle(option.value, ($event.target as HTMLInputElement).checked)" />
            <span class="min-w-0 flex-1 break-words">{{ option.label }}</span>
          </label>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts" generic="T extends string | number">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import { getFloatingPanelPosition } from '@/utils/floatingPanel'
const props = defineProps<{ modelValue: T[]; options: { value: T; label: string }[]; label: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: T[]] }>()
const panelID = `checkbox-dropdown-${useId()}`
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLElement>()
const open = ref(false)
const rect = ref<DOMRect>()
function updatePosition() { if (open.value) rect.value = trigger.value?.getBoundingClientRect() }
useEventListener(window, 'resize', updatePosition)
useEventListener(window, 'scroll', updatePosition, { capture: true })
onClickOutside(trigger, () => { open.value = false }, { ignore: [panel] })
watch(() => props.disabled, disabled => { if (disabled) open.value = false })
function close() { open.value = false; trigger.value?.focus() }
function checkboxOptions() { return Array.from(panel.value?.querySelectorAll<HTMLInputElement>('input[type="checkbox"]:not(:disabled)') ?? []) }
async function openAndFocus(last = false) {
  if (props.disabled) return
  open.value = true
  updatePosition()
  await nextTick()
  if (!open.value) return
  const options = checkboxOptions()
  options[last ? options.length - 1 : 0]?.focus()
}
function toggleOpen() { if (open.value) close(); else void openAndFocus() }
function onPanelKeydown(event: KeyboardEvent) {
  if (event.isComposing) return
  const options = checkboxOptions()
  const index = options.indexOf(document.activeElement as HTMLInputElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close()
  } else if (event.key === 'Tab' && ((event.shiftKey && index === 0) || (!event.shiftKey && index === options.length - 1))) {
    // Restore the trigger's place in the form's tab order before the browser
    // advances focus; the Teleport panel lives at the end of the document.
    if (event.shiftKey) event.preventDefault()
    close()
  } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key) && options.length) {
    event.preventDefault()
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1
      : (index + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length
    options[next]?.focus()
  } else if (event.key === 'Enter' && index >= 0) {
    event.preventDefault()
    options[index]?.click()
  }
}
const panelStyle = computed(() => {
  if (!rect.value) return {}
  const position = getFloatingPanelPosition(rect.value, window.innerWidth, window.innerHeight, { maxWidth: rect.value.width, viewportPadding: 8, gap: 4 })
  return { position: 'fixed' as const, zIndex: 100000020, width: `${position.width}px`, minWidth: '0', left: `${position.left}px`, top: position.top == null ? undefined : `${position.top}px`, bottom: position.bottom == null ? undefined : `${position.bottom}px`, maxHeight: `${position.maxHeight}px` }
})
const selectedLabel = computed(() => props.options.filter(option => props.modelValue.includes(option.value)).map(option => option.label).join(', '))
function toggle(value: T, checked: boolean) {
  emit('update:modelValue', checked ? [...props.modelValue, value] : props.modelValue.filter(item => item !== value))
}
</script>
