<template>
    <section class="catalog-quote min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700 surface-muted">
      <div>
        <div class="catalog-quote-header flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
          <h3 v-if="group" class="min-w-0 break-words text-sm font-semibold [overflow-wrap:anywhere]">{{ group.name }}<span v-if="group.is_exclusive" class="whitespace-nowrap text-purple-700 dark:text-purple-300">{{ ' ' + t('availableChannels.exclusive') }}</span></h3>
          <span class="min-w-0 break-words text-xs [overflow-wrap:anywhere] surface-muted">{{ offer.source.name }}</span>
          <span v-if="group && !group.is_exclusive" class="rounded surface-inset px-1.5 py-0.5 text-xs font-medium leading-4 surface-muted">{{ t('availableChannels.public') }}</span>
          <span v-if="group?.subscription_type === 'subscription'" class="text-xs surface-muted">{{ t('modelPlaza.badges.subscription') }}</span>
          <CatalogRateBadge v-if="group" :group="group" :offer="offer" />
        </div>
        <p v-if="offer.source.description" class="mt-2 break-words">{{ offer.source.description }}</p>
      </div>
      <p class="rounded-lg surface-inset p-3 text-xs leading-6">
        {{ t('availableChannels.catalog.quoteExplanation', { rate }) }}
        {{ t(`availableChannels.catalog.units.${offer.billing_unit}`) }}
        <span v-if="rateUnavailable" class="block text-amber-600 dark:text-amber-400">{{ t('availableChannels.catalog.ratesUnavailable') }}</span>
        <span v-if="independent" class="block">{{ t('availableChannels.catalog.independentRate') }}</span>
      </p>
      <p v-if="offer.rate_quote" class="text-xs surface-muted">
        {{ offer.rate_quote.rate_calculation === 'override_v1'
          ? t('billingRates.legacyFactorBreakdown', { base: offer.rate_quote.base_multiplier, policy: offer.rate_quote.policy_multiplier, peak: offer.rate_quote.peak_multiplier })
          : t('billingRates.factorBreakdown', { base: offer.rate_quote.base_multiplier, platform: offer.rate_quote.platform_multiplier, model: offer.rate_quote.model_multiplier, peak: offer.rate_quote.peak_multiplier }) }}
        · {{ t('billingRates.asOf', { time: offer.rate_quote.as_of }) }}
      </p>
      <p v-if="offer.price_time_multiplier != null && offer.price_time_multiplier !== 1" class="text-xs surface-muted">{{ t('billingRates.priceTimeFactor', { factor: offer.price_time_multiplier }) }}</p>
      <template v-if="offer.pricing">
        <CatalogPriceTable :pricing="offer.pricing" :rate="rate" :token="offer.billing_unit === 'token'" />
        <p v-if="offer.pricing.intervals.length" class="text-xs surface-muted">{{ t('availableChannels.catalog.wholeRequest') }}</p>
        <div v-if="offer.media_tiers?.length" class="flex flex-wrap gap-3">
          <div v-for="tier in offer.media_tiers" :key="tier.label" class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
            {{ tier.label }} <strong class="ml-2 font-mono">{{ catalogMoney(tier.price, rate) }}</strong><span class="ml-1 text-xs">{{ t(`availableChannels.catalog.units.${tier.unit}`) }}</span>
          </div>
        </div>
        <p v-if="offer.billing_unit === 'second'" class="text-xs">{{ t('availableChannels.catalog.videoSeconds') }}</p>
        <div v-if="offer.service_tier_pricing?.length" class="space-y-3">
          <h4 class="font-medium">{{ t('availableChannels.catalog.serviceTiers') }}</h4>
          <p class="text-xs surface-muted">{{ t('availableChannels.catalog.serviceTierNote') }}</p>
          <details v-for="tier in offer.service_tier_pricing" :key="tier.name" :open="openTiers.has(tier.name)" class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <summary class="cursor-pointer font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" @click.prevent="toggleTier(tier.name)">{{ t(`availableChannels.catalog.tiers.${tier.name}`) }}</summary>
            <CatalogPriceTable v-if="openTiers.has(tier.name)" :pricing="tier.pricing" :rate="rate" />
          </details>
        </div>
        <div v-if="Object.keys(offer.pricing.reasoning_effort_multipliers || {}).length">
          <h4 class="mb-2 font-medium">{{ t('availableChannels.catalog.reasoning') }}</h4>
          <p v-for="(multiplier, effort) in offer.pricing.reasoning_effort_multipliers" :key="effort">{{ effort }} · {{ multiplier }}×</p>
        </div>
      </template>
      <p v-else>{{ t(`availableChannels.catalog.reasons.${offer.price_reason || 'pricing_unavailable'}`) }}</p>
      <div v-if="group?.peak_rate_enabled" class="rounded-lg bg-amber-50/[0.15] p-3 text-xs dark:bg-amber-900/20">
        {{ t('availableChannels.catalog.peak', { start: group.peak_start, end: group.peak_end, timezone: group.peak_timezone, multiplier: group.peak_rate_multiplier }) }}
      </div>
      <div v-if="offer.time_pricing" class="space-y-1 text-xs">
        <h4 class="font-medium">{{ t('availableChannels.catalog.timeRules') }} · {{ offer.time_pricing.timezone }}</h4>
        <p v-if="offer.time_pricing.weekdays_only">{{ t('availableChannels.catalog.weekdays') }}</p>
        <p v-for="period in offer.time_pricing.periods" :key="period.start_time">[{{ period.start_time }}, {{ period.end_time }}) · {{ period.multiplier }}×</p>
      </div>
      <section v-if="official" class="space-y-3">
        <h4 class="font-medium">{{ t('availableChannels.catalog.official') }}</h4>
        <p class="text-xs surface-muted">{{ t('availableChannels.catalog.officialNote') }}</p>
        <CatalogPriceTable :pricing="official" />
      </section>
    </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CatalogGroup, CatalogOffer, UserSupportedModelPricing } from '@/api/channels'
import CatalogPriceTable from './CatalogPriceTable.vue'
import CatalogRateBadge from './CatalogRateBadge.vue'
import { catalogMoney, effectivePriceRate, rateSource } from './catalog'

const props = defineProps<{ offer: CatalogOffer; group?: CatalogGroup; rateUnavailable: boolean }>()
const { t } = useI18n()
const openTiers = ref(new Set<string>())
function toggleTier(name: string) {
  if (openTiers.value.has(name)) openTiers.value.delete(name)
  else openTiers.value.add(name)
}
watch(() => props.offer, () => { openTiers.value = new Set() })
const rate = computed(() => effectivePriceRate(props.group, props.offer))
const source = computed(() => rateSource(props.group, props.offer))
const independent = computed(() => source.value === 'media')
const official = computed<UserSupportedModelPricing | null>(() => props.offer.official_pricing ? {
  ...props.offer.official_pricing, billing_mode: 'token', image_input_price: null, image_output_price: null, per_request_price: null, intervals: props.offer.official_pricing.intervals || []
} : null)
</script>
