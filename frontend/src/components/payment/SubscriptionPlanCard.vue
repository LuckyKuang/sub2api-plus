<template>
  <div
    :class="[
      'subscription-plan-card group relative flex min-w-0 flex-col overflow-hidden rounded-2xl border transition-all',
      { 'subscription-plan-catalog': catalog },
      'hover:shadow-xl hover:-translate-y-0.5',
      borderClass,
      'surface-inset dark:surface-inset',
    ]"
  >
    <div class="plan-content flex min-w-0 flex-1 flex-col p-4">
      <!-- Header: name + badge + price -->
      <div class="plan-header mb-3 flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <h3
            :title="plan.name"
            class="h-12 min-w-0 break-words [overflow-wrap:anywhere] text-base font-bold leading-6 text-gray-900 dark:text-white line-clamp-2"
          >
            {{ plan.name }}
          </h3>
          <p v-if="plan.description" class="mt-0.5 text-xs leading-relaxed line-clamp-2 surface-muted">
            {{ plan.description }}
          </p>
        </div>
        <div class="plan-price shrink-0 text-right">
          <div class="flex items-baseline gap-1">
            <span class="text-xs surface-muted">{{ planCurrencySymbol }}</span>
            <span :class="['text-2xl font-extrabold tracking-tight', textClass]">{{ plan.price }}</span>
            <span v-if="plan.currency" class="text-xs font-medium surface-muted">{{ plan.currency }}</span>
          </div>
          <div class="flex items-center justify-end gap-1">
            <span :class="['inline-flex shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium', badgeLightClass]">
              {{ pLabel }}
            </span>
            <span class="text-[11px] surface-muted">/ {{ validitySuffix }}</span>
          </div>
          <div v-if="plan.original_price" class="mt-0.5 flex items-center justify-end gap-1.5">
            <span class="text-xs line-through surface-muted">{{ planCurrencySymbol }}{{ plan.original_price }}<template v-if="plan.currency"> {{ plan.currency }}</template></span>
            <span :class="['rounded px-1 py-0.5 text-[10px] font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
      </div>

      <!-- Group quota info (compact) -->
      <div class="mb-3 grid grid-cols-2 gap-x-3 gap-y-1 rounded-lg surface-inset px-3 py-2 text-xs">
        <div class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.rate') }}</span>
          <span class="font-medium surface-muted">{{ rateDisplay }}</span>
        </div>
        <div v-if="hasPeakRate" class="col-span-2 flex items-center justify-between gap-2">
          <span class="surface-muted">{{ t('payment.planCard.peakRate') }}</span>
          <span class="text-right font-medium text-amber-700 dark:text-amber-300">{{ peakRateDisplay }}</span>
        </div>
        <div v-if="plan.daily_limit_usd != null" class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.dailyLimit') }}</span>
          <span class="font-medium surface-muted">${{ plan.daily_limit_usd }}</span>
        </div>
        <div v-if="plan.weekly_limit_usd != null" class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.weeklyLimit') }}</span>
          <span class="font-medium surface-muted">${{ plan.weekly_limit_usd }}</span>
        </div>
        <div v-if="plan.monthly_limit_usd != null" class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.monthlyLimit') }}</span>
          <span class="font-medium surface-muted">${{ plan.monthly_limit_usd }}</span>
        </div>
        <div v-if="plan.five_hour_limit_usd != null" class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.fiveHourLimit') }}</span>
          <span class="font-medium surface-muted">&#36;{{ plan.five_hour_limit_usd }}</span>
        </div>
        <div v-if="plan.daily_limit_usd == null && plan.weekly_limit_usd == null && plan.monthly_limit_usd == null && plan.five_hour_limit_usd == null" class="flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.quota') }}</span>
          <span class="font-medium surface-muted">{{ t('payment.planCard.unlimited') }}</span>
        </div>
        <div v-if="modelScopeLabels.length > 0" class="col-span-2 flex items-center justify-between">
          <span class="surface-muted">{{ t('payment.planCard.models') }}</span>
          <div class="flex flex-wrap justify-end gap-1">
            <span v-for="scope in modelScopeLabels" :key="scope"
              class="rounded surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
              {{ scope }}
            </span>
          </div>
        </div>
      </div>

      <PlanModelCatalog class="mb-3" :catalog="plan.model_catalog" />

      <!-- Features list (compact) -->
      <div v-if="plan.features.length > 0" class="mb-3 space-y-1">
        <div v-for="feature in plan.features" :key="feature" class="flex items-start gap-1.5">
          <svg :class="['mt-0.5 h-3.5 w-3.5 flex-shrink-0', iconClass]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
          </svg>
          <span class="text-xs surface-muted">{{ feature }}</span>
        </div>
      </div>

      <!-- Subscribe Button -->
      <button
        type="button"
        class="btn btn-primary plan-purchase-action mt-auto min-h-11 w-full rounded-xl py-2.5 text-sm font-semibold shadow-sm transition-all active:scale-[0.98]"
        @click="emit('select', plan)"
      >
        {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { planValiditySuffix } from './validity'
import { currencySymbol } from '@/components/payment/currency'
import PlanModelCatalog from './PlanModelCatalog.vue'
import { planRateLabel } from '@/utils/billingRate'
import {
  platformBadgeLightClass,
  platformBorderClass,
  platformTextClass,
  platformIconClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

const props = defineProps<{
  plan: SubscriptionPlan
  activeSubscriptions?: UserSubscription[]
  catalog?: boolean
}>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const platform = computed(() => props.plan.group_platform || '')
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

// Derived color classes from central config
const borderClass = computed(() => platformBorderClass(platform.value))
const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const textClass = computed(() => platformTextClass(platform.value))
const iconClass = computed(() => platformIconClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => platformLabel(platform.value))

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const rateDisplay = computed(() => {
  return planRateLabel(props.plan) ?? t('billingRates.unknown')
})

const appStore = useAppStore()
const planCurrencySymbol = computed(() => currencySymbol(props.plan.currency || 'USD'))

const hasPeakRate = computed(() => groupHasPeakRate(props.plan))

const peakRateDisplay = computed(() => {
  return formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

const modelScopeLabels = computed(() => {
  if (platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  if (!scopes || scopes.length === 0) return []
  return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
})

const validitySuffix = computed(() => planValiditySuffix(props.plan, t))
</script>

<style scoped>
.subscription-plan-catalog {
  height: 100%;
}

@media (min-width: 1024px) {
  .subscription-plan-catalog .plan-header {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }

  .subscription-plan-catalog .plan-price {
    text-align: left;
  }

  .subscription-plan-catalog .plan-price > div {
    flex-wrap: wrap;
    justify-content: flex-start;
  }

  .subscription-plan-catalog .plan-content {
    padding: 14px;
  }

  .subscription-plan-catalog .plan-content > .grid {
    grid-template-columns: minmax(0, 1fr);
    padding-inline: 10px;
  }

  .subscription-plan-catalog .plan-content > .grid > .col-span-2 {
    grid-column: 1 / -1;
  }
}
</style>
