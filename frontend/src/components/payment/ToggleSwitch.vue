<template>
  <label class="flex flex-col items-center gap-0.5 cursor-pointer">
    <span class="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">{{ label }}</span>
    <Toggle
      :model-value="!!checked" @update:model-value="emit('toggle')"
      :aria-label="label"
    />
  </label>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
defineProps<{ label: string; checked: boolean }>()
const emit = defineEmits<{ toggle: [] }>()
</script>
