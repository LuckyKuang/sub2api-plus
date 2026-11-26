<template>
  <section class="price-section">
    <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      <h4 class="text-sm font-semibold">{{ title }}</h4>
      <span v-if="bands.length" class="text-xs surface-muted">{{ t(unit) }}</span>
    </div>
    <p v-if="!bands.length" class="mt-2 text-sm surface-muted">{{ t('modelPlaza.detail.noPricing') }}</p>
    <template v-else>
      <p v-if="bands[0].label" class="mt-2 text-xs font-medium surface-muted">{{ bands[0].label }}</p>
      <dl class="mt-2 flex flex-wrap gap-x-6 gap-y-1">
        <div v-for="line in bands[0].primary" :key="line.name" class="flex items-baseline gap-2">
          <dt class="text-xs surface-muted">{{ line.name }}</dt>
          <dd class="break-all text-base font-semibold tabular-nums">{{ line.value }}</dd>
        </div>
      </dl>
      <details v-if="details.length" class="breakdown">
        <summary class="cursor-pointer text-xs font-medium">{{ t('modelPlaza.catalog.details') }}</summary>
        <div v-for="(part, index) in details" :key="index" class="mt-3">
          <p v-if="part.label" class="mb-1 text-xs font-semibold">{{ part.label }}</p>
          <dl>
            <div v-for="line in part.lines" :key="line.name" class="price-line">
              <dt class="surface-muted">{{ line.name }}</dt>
              <dd class="break-all text-right font-medium tabular-nums">{{ line.value }}</dd>
            </div>
          </dl>
        </div>
      </details>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlazaModel } from '@/api/modelPlaza'
import { BILLING_MODE_TOKEN } from '@/constants/channel'
import { billingMode, hasTierCachePricing, priceBands, priceUnit, requestPrice, tokenPrice, type CachePrices } from './pricing'

const props = defineProps<{ model: PlazaModel; title: string; source: 'paid' | 'official'; rate: number }>()
const { t } = useI18n()
const isToken = computed(() => props.source === 'official' || billingMode(props.model) === BILLING_MODE_TOKEN)
const unit = computed(() => isToken.value ? 'modelPlaza.catalog.perMillion' : priceUnit(props.model))
const bands = computed(() => priceBands(props.model, props.source).map(({ label, prices }) => {
  const primary = isToken.value
    ? [
        { name: t('modelPlaza.table.input'), value: tokenPrice(prices.input_price, props.rate) },
        { name: t('modelPlaza.table.output'), value: tokenPrice(prices.output_price, props.rate) }
      ]
    : [{ name: t('modelPlaza.catalog.price'), value: requestPrice(prices.per_request_price, props.rate) }]
  return { label, primary, cache: isToken.value ? cacheLines(prices) : [] }
}))

function cacheLines(prices: CachePrices) {
  const lines = []
  const writes = [
    ['5m', prices.cache_write_price],
    ['1h', prices.cache_write_1h_price],
    ['12h', prices.cache_write_12h_price]
  ] as const
  for (const [duration, value] of writes) {
    if (value != null) lines.push({ name: t('modelPlaza.catalog.cacheWriteDuration', { duration }), value: tokenPrice(value, props.rate) })
  }
  if (prices.cache_read_price != null) {
    lines.push({ name: t('modelPlaza.catalog.cacheRead'), value: tokenPrice(prices.cache_read_price, props.rate) })
  }
  return lines
}

const details = computed(() => {
  const tiered = bands.value.length > 1
  const parts = bands.value.map(band => ({
    label: tiered ? band.label : '',
    lines: tiered ? [...band.primary, ...band.cache] : band.cache
  }))
  const official = props.model.official_pricing
  if (props.source === 'official' && official?.intervals?.length && !hasTierCachePricing(official.intervals)) {
    parts.push({ label: t('modelPlaza.table.cache'), lines: cacheLines(official) })
  }
  return parts.filter(part => part.lines.length > 0)
})
</script>

<style scoped>
.price-section { min-width: 0; padding: 14px; border: 1px solid var(--surface-edge); border-radius: var(--surface-radius-card); }
.breakdown { margin-top: 12px; padding-top: 10px; border-top: 1px solid var(--surface-edge); }
.breakdown summary { color: var(--console-accent-readable); }
.price-line { display: flex; justify-content: space-between; gap: 12px; padding: 3px 0; font-size: .8125rem; }
</style>
