<template>
  <BaseDialog :show="show" :title="model.name" placement="right" close-on-click-outside @close="$emit('close')">
    <template #header-actions>
      <button
        type="button"
        class="detail-copy"
        :title="t('modelPlaza.table.copyModelId', { model: model.name })"
        :aria-label="t('modelPlaza.table.copyModelId', { model: model.name })"
        @click="copyToClipboard(model.name)"
      >
        <Icon :name="copied ? 'check' : 'copy'" size="sm" aria-hidden="true" />
      </button>
    </template>
    <div class="plaza-model-detail min-w-0 space-y-4">
      <header>
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <span class="inline-flex items-center gap-1.5 text-sm font-semibold" :class="platformTextClass(current.model.platform)">
            <PlatformIcon :platform="current.model.platform as GroupPlatform" size="sm" />
            {{ platformLabel(current.model.platform) }}
          </span>
          <span class="break-words text-sm font-medium">{{ current.group.name }}</span>
          <span class="font-mono text-xs surface-muted">
            <del v-if="hasPersonalRate" class="mr-1">{{ current.group.rate_multiplier }}×</del>{{ rate }}×
          </span>
          <span v-if="current.group.is_exclusive" class="detail-tag">{{ t('modelPlaza.badges.exclusive') }}</span>
          <span v-if="current.group.subscription_type === 'subscription'" class="detail-tag">{{ t('modelPlaza.badges.subscription') }}</span>
        </div>
        <p v-if="current.group.description" class="mt-2 break-words text-sm surface-muted">{{ current.group.description }}</p>
      </header>

      <div :key="current.group.id" class="space-y-3">
        <PlazaPriceBreakdown
          :model="current.model" source="paid" :rate="rate"
          :title="hasVariableRate ? t('modelPlaza.catalog.standardPrice') : t('modelPlaza.table.paidPrice')"
        />
        <PlazaPriceBreakdown :model="current.model" source="official" :rate="1" :title="t('modelPlaza.table.officialPrice')" />
        <details v-if="hasRules" class="billing-rules">
          <summary class="cursor-pointer text-sm font-semibold">{{ t('modelPlaza.catalog.billingRules') }}</summary>
          <div class="mt-3 space-y-2 text-sm surface-muted">
            <p v-if="peakWindow">{{ t('modelPlaza.detail.peakNote', { window: peakWindow, multiplier: current.group.peak_rate_multiplier }) }}</p>
            <p v-if="tiersDisabled">{{ t('modelPlaza.detail.longContextDisabledNote') }}</p>
            <p v-if="current.model.long_context_basis">
              {{ t(current.model.long_context_basis === 'marginal' ? 'modelPlaza.table.tierHintMarginal' : 'modelPlaza.table.tierHint') }}
            </p>
            <p v-for="([effort, multiplier]) in efforts" :key="effort">
              {{ t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier }) }}
            </p>
            <template v-if="schedule?.periods.length">
              <p>{{ scheduleNote }}</p>
              <PlazaPriceBreakdown
                v-for="period in schedule.periods" :key="period.start_time"
                :title="formatTimeWindow(period) + ' · ' + periodRate(rate, period) + '×'"
                :model="current.model" :rate="periodRate(rate, period)" source="paid"
              />
            </template>
          </div>
        </details>
      </div>

      <section v-if="comparisons.length > 1" class="space-y-2">
        <h4 class="text-sm font-semibold">{{ t('modelPlaza.catalog.comparison') }}</h4>
        <div class="space-y-1.5">
          <button
            v-for="{ offer, quote } in comparisons"
            :key="offer.group.id"
            type="button"
            class="compare-offer"
            :aria-pressed="current.group.id === offer.group.id"
            @click="selectedGroupId = offer.group.id"
          >
            <span class="flex min-w-0 items-center justify-between gap-2">
              <span class="break-words text-sm font-medium">{{ offer.group.name }}</span>
              <span class="font-mono text-xs surface-muted">{{ quote.rate }}×</span>
            </span>
            <span v-if="offer.model.pricing" class="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs">
              <span v-for="price in quote.prices" :key="price.label">
                <span class="surface-muted">{{ t(price.label) }}</span> {{ price.value }}
              </span>
              <span class="surface-muted">{{ t(quote.unit) }} {{ quote.tier }}</span>
            </span>
            <span v-else class="text-xs surface-muted">{{ t('modelPlaza.detail.noPricing') }}</span>
          </button>
        </div>
      </section>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
