<template>
  <section class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <div>
      <h3 class="text-sm font-semibold">{{ t('billingRates.editor.title') }}</h3>
      <p class="input-hint">{{ t('billingRates.editor.hint') }}</p>
    </div>
    <details v-if="showPlatforms">
      <summary class="cursor-pointer text-sm font-medium">{{ t('billingRates.editor.platforms') }}</summary>
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <div v-for="item in platforms" :key="item.id" :data-platform="item.id" class="flex min-w-0 items-center gap-3">
          <label class="flex min-w-0 flex-1 items-center gap-2 text-sm">
            <input type="checkbox" class="checkbox" :checked="hasPlatform(item.id)" @change="togglePlatform(item.id, $event)">
            <span class="break-words">{{ item.display_name }}</span>
          </label>
          <input v-if="hasPlatform(item.id)" :id="`${idPrefix}-${item.id}-rate`" :value="draft.platform_multipliers[item.id]" type="number" min="0" max="999999.9999" step="0.0001" required class="input w-28" :aria-label="`${item.display_name} ${t('billingRates.editor.multiplier')}`" @input="setPlatformRate(item.id, $event)">
          <span v-else class="text-xs surface-muted">{{ t('billingRates.editor.inherit') }}</span>
        </div>
      </div>
    </details>
    <div v-if="showModels" class="space-y-3">
      <div class="flex items-center justify-between gap-2">
        <h4 class="text-sm font-medium">{{ t('billingRates.editor.models') }}</h4>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="draft.model_multipliers.length >= 256" @click="addModel">{{ t('billingRates.editor.addModel') }}</button>
      </div>
      <div v-for="{ row, index } in visibleModels" :key="rowKeys[index]" class="grid gap-3 rounded-lg surface-inset p-3 sm:grid-cols-2">
        <div>
          <label :for="`${idPrefix}-model-${index}-platform`" class="input-label">{{ t('billingRates.editor.platform') }}</label>
          <Select :id="`${idPrefix}-model-${index}-platform`" :model-value="row.platform" :options="platformOptions" searchable @update:model-value="setModel(index, 'platform', String($event))" />
        </div>
        <div>
          <label :for="`${idPrefix}-model-${index}-name`" class="input-label">{{ t('billingRates.editor.model') }}</label>
          <input :id="`${idPrefix}-model-${index}-name`" :value="row.model" class="input" type="text" required maxlength="256" :placeholder="t('billingRates.editor.modelPlaceholder')" @input="setModel(index, 'model', inputValue($event))">
        </div>
        <div>
          <label :for="`${idPrefix}-model-${index}-unit`" class="input-label">{{ t('billingRates.editor.unit') }}</label>
          <Select :id="`${idPrefix}-model-${index}-unit`" :model-value="row.billing_unit ?? ''" :options="unitOptions" searchable @update:model-value="setModel(index, 'billing_unit', String($event))" />
        </div>
        <div class="flex items-end gap-2">
          <div class="min-w-0 flex-1">
            <label :for="`${idPrefix}-model-${index}-rate`" class="input-label">{{ t('billingRates.editor.multiplier') }}</label>
            <input :id="`${idPrefix}-model-${index}-rate`" :value="row.multiplier" class="input" type="number" min="0" max="999999.9999" step="0.0001" required @input="setModel(index, 'multiplier', inputNumber($event))">
          </div>
          <button type="button" class="btn btn-secondary" :aria-label="t('billingRates.editor.remove')" @click="removeModel(index)">×</button>
        </div>
      </div>
    </div>
    <p v-if="showValidation && error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(`billingRates.editor.errors.${error}`) }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { listPlatforms } from '@/constants/platformCatalog'
import type { BillingModelMultiplier, BillingRatePolicy } from '@/types/billingRate'
import { billingRatePolicyError, billingRateUnits, cloneBillingRatePolicy } from '@/utils/billingRate'

