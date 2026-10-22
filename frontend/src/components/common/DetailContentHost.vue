<template>
  <div v-if="embedded && show" class="min-w-0"><slot /><slot name="footer" /></div>
  <BaseDialog v-else-if="!embedded" v-bind="$attrs" :show="show" :title="title" @close="$emit('close')">
    <slot />
    <template v-if="$slots.footer" #footer><slot name="footer" /></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import BaseDialog from './BaseDialog.vue'
defineOptions({ inheritAttrs: false })
defineProps<{ embedded?: boolean; show: boolean; title: string }>()
defineEmits<{ close: [] }>()
</script>
