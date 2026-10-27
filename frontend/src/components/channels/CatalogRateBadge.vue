<template>
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <strong class="catalog-effective-rate rounded px-1.5 py-0.5 text-xs font-semibold leading-4" :class="source === 'personal' ? 'bg-emerald-100/[0.15] text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300' : source === 'media' ? 'bg-cyan-100/[0.15] text-cyan-800 dark:bg-cyan-900/30 dark:text-cyan-300' : 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/30 dark:text-blue-300'">
          {{ Number.isFinite(rate) ? t(`availableChannels.catalog.rateLabels.${source}`, { rate }) : t('billingRates.unknown') }}
        </strong>
        <span v-if="source !== 'group'" class="text-xs surface-muted">{{ t('availableChannels.catalog.defaultRate', { rate: group.rate_multiplier }) }}</span>
        <span v-if="source === 'media' && group.user_rate_multiplier != null" class="text-xs surface-muted">{{ t('availableChannels.catalog.overriddenPersonalRate', { rate: group.user_rate_multiplier }) }}</span>
        <span v-if="offer.rate_quote && offer.rate_quote.rate_calculation !== 'override_v1'" class="text-xs surface-muted">{{ t('billingRates.policyFactors', { platform: offer.rate_quote.platform_multiplier, model: offer.rate_quote.model_multiplier }) }}</span>
        <span v-else-if="offer.rate_quote && offer.rate_quote.source !== 'default'" class="text-xs surface-muted">{{ t(`billingRates.source.${offer.rate_quote.source}`) }} ×{{ offer.rate_quote.policy_multiplier }}</span>
      </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CatalogGroup, CatalogOffer } from '@/api/channels'
import { effectiveRate, rateSource } from './catalog'
const props = defineProps<{ group: CatalogGroup; offer: CatalogOffer }>()
const { t } = useI18n()
const rate = computed(() => effectiveRate(props.group, props.offer))
const source = computed(() => rateSource(props.group, props.offer))
</script>
