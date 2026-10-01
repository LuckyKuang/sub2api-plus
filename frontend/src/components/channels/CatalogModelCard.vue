<template>
  <article class="catalog-model-card flex min-w-0 flex-col rounded-xl border border-gray-200 bg-white p-4 shadow-sm transition-shadow hover:shadow-md dark:border-dark-700 dark:bg-dark-900">
    <div class="flex items-center justify-between gap-2 text-xs">
      <span class="inline-flex min-w-0 items-center gap-1.5" :class="platformTextClass(model.platform)">
        <PlatformIcon :platform="model.platform" size="sm" />{{ model.platform }}
      </span>
      <span v-if="model.prices.length > 1" class="text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.multipleUnits') }}</span>
    </div>
    <div class="mb-4 mt-3 flex items-start gap-2">
      <h3 class="min-w-0 flex-1 break-words font-mono text-sm font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">{{ model.name }}</h3>
      <button class="shrink-0 rounded p-1 text-gray-400 hover:text-primary-500 focus-visible:ring-2 focus-visible:ring-primary-500" :aria-label="t('availableChannels.catalog.copyModel', { model: model.name })" @click="copyToClipboard(model.name)">
        <Icon name="copy" size="sm" />
      </button>
    </div>
    <div class="space-y-3">
      <div v-for="price in model.prices" :key="price.unit" class="space-y-1">
        <p class="text-[11px] text-gray-500 dark:text-dark-400">{{ t(`availableChannels.catalog.units.${price.unit}`) }}</p>
        <div v-if="price.input || price.output" class="grid gap-2" :class="price.unit === 'token' ? 'grid-cols-2' : 'grid-cols-1'">
          <div>
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t(price.unit === 'token' ? 'availableChannels.pricing.inputPrice' : 'availableChannels.catalog.unitPrice') }}</p>
            <p class="break-words font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ catalogRange(price.input, price.unit) }}</p>
          </div>
          <div v-if="price.unit === 'token'">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.pricing.outputPrice') }}</p>
            <p class="break-words font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ catalogRange(price.output, price.unit) }}</p>
          </div>
        </div>
        <p v-if="price.incomplete" class="text-xs text-gray-500 dark:text-dark-400">{{ t(price.input || price.output ? 'availableChannels.catalog.partialUnknown' : 'availableChannels.catalog.reasons.pricing_unavailable') }}</p>
      </div>
    </div>
    <p class="mt-2 text-[11px] text-gray-500 dark:text-dark-400">{{ t(rateUnavailable ? 'availableChannels.catalog.referenceRange' : 'availableChannels.catalog.effectiveRange') }}</p>
    <div class="mb-3 mt-2 flex flex-wrap gap-1.5 text-[11px] font-medium">
      <span v-if="model.quotes.some(q => rateSource(q.group, q.offer) === 'personal')" class="rounded-md bg-emerald-100 px-2 py-1 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300">{{ t('availableChannels.catalog.personalRate') }}</span>
      <span v-if="model.tiered !== 'none'" class="rounded-md bg-blue-50 px-2 py-1 text-blue-700 ring-1 ring-inset ring-blue-200 dark:bg-blue-900/30 dark:text-blue-300 dark:ring-blue-800">{{ t(model.tiered === 'all' ? 'availableChannels.pricing.intervals' : 'availableChannels.catalog.someTiered') }}</span>
      <span v-if="model.dynamic !== 'none'" class="rounded-md bg-amber-50 px-2 py-1 text-amber-800 ring-1 ring-inset ring-amber-200 dark:bg-amber-900/30 dark:text-amber-300 dark:ring-amber-800">{{ t(model.dynamic === 'all' ? 'availableChannels.catalog.dynamicBilling' : 'availableChannels.catalog.someDynamic') }}</span>
    </div>
    <div class="mt-auto flex items-center justify-between gap-2 border-t border-gray-100 pt-3 text-xs dark:border-dark-800">
      <span class="text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.quoteCount', { count: model.quotes.length }) }}<span v-if="model.quotes.some(q => q.group.is_exclusive)" class="whitespace-nowrap font-medium text-purple-700 dark:text-purple-300" :title="t('availableChannels.catalog.exclusiveQuote')">{{ ' ' + t('availableChannels.exclusive') }}</span></span>
      <button class="shrink-0 rounded font-medium text-primary-600 focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-400" :aria-label="t('availableChannels.catalog.detailsFor', { model: model.name })" @click="$emit('details', model)">
        {{ t('availableChannels.catalog.details') }} <span aria-hidden="true">→</span>
      </button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { platformTextClass } from '@/utils/platformColors'
import { catalogRange, rateSource, type CatalogModel } from './catalog'

defineProps<{ model: CatalogModel; rateUnavailable: boolean }>()
defineEmits<{ details: [model: CatalogModel] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
</script>