import type { GroupPlatform } from '@/types'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { platformLabel, platformTextClass } from '@/utils/platformColors'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import PlazaPriceBreakdown from './PlazaPriceBreakdown.vue'
import { formatTimeWindow, groupRates, modelRate, periodRate, reasoningEffortMultipliers, summarizeOffer, timePricing, usesIndependentMediaRate, type PlazaOffer } from './pricing'

const props = defineProps<{ group: ModelPlazaGroup; model: PlazaModel; offers: PlazaOffer[]; show: boolean }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const appStore = useAppStore()
const selectedGroupId = ref(props.group.id)
watch(() => [props.show, props.group.id, props.model.name, props.model.platform], () => { selectedGroupId.value = props.group.id })
const current = computed(() => props.offers.find(offer => offer.group.id === selectedGroupId.value) ?? props)
const comparisons = computed(() => props.offers.map(offer => ({ offer, quote: summarizeOffer(offer) })))
const rates = computed(() => groupRates(current.value.group))
const rate = computed(() => modelRate(current.value.model, rates.value))
const hasPersonalRate = computed(() => !usesIndependentMediaRate(current.value.model, rates.value)
  && rates.value.userRateMultiplier != null && rates.value.userRateMultiplier !== rates.value.rateMultiplier)
const efforts = computed(() => reasoningEffortMultipliers(current.value.model))
const peakWindow = computed(() => formatPeakRateWindow(current.value.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)))
const tiersDisabled = computed(() => current.value.group.long_context_pricing_enabled === false && (current.value.model.official_pricing?.intervals?.length ?? 0) > 1)
const schedule = computed(() => timePricing(current.value.model))
const scheduleNote = computed(() => {
  if (!schedule.value) return ''
  const key = schedule.value.weekdays_only ? 'modelPlaza.table.timePricingRowHintWeekdays' : 'modelPlaza.table.timePricingRowHint'
  const periodNote = t(key, { timezone: schedule.value.timezone })
  return peakWindow.value ? periodNote + t('modelPlaza.table.timePricingRowHintPeak', {
    window: peakWindow.value, multiplier: current.value.group.peak_rate_multiplier
  }) : periodNote
})
const hasVariableRate = computed(() => !!(peakWindow.value || schedule.value?.periods.length))
const hasRules = computed(() => hasVariableRate.value || tiersDisabled.value || current.value.model.long_context_basis || efforts.value.length)
</script>

<style scoped>
.detail-copy { display: grid; place-items: center; width: var(--control-height); height: var(--control-height); border-radius: var(--surface-radius-control); color: var(--surface-muted); }
.detail-copy:hover, .detail-copy:focus-visible { color: var(--console-accent-readable); background: var(--surface-hover); }
.detail-tag { padding: 2px 6px; border-radius: var(--surface-radius-tag); background: var(--surface-soft); font-size: .75rem; }
.compare-offer { display: block; width: 100%; padding: 8px 12px; text-align: left; border: 1px solid var(--surface-edge); border-radius: var(--surface-radius-control); }
.compare-offer[aria-pressed="true"] { border-color: var(--console-control-edge); background: var(--surface-selected); }
.compare-offer:hover { background: var(--surface-hover); }
.billing-rules { padding: 14px; border-radius: var(--surface-radius-card); background: var(--surface-soft); }
@media (max-width: 639px) {
  :global(.detail-dialog.modal-content:has(.plaza-model-detail)) {
    inset: 0;
    width: 100%;
    height: 100dvh;
    max-height: 100dvh;
    border-radius: 0;
    padding-top: env(safe-area-inset-top);
  }
}
</style>
