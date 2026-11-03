<template>
  <section class="mt-3 space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="long-context-policy">
    <h4 class="text-sm font-medium">{{ t('longContext.title') }}</h4>
    <p class="input-hint">{{ t('longContext.hint') }}</p>
    <details open>
      <summary class="cursor-pointer text-sm">{{ t('longContext.platformDefaults') }}</summary>
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <label v-for="item in platforms" :key="item.id" class="min-w-0" :data-platform="item.id"><span class="input-label">{{ item.display_name }}</span><input :value="modelValue.long_context_platforms?.[item.id] ?? ''" type="number" min="0" max="1000000000" step="1" class="input" :aria-label="`${item.display_name} ${t('longContext.threshold')}`" :placeholder="defaultThresholds(item.id)" @input="setPlatform(item.id, $event)" /><span class="mt-1 block text-xs surface-muted">{{ t('longContext.presetValues', { values: defaultThresholds(item.id) }) }}</span></label>
      </div>
    </details>
    <p v-if="defaultsError" role="alert" class="text-sm text-red-600">{{ t('longContext.defaultsFailed') }} <button type="button" class="underline" @click="loadDefaults">{{ t('common.tryAgain') }}</button></p>
    <div class="flex items-center justify-between gap-2"><span class="text-sm">{{ t('longContext.modelOverrides') }}</span><button type="button" class="btn btn-secondary btn-sm" :disabled="rules.length >= 256" @click="addRule">{{ t('common.add') }}</button></div>
    <div v-for="(rule, index) in rules" :key="index" class="space-y-3 rounded-lg surface-inset p-3">
      <div class="grid gap-3 sm:grid-cols-2">
        <label><span class="input-label">{{ t('billingRates.editor.platform') }}</span><Select :model-value="rule.platform" :options="platformOptions" @update:model-value="setRule(index, 'platform', String($event))" /></label>
        <label><span class="input-label">{{ t('billingRates.editor.model') }}</span><input :value="rule.model" class="input" maxlength="256" required :placeholder="props.platform === 'composite' ? t('longContext.publicModelPlaceholder') : ''" @input="setRule(index, 'model', text($event))" /><span v-if="props.platform === 'composite'" class="mt-1 block text-xs surface-muted">{{ t('longContext.publicModelHint') }}</span></label>
        <label><span class="input-label">{{ t('longContext.threshold') }}</span><input :value="rule.threshold" class="input" type="number" min="0" max="1000000000" step="1" required @input="setRule(index, 'threshold', number($event))" /></label>
        <label><span class="input-label">{{ t('longContext.mode') }}</span><Select :model-value="rule.mode || 'inherit'" :options="modeOptions" @update:model-value="setRule(index, 'mode', String($event))" /></label>
      </div>
      <div v-if="rule.mode === 'prices' || rule.mode === 'multipliers'" class="grid gap-3 sm:grid-cols-2">
        <label v-for="meter in meters" :key="meter"><span class="input-label">{{ t(`longContext.${meter}`) }} · {{ t(rule.mode === 'prices' ? 'longContext.priceUnit' : 'longContext.factorUnit') }}</span><input :value="rule[meter] ?? ''" class="input" type="number" min="0" max="999999.9999" step="any" :required="meter === 'input' || meter === 'output'" :placeholder="t('longContext.inherit')" @input="setRule(index, meter, text($event) === '' ? undefined : number($event))" /></label>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" @click="removeRule(index)">{{ t('common.delete') }}</button>
    </div>
    <p v-if="showValidation && invalid" role="alert" class="text-sm text-red-600">{{ t('longContext.invalid') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { listPlatforms } from '@/constants/platformCatalog'
import { cloneBillingRatePolicy } from '@/utils/billingRate'
import type { BillingRatePolicy, LongContextModelRule } from '@/types/billingRate'
import { apiClient } from '@/api/client'
const props = defineProps<{ modelValue: BillingRatePolicy; platform: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: BillingRatePolicy] }>()
const { t } = useI18n()
const showValidation = ref(false)
const defaults = ref<Record<string, { model: string; threshold: number }[]>>({})
const defaultsLoaded = ref(false)
const defaultsError = ref(false)
async function loadDefaults() {
  defaultsError.value = false
  try {
    const { data } = await apiClient.get<typeof defaults.value>('/admin/channels/long-context-defaults')
    if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('Invalid defaults response')
    defaults.value = data
    defaultsLoaded.value = true
  } catch { defaultsError.value = true }
}
onMounted(loadDefaults)
function defaultThresholds(platform: string) {
  if (!defaultsLoaded.value) return t(defaultsError.value ? 'longContext.defaultsUnknown' : 'common.loading')
  const values = [...new Set((defaults.value[platform] ?? []).map(item => item.threshold))].sort((a, b) => a - b)
  return (values.length ? values : [0]).join(' / ')
}
const platforms = computed(() => listPlatforms().filter(item => props.platform === 'composite' || item.id === props.platform))
const platformOptions = computed(() => platforms.value.map(item => ({ value: item.id, label: item.display_name })))
const rules = computed(() => props.modelValue.long_context_models ?? [])
const meters = ['input', 'output', 'cache_write', 'cache_write_1h', 'cache_read'] as const
const modeOptions = computed(() => ['inherit', 'prices', 'multipliers'].map(value => ({ value, label: t(`longContext.${value}`) })))
const text = (event: Event) => (event.target as HTMLInputElement).value
const number = (event: Event) => text(event) === '' ? Number.NaN : Number(text(event))
function setPlatform(platform: string, event: Event) {
  const policy = cloneBillingRatePolicy(props.modelValue)
  policy.long_context_platforms ??= {}
  if (text(event) === '') delete policy.long_context_platforms[platform]
  else policy.long_context_platforms[platform] = number(event)
  emit('update:modelValue', policy)
}
function setRule(index: number, key: keyof LongContextModelRule, value: string | number | undefined) {
  const policy = cloneBillingRatePolicy(props.modelValue)
  policy.long_context_models![index] = { ...policy.long_context_models![index]!, [key]: value }
  emit('update:modelValue', policy)
}
function addRule() { const policy = cloneBillingRatePolicy(props.modelValue); (policy.long_context_models ??= []).push({ platform: platforms.value[0]?.id || '', model: '', threshold: 0, mode: 'inherit' }); emit('update:modelValue', policy) }
function removeRule(index: number) { const policy = cloneBillingRatePolicy(props.modelValue); policy.long_context_models!.splice(index, 1); emit('update:modelValue', policy) }
const invalid = computed(() => {
  const validThreshold = (value: number) => Number.isInteger(value) && value >= 0 && value <= 1e9
  const validPrice = (value?: number) => value === undefined || (Number.isFinite(value) && value >= 0 && value <= 999999.9999)
  if (Object.values(props.modelValue.long_context_platforms ?? {}).some(value => !validThreshold(value))) return true
  const seen = new Set<string>()
  return rules.value.some(rule => {
    const key = `${rule.platform}\0${rule.model.toLowerCase()}`
    const duplicate = seen.has(key); seen.add(key)
    return duplicate || !listPlatforms().some(item => item.id === rule.platform) || !rule.model || /[\s*]/.test(rule.model) || !validThreshold(rule.threshold) || meters.some(meter => !validPrice(rule[meter])) || ((rule.mode === 'prices' || rule.mode === 'multipliers') && (rule.input === undefined || rule.output === undefined))
  })
})
function validate() { showValidation.value = true; return !invalid.value }
defineExpose({ validate })
</script>
