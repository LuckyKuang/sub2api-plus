<template>
  <div class="max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700" tabindex="0" :aria-label="t('availableChannels.catalog.prices')">
    <table class="w-full text-left text-xs">
      <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800 dark:text-dark-400"><tr>
        <th class="px-3 py-2">{{ t('availableChannels.catalog.context') }}</th>
        <th v-for="field in fields" :key="field.key" class="whitespace-nowrap px-3 py-2">{{ t(field.key === 'per_request_price' && !token ? 'availableChannels.catalog.unitPrice' : `availableChannels.pricing.${field.label}`) }}</th>
      </tr></thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="(row, i) in rows" :key="i">
        <th class="whitespace-nowrap px-3 py-2 font-normal">{{ row.label }}</th>
        <td v-for="field in fields" :key="field.key" class="whitespace-nowrap px-3 py-2 font-mono">{{ catalogMoney(row.pricing[field.key], rate, token && field.key !== 'per_request_price') }}</td>
      </tr></tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSupportedModelPricing } from '@/api/channels'
import { catalogMoney } from './catalog'
const props = withDefaults(defineProps<{ pricing: UserSupportedModelPricing; rate?: number; token?: boolean }>(), { rate: 1, token: true })
const { t } = useI18n()
type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_write_1h_price' | 'cache_read_price' | 'per_request_price' | 'image_input_price' | 'image_output_price'
const columns: { key: PriceKey; label: string }[] = [
  { key: 'input_price', label: 'inputPrice' }, { key: 'output_price', label: 'outputPrice' },
  { key: 'cache_write_price', label: 'cacheWrite5mPrice' }, { key: 'cache_write_1h_price', label: 'cacheWrite1hPrice' },
  { key: 'cache_read_price', label: 'cacheReadPrice' }, { key: 'image_input_price', label: 'imageInputPrice' },
  { key: 'image_output_price', label: 'imageOutputPrice' }, { key: 'per_request_price', label: 'perRequestPrice' }
]
const rows = computed(() => {
  const base = { label: t('availableChannels.catalog.baseTier'), pricing: props.pricing }
  return [base, ...props.pricing.intervals.map(iv => ({
    label: `(${iv.min_tokens.toLocaleString()}, ${iv.max_tokens == null ? '∞' : iv.max_tokens.toLocaleString()}]`,
    pricing: iv as Partial<Record<PriceKey, number | null>>
  }))]
})
const fields = computed(() => columns.filter(f => rows.value.some(r => r.pricing[f.key] != null)))
</script>
