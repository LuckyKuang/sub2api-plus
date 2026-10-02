<template>
  <BaseDialog :show="!!model" :title="t('availableChannels.catalog.details')" width="wide" @close="$emit('close')">
    <div v-if="model" ref="content" class="min-w-0 space-y-5">
      <div>
        <h3 class="break-words font-mono text-lg font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">{{ model.name }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ model.platform }} · {{ t('availableChannels.catalog.groupQuotes', { count: model.quotes.length }) }}</p>
      </div>
      <p class="rounded-lg bg-gray-50 p-3 text-sm text-gray-600 dark:bg-dark-800 dark:text-dark-300">{{ t('availableChannels.catalog.billingGroupNote') }}</p>
      <p v-if="rateUnavailable" class="text-sm text-amber-700 dark:text-amber-300">{{ t('availableChannels.catalog.ratesUnavailable') }}</p>
      <div class="max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700" tabindex="0" :aria-label="t('availableChannels.catalog.quoteComparison')">
        <table class="catalog-comparison w-full text-left text-xs text-gray-700 dark:text-gray-200">
          <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800 dark:text-dark-400"><tr>
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
      <CatalogQuoteDetails v-for="quote in model.quotes" :key="quote.offer.offer_key" :group="quote.group" :offer="quote.offer" :rate-unavailable="rateUnavailable" />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import CatalogQuoteDetails from './CatalogQuoteDetails.vue'
import CatalogRateBadge from './CatalogRateBadge.vue'
import { catalogMoney, quotePrice, type CatalogModel } from './catalog'

const props = defineProps<{ model: CatalogModel | null; rateUnavailable: boolean }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const content = ref<HTMLElement>()

// BaseDialog owns Escape and focus restoration. Constrain Tab/focus to this
// dialog (including its header button) without changing other modal consumers.
function focusables(): HTMLElement[] {
  return Array.from(content.value?.closest('[role="dialog"]')?.querySelectorAll<HTMLElement>('button, a[href], input, select, [tabindex="0"]') || [])
}
function trapTab(event: KeyboardEvent) {
  if (!props.model || event.key !== 'Tab') return
  const list = focusables(), first = list[0], last = list[list.length - 1]
  if (!first) return
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
function containFocus(event: FocusEvent) {
  const dialog = content.value?.closest('[role="dialog"]')
  if (props.model && dialog && !dialog.contains(event.target as Node)) focusables()[0]?.focus()
}
onMounted(() => { document.addEventListener('keydown', trapTab); document.addEventListener('focusin', containFocus) })
onUnmounted(() => { document.removeEventListener('keydown', trapTab); document.removeEventListener('focusin', containFocus) })
</script>
