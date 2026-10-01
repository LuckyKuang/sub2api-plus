<template>
  <SearchableSelect
    v-if="isSearchable"
    v-bind="props"
    :searchable="true"
    @update:model-value="emit('update:modelValue', $event)"
    @change="(value, option) => emit('change', value, option)"
    @search="emit('search', $event)"
  >
    <template v-if="$slots.selected || $slots.details" #selected="{ option }">
      <slot v-if="$slots.selected" name="selected" :option="option" />
      <slot v-else-if="option" name="details" :option="option" />
      <template v-else>{{ placeholderText }}</template>
    </template>
    <template v-if="$slots.option" #option="{ option, selected }">
      <slot name="option" :option="option" :selected="selected" />
    </template>
  </SearchableSelect>
  <div v-else class="native-select space-y-2">
    <select
      :id="id"
      :value="selectedKey"
      class="input"
      :class="error && 'input-error'"
      :disabled="disabled"
      :aria-label="ariaLabel ?? placeholderText"
      :aria-describedby="ariaDescribedby"
      :aria-invalid="error || undefined"
      @change="selectOption"
    >
      <option v-if="showPlaceholder" :value="placeholderKey" :disabled="!clearable">
        {{ loading ? t('common.loading') : options.length === 0 ? emptyTextDisplay : placeholderText }}
      </option>
      <template v-for="(entry, index) in entries" :key="index">
        <optgroup v-if="entry.header" :label="getOptionLabel(entry.header)">
          <option v-for="option in entry.options" :key="optionKey(option)" :value="optionKey(option)" :disabled="Boolean(option.disabled)">
            {{ getOptionLabel(option) }}
          </option>
        </optgroup>
        <template v-else>
          <option v-for="option in entry.options" :key="optionKey(option)" :value="optionKey(option)" :disabled="Boolean(option.disabled)">
            {{ getOptionLabel(option) }}
          </option>
        </template>
      </template>
    </select>
    <slot v-if="selectedOption" name="details" :option="selectedOption">
      <slot name="selected" :option="selectedOption" />
    </slot>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
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
}
const props = withDefaults(defineProps<Props>(), {
  disabled: false, error: false, searchable: false, creatable: false,
  creatablePrefix: '', clearable: false, valueKey: 'value', labelKey: 'label',
  remote: false, loading: false,
})
const emit = defineEmits<{
  'update:modelValue': [value: Value]
  change: [value: Value, option: Option | null]
  search: [query: string]
}>()
const { t } = useI18n()
const placeholderKey = 'placeholder'
const placeholderText = computed(() => props.placeholder ?? t('common.selectOption'))
const emptyTextDisplay = computed(() => props.emptyText ?? t('common.noOptionsFound'))
const isSearchable = computed(() => props.remote || props.creatable || props.searchable === true || (props.searchable === 'auto' && props.options.length > 5))
const getOptionValue = (option: Option): Value => (option[props.valueKey] ?? null) as Value
const getOptionLabel = (option: Option): string => String(option[props.labelKey] ?? '')
const encodeValue = (value: Value | undefined): string => `${typeof value}:${String(value)}`
const optionKey = (option: Option): string => encodeValue(getOptionValue(option))
const selectedOption = computed(() => props.options.find(option => option.kind !== 'group' && getOptionValue(option) === props.modelValue))
const selectedKey = computed(() => selectedOption.value ? encodeValue(props.modelValue) : placeholderKey)
const showPlaceholder = computed(() => props.clearable || !selectedOption.value)
const entries = computed(() => {
  const result: { header?: Option; options: Option[] }[] = []
  let group: typeof result[number] | undefined
  for (const option of props.options) {
    if (option.kind === 'group') {
      group = { header: option, options: [] }
      result.push(group)
    } else if (group) {
      group.options.push(option)
    } else {
      result.push({ options: [option] })
    }
  }
  return result
})
function selectOption(event: Event): void {
  if (props.disabled) return
  const key = (event.target as HTMLSelectElement).value
  const option = props.options.find(option => option.kind !== 'group' && optionKey(option) === key)
  if (!option && !(key === placeholderKey && props.clearable)) return
  if (option?.disabled) return
  const value = option ? getOptionValue(option) : null
  emit('update:modelValue', value)
  emit('change', value, option ?? null)
}
</script>
