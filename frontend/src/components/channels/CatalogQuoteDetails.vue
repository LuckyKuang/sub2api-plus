<template>
    <section class="catalog-quote min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 text-sm text-gray-700 dark:border-dark-700 dark:text-gray-200">
      <div>
        <div class="catalog-quote-header flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
          <h3 class="min-w-0 break-words text-sm font-semibold [overflow-wrap:anywhere]">{{ group.name }}<span v-if="group.is_exclusive" class="whitespace-nowrap text-purple-700 dark:text-purple-300">{{ ' ' + t('availableChannels.exclusive') }}</span></h3>
          <span class="min-w-0 break-words text-xs text-gray-500 [overflow-wrap:anywhere] dark:text-dark-400">{{ offer.source.name }}</span>
          <span v-if="!group.is_exclusive" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium leading-4 text-gray-600 dark:bg-dark-800 dark:text-dark-300">{{ t('availableChannels.public') }}</span>
          <span v-if="group.subscription_type === 'subscription'" class="text-xs text-gray-500 dark:text-dark-400">{{ t('modelPlaza.badges.subscription') }}</span>
          <CatalogRateBadge :group="group" :offer="offer" />
        </div>
        <p v-if="offer.source.description" class="mt-2 break-words">{{ offer.source.description }}</p>
      </div>
      <p class="rounded-lg bg-gray-50 p-3 text-xs leading-6 dark:bg-dark-800">
        {{ t('availableChannels.catalog.quoteExplanation', { rate }) }}
        {{ t(`availableChannels.catalog.units.${offer.billing_unit}`) }}
        <span v-if="rateUnavailable" class="block text-amber-600 dark:text-amber-400">{{ t('availableChannels.catalog.ratesUnavailable') }}</span>
        <span v-if="independent" class="block">{{ t('availableChannels.catalog.independentRate') }}</span>
      </p>
      <template v-if="offer.pricing">
        <CatalogPriceTable :pricing="offer.pricing" :rate="rate" :token="offer.billing_unit === 'token'" />
        <p v-if="offer.pricing.intervals.length" class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.wholeRequest') }}</p>
        <div v-if="offer.media_tiers?.length" class="flex flex-wrap gap-3">
          <div v-for="tier in offer.media_tiers" :key="tier.label" class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
            {{ tier.label }} <strong class="ml-2 font-mono">{{ catalogMoney(tier.price, rate) }}</strong><span class="ml-1 text-xs">{{ t(`availableChannels.catalog.units.${tier.unit}`) }}</span>
          </div>
        </div>
        <p v-if="offer.billing_unit === 'second'" class="text-xs">{{ t('availableChannels.catalog.videoSeconds') }}</p>
        <div v-if="offer.service_tier_pricing?.length" class="space-y-3">
          <h4 class="font-medium">{{ t('availableChannels.catalog.serviceTiers') }}</h4>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.serviceTierNote') }}</p>
          <section v-for="tier in offer.service_tier_pricing" :key="tier.name" class="space-y-2">
            <h5 class="font-medium">{{ t(`availableChannels.catalog.tiers.${tier.name}`) }}</h5>
            <CatalogPriceTable :pricing="tier.pricing" :rate="rate" />
          </section>
        </div>
        <div v-if="Object.keys(offer.pricing.reasoning_effort_multipliers || {}).length">
          <h4 class="mb-2 font-medium">{{ t('availableChannels.catalog.reasoning') }}</h4>
          <p v-for="(multiplier, effort) in offer.pricing.reasoning_effort_multipliers" :key="effort">{{ effort }} · {{ multiplier }}×</p>
        </div>
      </template>
      <p v-else>{{ t(`availableChannels.catalog.reasons.${offer.price_reason || 'pricing_unavailable'}`) }}</p>
      <div v-if="group.peak_rate_enabled" class="rounded-lg bg-amber-50 p-3 text-xs dark:bg-amber-900/20">
        {{ t('availableChannels.catalog.peak', { start: group.peak_start, end: group.peak_end, timezone: group.peak_timezone, multiplier: group.peak_rate_multiplier }) }}
      </div>
      <div v-if="offer.time_pricing" class="space-y-1 text-xs">
        <h4 class="font-medium">{{ t('availableChannels.catalog.timeRules') }} · {{ offer.time_pricing.timezone }}</h4>
        <p v-if="offer.time_pricing.weekdays_only">{{ t('availableChannels.catalog.weekdays') }}</p>
        <p v-for="period in offer.time_pricing.periods" :key="period.start_time">[{{ period.start_time }}, {{ period.end_time }}) · {{ period.multiplier }}×</p>
      </div>
      <section v-if="official" class="space-y-3">
        <h4 class="font-medium">{{ t('availableChannels.catalog.official') }}</h4>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.catalog.officialNote') }}</p>
        <CatalogPriceTable :pricing="official" />
      </section>
    </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CatalogGroup, CatalogOffer, UserSupportedModelPricing } from '@/api/channels'
import CatalogPriceTable from './CatalogPriceTable.vue'
import CatalogRateBadge from './CatalogRateBadge.vue'
import { catalogMoney, effectiveRate, rateSource } from './catalog'

const props = defineProps<{ offer: CatalogOffer; group: CatalogGroup; rateUnavailable: boolean }>()
const { t } = useI18n()
const rate = computed(() => effectiveRate(props.group, props.offer))
const source = computed(() => rateSource(props.group, props.offer))
const independent = computed(() => source.value === 'media')
const official = computed<UserSupportedModelPricing | null>(() => props.offer.official_pricing ? {
  ...props.offer.official_pricing, billing_mode: 'token', image_input_price: null, image_output_price: null, per_request_price: null, intervals: props.offer.official_pricing.intervals || []
} : null)
</script>
