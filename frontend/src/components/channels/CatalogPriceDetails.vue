<template>
  <BaseDialog :show="!!model" :title="t('availableChannels.catalog.details')" width="wide" @close="$emit('close')">
    <div v-if="model" :key="model.key" class="min-w-0 space-y-5">
      <div>
        <h3 class="break-words font-mono text-lg font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">{{ model.name }}</h3>
        <p class="mt-1 text-sm surface-muted">{{ model.platform }} · {{ t('availableChannels.catalog.groupQuotes', { count: model.quotes.length }) }}</p>
      </div>
      <p class="rounded-lg surface-inset p-3 text-sm surface-muted">{{ t('availableChannels.catalog.billingGroupNote') }}</p>
      <p v-if="rateUnavailable" class="text-sm text-amber-700 dark:text-amber-300">{{ t('availableChannels.catalog.ratesUnavailable') }}</p>
      <div class="record-table-scroll record-table-dialog-scroll max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700" tabindex="0" :aria-label="t('availableChannels.catalog.quoteComparison')">
        <table class="record-table catalog-comparison w-full text-left text-xs surface-muted">
          <thead class="surface-inset surface-muted"><tr>
            <th class="px-3 py-2">{{ t('modelPlaza.filters.groupLabel') }}</th>
            <th class="px-3 py-2">{{ t('availableChannels.catalog.effectiveRateLabel') }}</th>
            <th class="px-3 py-2">{{ t('availableChannels.catalog.baseInputOrUnit') }}</th>
            <th class="px-3 py-2">{{ t('availableChannels.pricing.outputPrice') }}</th>
            <th class="px-3 py-2">{{ t('availableChannels.catalog.billingUnit') }}</th>
          </tr></thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="quote in model.quotes" :key="quote.offer.offer_key">
            <th class="min-w-28 break-words px-3 py-2 font-medium">
              {{ quote.group.name }}<span v-if="quote.group.is_exclusive" class="whitespace-nowrap text-purple-700 dark:text-purple-300">{{ ' ' + t('availableChannels.exclusive') }}</span>
            </th>
            <td class="min-w-28 px-3 py-2"><CatalogRateBadge :group="quote.group" :offer="quote.offer" /></td>
            <td class="whitespace-nowrap px-3 py-2 font-mono">{{ catalogMoney(quotePrice(quote, false), 1, quote.offer.billing_unit === 'token') }}</td>
            <td class="whitespace-nowrap px-3 py-2 font-mono">{{ quote.offer.billing_unit === 'token' ? catalogMoney(quotePrice(quote, true), 1, true) : '—' }}</td>
            <td class="whitespace-nowrap px-3 py-2">{{ t(`availableChannels.catalog.units.${quote.offer.billing_unit}`) }}</td>
          </tr></tbody>
        </table>
      </div>
      <details v-for="quote in model.quotes" :key="quoteKey(quote)" class="catalog-quote-disclosure min-w-0" :open="openQuotes.has(quoteKey(quote))">
        <summary class="cursor-pointer rounded-xl border border-gray-200 p-4 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-700" @click.prevent="toggleQuote(quoteKey(quote))">
          <span class="font-semibold">{{ quote.group.name }}</span>
          <span class="ml-2 surface-muted">{{ t('availableChannels.catalog.quoteDetails') }}</span>
        </summary>
        <CatalogQuoteDetails v-if="openQuotes.has(quoteKey(quote))" class="mt-3" :group="quote.group" :offer="quote.offer" :rate-unavailable="rateUnavailable" />
      </details>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import CatalogQuoteDetails from './CatalogQuoteDetails.vue'
import CatalogRateBadge from './CatalogRateBadge.vue'
import { catalogMoney, quotePrice, type CatalogModel } from './catalog'

const props = defineProps<{ model: CatalogModel | null; rateUnavailable: boolean }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const openQuotes = ref(new Set<string>())
const quoteKey = (quote: CatalogModel['quotes'][number]) => `${quote.group.id}:${quote.offer.offer_key}`
function toggleQuote(key: string) {
  if (openQuotes.value.has(key)) openQuotes.value.delete(key)
  else openQuotes.value.add(key)
}
// BaseDialog owns the shared focus trap, Escape and focus restoration. Hidden
// quotes and tiers have no mounted controls for its keyboard loop to inspect.
watch(() => props.model?.key, () => { openQuotes.value = new Set() })
</script>
