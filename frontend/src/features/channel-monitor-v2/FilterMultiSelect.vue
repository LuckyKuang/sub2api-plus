<template>
  <div :class="compact ? 'min-w-[6.5rem] sm:min-w-[7.25rem]' : 'min-w-[150px] sm:min-w-[160px]'">
    <select :value="''" class="input" :class="compact ? 'h-8 rounded-lg py-1 text-xs' : ''" :aria-label="label" @change="choose">
      <option value="" disabled>{{ t('channelMonitorV2.filters.labelValue', { label, value: selectionLabel }) }}</option>
      <option value="all">{{ allLabel }}</option>
      <option v-for="option in options" :key="option.value" :value="'option:' + option.value">
        {{ modelValue.includes(option.value) ? '✓ ' : '' }}{{ option.label }}{{ option.count == null ? '' : ' · ' + formatCount(option.count) }}
      </option>
    </select>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed } from 'vue'
import { monitorIntlLocale } from '@/features/channel-monitor-v2/monitorFormat'

interface FilterOption { value: string; label: string; count?: number }
const props = withDefaults(defineProps<{
  label: string
  allLabel: string
  modelValue: string[]
  options: FilterOption[]
  compact?: boolean
}>(), { compact: false })
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { t, locale } = useI18n()
const selectionLabel = computed(() => {
  if (props.modelValue.length === 0) return props.allLabel
  if (props.modelValue.length === 1) return props.options.find(item => item.value === props.modelValue[0])?.label || props.modelValue[0]
  return t('channelMonitorV2.filters.selectedCount', { count: props.modelValue.length })
})
function choose(event: Event): void {
  const select = event.target as HTMLSelectElement
  if (select.value === 'all') emit('update:modelValue', [])
  else if (select.value.startsWith('option:')) {
    const value = select.value.slice(7)
    const selected = new Set(props.modelValue)
    if (selected.has(value)) selected.delete(value)
    else selected.add(value)
    emit('update:modelValue', [...selected])
  }
  select.value = ''
}
function formatCount(value: number): string {
  return Intl.NumberFormat(locale.value || monitorIntlLocale(), { notation: value >= 10000 ? 'compact' : 'standard' }).format(value)
}
</script>
