<template>
  <div ref="navigation" class="card workspace-tabs workspace-fixed" role="tablist" :aria-label="label">
    <button
      v-for="tab in tabs"
      :key="tab.value"
      type="button"
      role="tab"
      class="workspace-tab"
      :aria-selected="modelValue === tab.value"
      :aria-controls="tab.panelId"
      :disabled="tab.disabled"
      :tabindex="modelValue === tab.value ? 0 : -1"
      @click="emit('update:modelValue', tab.value)"
      @keydown="navigate($event, tab.value)"
    >{{ tab.label }}</button>
  </div>
</template>

<script setup lang="ts" generic="T extends string">
import { nextTick, ref } from 'vue'

const props = defineProps<{
  modelValue: T
  tabs: { value: T; label: string; panelId?: string; disabled?: boolean }[]
  label: string
}>()
const emit = defineEmits<{ 'update:modelValue': [value: T] }>()
const navigation = ref<HTMLElement | null>(null)

async function navigate(event: KeyboardEvent, value: T) {
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const enabled = props.tabs.filter(tab => !tab.disabled)
  const index = enabled.findIndex(tab => tab.value === value)
  let next: number
  switch (event.key) {
    case 'ArrowRight': next = (index + 1) % enabled.length; break
    case 'ArrowLeft': next = (index - 1 + enabled.length) % enabled.length; break
    case 'Home': next = 0; break
    case 'End': next = enabled.length - 1; break
    default: return
  }
  if (!enabled.length) return
  event.preventDefault()
  emit('update:modelValue', enabled[next].value)
  await nextTick()
  navigation.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')[next]?.focus()
}
</script>
