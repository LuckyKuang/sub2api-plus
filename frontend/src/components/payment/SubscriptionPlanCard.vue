<template>
  <div
    :class="[
      'subscription-plan-card card group relative flex min-w-0 flex-col overflow-hidden transition-all',
      { 'subscription-plan-catalog': catalog },
      'hover:shadow-xl hover:-translate-y-0.5',
      borderClass,
    ]"
  >
    <div class="plan-content flex min-w-0 flex-1 flex-col p-3.5">
      <!-- Header: name + badge + price -->
      <div class="plan-header mb-2.5 flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <h3
            :title="plan.name"
            class="h-10 min-w-0 break-words [overflow-wrap:anywhere] text-[15px] font-bold leading-5 text-gray-900 dark:text-white line-clamp-2"
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
            <span :class="['text-xl font-extrabold tracking-tight tabular-nums', textClass]">{{ plan.price }}</span>
            <span v-if="plan.currency" class="text-xs font-medium surface-muted">{{ plan.currency }}</span>
          </div>
          <div class="flex items-center justify-end gap-1">
            <span :class="['inline-flex shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-medium', badgeLightClass]">
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

      <!-- Group quota info: rate tag top-left, then one labelled bar per limit -->
      <div class="plan-quota surface-subcard mb-2.5 space-y-1.5 px-2.5 py-2 text-xs">
        <div class="flex items-center gap-2">
          <span class="surface-muted">{{ t('payment.planCard.rate') }}</span>
          <span
            v-if="rateValue"
            class="rounded-md bg-primary-100 px-1.5 py-0.5 text-xs font-bold tabular-nums text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
          >{{ rateValue }}</span>
          <span v-else class="font-medium surface-muted">{{ t('billingRates.unknown') }}</span>
        </div>
        <div v-if="hasPeakRate" class="flex items-center justify-between gap-2">
          <span class="surface-muted">{{ t('payment.planCard.peakRate') }}</span>
          <span class="text-right font-medium text-amber-700 dark:text-amber-300">{{ peakRateDisplay }}</span>
        </div>
        <div v-for="bar in limitBars" :key="bar.key" class="space-y-1" :data-limit="bar.key">
          <div class="flex items-center justify-between gap-2">
            <span class="font-medium surface-muted">{{ bar.label }}</span>
            <span class="tabular-nums surface-muted">{{ bar.amount }}</span>
          </div>
          <div class="relative h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
            <div
              class="console-data-mark absolute inset-y-0 left-0 rounded-full transition-all duration-300"
              :class="bar.barClass"
              :style="{ width: bar.width }"
            ></div>
          </div>
        </div>
        <div v-if="limitBars.length === 0" class="flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-50 to-teal-50 px-2 py-1 dark:from-emerald-900/20 dark:to-teal-900/20">
          <span class="text-base text-emerald-600 dark:text-emerald-400">∞</span>
          <span class="font-medium text-emerald-700 dark:text-emerald-300">{{ t('payment.planCard.unlimited') }}</span>
        </div>
        <div v-if="modelScopeLabels.length > 0" class="flex items-center justify-between gap-2">
          <span class="surface-muted">{{ t('payment.planCard.models') }}</span>
          <div class="flex flex-wrap justify-end gap-1">
            <span v-for="scope in modelScopeLabels" :key="scope"
              class="rounded surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
              {{ scope }}
            </span>
          </div>
        </div>
      </div>

      <PlanModelCatalog class="mb-2.5" dialog :catalog="plan.model_catalog" />

      <!-- Features list (compact) -->
      <div v-if="plan.features.length > 0" class="mb-2.5 space-y-1">
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
        class="btn btn-primary btn-lg plan-purchase-action mt-auto w-full text-sm font-semibold active:scale-[0.98]"
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

// Prefer the model-catalog quote; when the catalog has no priced model, fall
// back to the group/personal multiplier the API sends. null means truly unknown.
const rateValue = computed(() => {
  const catalog = props.plan.model_catalog
  const min = catalog?.models.length ? catalog.rate_min : null
  const max = catalog?.models.length ? catalog.rate_max : null
  if (min != null && max != null && Number.isFinite(min) && Number.isFinite(max)) {
    return min === max ? formatRateX(min) : `${formatRateX(min)}–${formatRateX(max)}`
  }
  // Composite groups price per model, so a bare group multiplier would mislead.
  if (platform.value === 'composite') return null
  const base = props.plan.rate_multiplier
  return typeof base === 'number' && Number.isFinite(base) && base > 0 ? formatRateX(base) : null
})

function formatRateX(rate: number): string {
  return `${Number(rate.toPrecision(10))}x`
}

// Monthly is the most saturated; weekly, daily, 5h progressively fainter.
const LIMIT_DEFS = [
  { key: 'monthly', field: 'monthly_limit_usd', label: 'monthlyLimit', barClass: 'bg-violet-600 dark:bg-violet-500' },
  { key: 'weekly', field: 'weekly_limit_usd', label: 'weeklyLimit', barClass: 'bg-blue-500 dark:bg-blue-500/90' },
  { key: 'daily', field: 'daily_limit_usd', label: 'dailyLimit', barClass: 'bg-sky-400 dark:bg-sky-500/80' },
  { key: 'fiveHour', field: 'five_hour_limit_usd', label: 'fiveHourLimit', barClass: 'bg-slate-400 dark:bg-slate-500' },
] as const

function formatUsd(value: number): string {
  return `$${Number(value.toFixed(2))}`
}

// A plan card describes the plan, not the buyer's current usage (that lives on
// the subscriptions page), so each bar shows its limit relative to the largest
// limit on the card: monthly is full and every colour stays visible.
const MIN_BAR_RATIO = 0.03
const limitBars = computed(() => {
  const rows = LIMIT_DEFS
    .map(def => ({ def, limit: props.plan[def.field] }))
    .filter((row): row is { def: (typeof LIMIT_DEFS)[number]; limit: number } => typeof row.limit === 'number' && row.limit > 0)
  const largest = Math.max(0, ...rows.map(row => row.limit))
  return rows.map(({ def, limit }) => ({
    key: def.key,
    label: t(`payment.planCard.${def.label}`),
    amount: formatUsd(limit),
    barClass: def.barClass,
    width: `${Math.round(Math.min(Math.max(limit / largest, MIN_BAR_RATIO), 1) * 100)}%`,
  }))
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
    gap: 8px;
  }

  .subscription-plan-catalog .plan-price {
    text-align: left;
  }

  .subscription-plan-catalog .plan-price > div {
    flex-wrap: wrap;
    justify-content: flex-start;
  }

  .subscription-plan-catalog .plan-content {
    padding: 12px;
  }

  .subscription-plan-catalog .plan-quota {
    padding-inline: 8px;
  }
}
</style>
