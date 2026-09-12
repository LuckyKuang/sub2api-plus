<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
import { QUOTA_THRESHOLD_TYPE_FIXED, QUOTA_THRESHOLD_TYPE_PERCENTAGE, type QuotaThresholdType } from '@/constants/account'

defineProps<{
  enabled: boolean | null
  threshold: number | null
  thresholdType: QuotaThresholdType | null
}>()

const emit = defineEmits<{
  'update:enabled': [value: boolean | null]
  'update:threshold': [value: number | null]
  'update:thresholdType': [value: QuotaThresholdType | null]
}>()
</script>

<template>
  <div class="flex items-center gap-1.5">
    <Toggle
      :model-value="!!enabled"
      @update:model-value="emit('update:enabled', $event)"
    />
    <template v-if="enabled">
      <input
        :value="threshold"
        @input="emit('update:threshold', parseFloat(($event.target as HTMLInputElement).value) || null)"
        type="number"
        min="0"
        :max="thresholdType === QUOTA_THRESHOLD_TYPE_PERCENTAGE ? 100 : undefined"
        :step="thresholdType === QUOTA_THRESHOLD_TYPE_PERCENTAGE ? 1 : 0.01"
        class="input py-1 text-sm flex-1 min-w-0"
      />
      <select
        :value="thresholdType || QUOTA_THRESHOLD_TYPE_FIXED"
        @change="emit('update:thresholdType', ($event.target as HTMLSelectElement).value as QuotaThresholdType)"
        class="input py-1 text-xs w-[4.5rem] flex-shrink-0 text-center"
      >
        <option :value="QUOTA_THRESHOLD_TYPE_FIXED">$</option>
        <option :value="QUOTA_THRESHOLD_TYPE_PERCENTAGE">%</option>
      </select>
    </template>
  </div>
</template>
