<template>
  <div class="flex items-center gap-1.5">
    <span
      :class="[
        'console-data-mark inline-block h-2 w-2 rounded-full',
        variantClass
      ]"
    ></span>
    <span class="text-sm surface-muted">
      {{ label }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  label: string
}>()

const variantClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'bg-green-500'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'bg-yellow-500'
    case 'error':
    case 'danger':
      return 'bg-red-500'
    default:
      return 'bg-gray-400'
  }
})
</script>
