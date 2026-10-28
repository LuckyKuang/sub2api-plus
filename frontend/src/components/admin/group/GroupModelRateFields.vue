<template>
  <section class="space-y-3 rounded-lg surface-inset p-3" data-testid="group-model-selling-rates">
    <h4 class="text-xs font-semibold">{{ t('billingRates.editor.cardTitle') }}</h4>
    <p class="text-xs surface-muted">{{ t('billingRates.editor.cardHint') }}</p>
    <div v-if="platform === 'composite' || selectablePlatform" class="space-y-1">
      <label :for="`${idPrefix}-supplier`" class="input-label">{{ t('billingRates.editor.platform') }}</label>
      <Select :id="`${idPrefix}-supplier`" :model-value="concretePlatform" :options="platformOptions" :placeholder="t('billingRates.editor.choosePlatform')" searchable @update:model-value="emit('update:platform', String($event))" />
    </div>
    <p v-if="!concretePlatform" class="text-xs surface-muted">{{ t('billingRates.editor.choosePlatform') }}</p>
    <p v-else-if="!models.length" class="text-xs surface-muted">{{ t('billingRates.editor.addModelsFirst') }}</p>
    <div v-for="(model, index) in concretePlatform ? models : []" :key="model" class="flex min-w-0 flex-wrap items-center gap-2">
      <label :for="`${idPrefix}-${index}`" class="min-w-0 flex-1 break-words text-xs font-mono">{{ model }}</label>
      <input :id="`${idPrefix}-${index}`" :value="modelRate(model)" type="number" min="0" max="999999.9999" step="0.0001" placeholder="1" class="input w-28" :aria-label="`${model} ${t('billingRates.editor.multiplier')}`" @input="setRate(model, $event)">
      <span class="text-xs surface-muted">{{ t('billingRates.editor.modelDefault') }}</span>
    </div>
    <button v-if="hasRates" type="button" class="btn btn-secondary btn-sm" @click="emit('rateOnly')">{{ t('billingRates.editor.keepRateOnly') }}</button>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { listPlatforms } from '@/constants/platformCatalog'
import type { BillingRatePolicy } from '@/types/billingRate'
import { cloneBillingRatePolicy } from '@/utils/billingRate'
const props = defineProps<{ modelValue: BillingRatePolicy; models: string[]; platform: string; selectablePlatform?: boolean; idPrefix: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: BillingRatePolicy]; 'update:platform': [value: string]; rateOnly: [] }>()
const { t } = useI18n()
const platformOptions = computed(() => listPlatforms().map(item => ({ value: item.id, label: item.display_name })))
const concretePlatform = computed(() => platformOptions.value.some(item => item.value === props.platform) ? props.platform : '')
const matches = (row: BillingRatePolicy['model_multipliers'][number], model: string) => row.platform === concretePlatform.value && row.model.toLowerCase() === model.toLowerCase() && !row.billing_unit
const modelRate = (model: string) => props.modelValue.model_multipliers.find(row => matches(row, model))?.multiplier ?? ''
const hasRates = computed(() => props.modelValue.model_multipliers.some(row => row.platform === concretePlatform.value && props.models.some(model => row.model.toLowerCase() === model.toLowerCase())))
function setRate(model: string, event: Event) {
  const raw = (event.target as HTMLInputElement).value.trim()
  const policy = cloneBillingRatePolicy(props.modelValue)
  policy.model_multipliers = policy.model_multipliers.filter(row => !matches(row, model))
  if (raw !== '') policy.model_multipliers.push({ platform: concretePlatform.value, model, multiplier: Number(raw) })
  emit('update:modelValue', policy)
}
</script>
