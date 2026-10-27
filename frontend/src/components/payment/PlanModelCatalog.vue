<template>
  <details v-if="catalog" class="space-y-3 rounded-lg surface-inset p-3 text-xs">
    <summary class="cursor-pointer font-medium">{{ t('billingRates.models') }} · {{ catalog.models.length }}</summary>
    <p class="leading-5 surface-muted">{{ t('billingRates.saleNote') }}</p>
    <p v-if="!catalog.models.length" class="surface-muted">{{ t('billingRates.noModels') }}</p>
    <details v-for="model in catalog.models" :key="model.offer_key" :open="openModels.has(model.offer_key)" class="border-t border-gray-200 py-2 dark:border-dark-600">
      <summary class="flex min-w-0 cursor-pointer flex-wrap items-start justify-between gap-2" @click.prevent="toggleModel(model.offer_key)">
      <div class="min-w-0 flex-1">
        <span class="font-medium">{{ platformLabel(model.platform) }}</span>
        <div class="break-words font-mono [overflow-wrap:anywhere]">{{ model.name }}</div>
        <span class="surface-muted">{{ t(`billingRates.units.${model.billing_unit}`, model.billing_unit) }}</span>
      </div>
      <div class="text-right">
        <strong>{{ model.rate_quote ? formatBillingRate(model.rate_quote.effective_multiplier) : t('billingRates.unknown') }}</strong>
        <p v-if="model.rate_quote" class="mt-1 surface-muted">{{ t(`billingRates.source.${model.rate_quote.source}`) }}</p>
      </div>
      </summary>
      <CatalogQuoteDetails v-if="openModels.has(model.offer_key)" class="mt-3" :offer="model" :rate-unavailable="!model.rate_quote" />
    </details>
    <p v-if="catalog.as_of" class="surface-muted">{{ t('billingRates.asOf', { time: catalog.as_of }) }}</p>
  </details>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SaleModelCatalog } from '@/types/billingRate'
import { formatBillingRate } from '@/utils/billingRate'
import { platformLabel } from '@/utils/platformColors'
import CatalogQuoteDetails from '@/components/channels/CatalogQuoteDetails.vue'
const props = defineProps<{ catalog?: SaleModelCatalog }>()
const { t } = useI18n()
const openModels = ref(new Set<string>())
function toggleModel(key: string) {
  if (openModels.value.has(key)) openModels.value.delete(key)
  else openModels.value.add(key)
}
watch(() => props.catalog, () => { openModels.value = new Set() })
</script>
