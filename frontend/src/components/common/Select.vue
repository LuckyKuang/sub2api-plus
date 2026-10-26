<template>
  <SearchableSelect
    v-bind="props"
    :searchable="isSearchable"
    @update:model-value="emit('update:modelValue', $event)"
    @change="(value, option) => emit('change', value, option)"
    @search="emit('search', $event)"
  >
    <template v-if="$slots.selected" #selected="{ option }">
      <slot name="selected" :option="option" />
    </template>
    <template v-if="$slots.option" #option="{ option, selected }">
      <slot name="option" :option="option" :selected="selected" />
    </template>
  </SearchableSelect>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SearchableSelect from './SearchableSelect.vue'

export interface SelectOption {
  value: string | number | boolean | null
  label: string
  disabled?: boolean
  [key: string]: unknown
}
type Option = SelectOption | Record<string, unknown>
type Value = SelectOption['value']
interface Props {
  modelValue: Value | undefined
  options: Option[]
  placeholder?: string
  disabled?: boolean
  error?: boolean
  searchable?: boolean | 'auto'
  searchPlaceholder?: string
  emptyText?: string
  valueKey?: string
  labelKey?: string
  creatable?: boolean
  creatablePrefix?: string
  clearable?: boolean
  id?: string
  ariaLabel?: string
  ariaDescribedby?: string
  remote?: boolean
  loading?: boolean
  triggerClass?: string
}
const props = withDefaults(defineProps<Props>(), {
  disabled: false, error: false, searchable: 'auto', creatable: false,
  creatablePrefix: '', clearable: false, valueKey: 'value', labelKey: 'label',
  remote: false, loading: false,
})
const emit = defineEmits<{
  'update:modelValue': [value: Value]
  change: [value: Value, option: Option | null]
  search: [query: string]
}>()
const isSearchable = computed(() => props.remote || props.creatable || props.searchable === true || (props.searchable === 'auto' && props.options.length > 5))
</script>
