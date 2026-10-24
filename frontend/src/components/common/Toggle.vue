<template>
  <button
    type="button"
    @click="toggle"
    :disabled="disabled"
    :aria-disabled="disabled"
    class="toggle-track relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-800"
    :class="[modelValue ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600']"
    role="switch"
    :aria-checked="modelValue"
  >
    <span
      class="pointer-events-none inline-block h-5 w-5 transform rounded-full surface-opaque shadow ring-0 transition duration-200 ease-in-out"
      :class="[modelValue ? 'translate-x-5' : 'translate-x-0']"
    />
  </button>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<style scoped>
.toggle-track { border: 1px solid var(--surface-edge); padding: 1px; }
.toggle-track[aria-checked="true"] { border-color: rgb(var(--console-accent-500, 20 184 166) / 65%); }
</style>
