<template>
  <section class="min-w-0 space-y-2 text-xs" aria-live="polite">
    <p v-if="loading" class="surface-muted">{{ t('common.loading') }}</p>
    <div v-else-if="failed || !summary || summary.status === 'unavailable'" class="flex flex-wrap items-center gap-2 text-amber-700 dark:text-amber-300">
      <span>{{ t('billingRates.unknown') }}</span>
      <button v-if="!compact" type="button" class="btn btn-secondary btn-sm" @click="emit('retry')">{{ t('billingRates.retry') }}</button>
    </div>
    <p v-else-if="summary.status === 'empty'" class="surface-muted">{{ t('billingRates.noModels') }}</p>
    <template v-else>
      <div v-for="entry in summary.entries" :key="`${entry.platform}:${entry.billing_unit}`" :data-rate-entry="`${entry.platform}:${entry.billing_unit}`" class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <strong>{{ platformLabel(entry.platform) }}</strong>
        <span class="surface-muted">{{ t(`billingRates.units.${entry.billing_unit}`, entry.billing_unit) }}</span>
        <span>{{ t('billingRates.platformDefault') }} {{ entry.default_quote ? formatBillingRate(entry.default_quote.effective_multiplier) : t('billingRates.unknown') }}</span>
        <span v-if="entry.has_model_exceptions || !entry.default_quote" class="surface-muted">{{ t('billingRates.modelRange') }} {{ range(entry.rate_min, entry.rate_max) }}</span>
      </div>
      <template v-if="!compact">
        <p class="surface-muted">{{ t('billingRates.keyQuoteNote') }}</p>
        <PlanModelCatalog :catalog="modelCatalog" />
      </template>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CatalogGroup } from '@/api/channels'
import { formatBillingRate } from '@/utils/billingRate'
import { platformLabel } from '@/utils/platformColors'
import PlanModelCatalog from '@/components/payment/PlanModelCatalog.vue'
const props = defineProps<{ group?: CatalogGroup; loading?: boolean; failed?: boolean; compact?: boolean }>()
const emit = defineEmits<{ retry: [] }>()
const { t } = useI18n()
const summary = computed(() => props.group?.rate_summary)
const modelCatalog = computed(() => ({ policy_version: summary.value?.policy_version ?? 0, as_of: summary.value?.as_of ?? '', models: props.group?.models ?? [], platforms: [], rate_min: null, rate_max: null }))
function range(min: number | null, max: number | null) {
  if (min == null || max == null) return t('billingRates.unknown')
  return min === max ? formatBillingRate(min) : `${formatBillingRate(min)}–${formatBillingRate(max)}`
}
</script>