const props = withDefaults(defineProps<{ modelValue: BillingRatePolicy; platform: string; idPrefix: string; showPlatforms?: boolean; showModels?: boolean; cardModelKeys?: string[] }>(), { showPlatforms: true, showModels: true, cardModelKeys: () => [] })
const emit = defineEmits<{ 'update:modelValue': [value: BillingRatePolicy] }>()
const { t } = useI18n()
const draft = ref(cloneBillingRatePolicy(props.modelValue))
const rowKeys = ref(draft.value.model_multipliers.map(() => Symbol()))
const freshRows = ref(new Set<symbol>())
const showValidation = ref(false)
let lastEmitted: BillingRatePolicy | undefined
watch(() => props.modelValue, value => {
  if (toRaw(value) === lastEmitted) return
  const previous = draft.value.model_multipliers
  const previousKeys = rowKeys.value
  const used = new Set<number>()
  const next = cloneBillingRatePolicy(value)
  rowKeys.value = next.model_multipliers.map(row => {
    const index = previous.findIndex((old, index) => !used.has(index) && old.platform === row.platform && old.model === row.model && (old.billing_unit ?? '') === (row.billing_unit ?? '') && Object.is(old.multiplier, row.multiplier))
    if (index < 0) return Symbol()
    used.add(index)
    return previousKeys[index]!
  })
  freshRows.value = new Set(rowKeys.value.filter(key => freshRows.value.has(key)))
  draft.value = next
  showValidation.value = false
}, { deep: true })
const platforms = computed(() => listPlatforms().filter(item => props.platform === 'composite' || item.id === props.platform))
const platformOptions = computed(() => platforms.value.map(item => ({ value: item.id, label: item.display_name })))
const unitOptions = computed(() => billingRateUnits.map(unit => ({ value: unit, label: t(`billingRates.units.${unit || 'all'}`) })))
const error = computed(() => billingRatePolicyError(draft.value))
const visibleModels = computed(() => draft.value.model_multipliers.map((row, index) => ({ row, index })).filter(({ row, index }) => freshRows.value.has(rowKeys.value[index]!) || !!row.billing_unit || !props.cardModelKeys.includes(`${row.platform}:${row.model.toLowerCase()}`)))
const hasPlatform = (platform: string) => Object.prototype.hasOwnProperty.call(draft.value.platform_multipliers, platform)
function publish() {
  // Keep an editable shared draft even while incomplete. Submission validates
  // the whole policy; a sibling platform/model editor must not erase it.
  const value = cloneBillingRatePolicy(draft.value)
  lastEmitted = value
  emit('update:modelValue', value)
}
const inputValue = (event: Event) => (event.target as HTMLInputElement).value
const inputNumber = (event: Event) => inputValue(event).trim() === '' ? Number.NaN : Number(inputValue(event))
function togglePlatform(platform: string, event: Event) {
  if ((event.target as HTMLInputElement).checked) draft.value.platform_multipliers[platform] = 1
  else delete draft.value.platform_multipliers[platform]
  publish()
}
function setPlatformRate(platform: string, event: Event) { draft.value.platform_multipliers[platform] = inputNumber(event); publish() }
function setModel(index: number, key: keyof BillingModelMultiplier, value: string | number) {
  draft.value.model_multipliers[index] = { ...draft.value.model_multipliers[index]!, [key]: value }
  publish()
}
function addModel() {
  draft.value.model_multipliers.push({ platform: platforms.value[0]?.id ?? '', model: '', multiplier: 1 })
  const key = Symbol()
  rowKeys.value.push(key)
  freshRows.value.add(key)
  publish()
}
function removeModel(index: number) { freshRows.value.delete(rowKeys.value[index]!); draft.value.model_multipliers.splice(index, 1); rowKeys.value.splice(index, 1); publish() }
function validate() { showValidation.value = true; return !error.value }
defineExpose({ validate })
</script>
