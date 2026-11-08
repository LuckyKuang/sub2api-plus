import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { planValiditySuffix } from './paymentValidity'
import { currencySymbol } from './paymentCurrency'
import PlanModelCatalog from './PlanModelCatalog'
import { planRateLabel } from '@/utils/billingRate'
import {
  platformBadgeLightClass,
  platformBorderClass,
  platformTextClass,
  platformIconClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

export interface SubscriptionPlanCardProps {
  plan: SubscriptionPlan
  activeSubscriptions?: UserSubscription[]
  catalog?: boolean
  onSelect: (plan: SubscriptionPlan) => void
}

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

export function SubscriptionPlanCard({ plan, activeSubscriptions, catalog = false, onSelect }: SubscriptionPlanCardProps) {
  const { t } = useTranslation()
  const appStore = useAppStore()

  const platform = plan.group_platform || ''
  const isRenewal = activeSubscriptions?.some(s => s.group_id === plan.group_id && s.status === 'active') ?? false

  const borderClass = platformBorderClass(platform)
  const badgeLightClass = platformBadgeLightClass(platform)
  const textClass = platformTextClass(platform)
  const iconClass = platformIconClass(platform)
  const discountClass = platformDiscountClass(platform)
  const pLabel = platformLabel(platform)

  let discountText = ''
  if (plan.original_price && plan.original_price > 0) {
    const pct = Math.round((1 - plan.price / plan.original_price) * 100)
    discountText = pct > 0 ? `-${pct}%` : ''
  }

  const rateDisplay = planRateLabel(plan) ?? t('billingRates.unknown')
  const planCurrencySymbol = currencySymbol(plan.currency || 'USD')
  const hasPeakRate = groupHasPeakRate(plan)
  const peakRateDisplay = formatPeakRateWindow(plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))

  const modelScopeLabels = useMemo(() => {
    if (platform !== 'antigravity') return []
    const scopes = plan.supported_model_scopes
    if (!scopes || scopes.length === 0) return []
    return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
  }, [platform, plan.supported_model_scopes])

  const validitySuffix = planValiditySuffix(plan, t)

  return (
    <div
      className={cn(
        'subscription-plan-card group relative flex min-w-0 flex-col overflow-hidden rounded-2xl border transition-all',
        catalog && 'subscription-plan-catalog',
        'hover:shadow-xl hover:-translate-y-0.5',
        borderClass,
        'surface-inset dark:surface-inset',
      )}
    >
      <div className="plan-content flex min-w-0 flex-1 flex-col p-4">
        {/* Header: name + badge + price */}
        <div className="plan-header mb-3 flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <h3
              title={plan.name}
              className="h-12 min-w-0 break-words [overflow-wrap:anywhere] text-base font-bold leading-6 text-gray-900 dark:text-white line-clamp-2"
            >
              {plan.name}
            </h3>
            {plan.description && (
              <p className="mt-0.5 text-xs leading-relaxed line-clamp-2 surface-muted">
                {plan.description}
              </p>
            )}
          </div>
          <div className="plan-price shrink-0 text-right">
            <div className="flex items-baseline gap-1">
              <span className="text-xs surface-muted">{planCurrencySymbol}</span>
              <span className={cn('text-2xl font-extrabold tracking-tight', textClass)}>{plan.price}</span>
              {plan.currency && <span className="text-xs font-medium surface-muted">{plan.currency}</span>}
            </div>
            <div className="flex items-center justify-end gap-1">
              <span className={cn('inline-flex shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium', badgeLightClass)}>
                {pLabel}
              </span>
              <span className="text-[11px] surface-muted">/ {validitySuffix}</span>
            </div>
            {plan.original_price && (
              <div className="mt-0.5 flex items-center justify-end gap-1.5">
                <span className="text-xs line-through surface-muted">{planCurrencySymbol}{plan.original_price}{plan.currency ? ` ${plan.currency}` : ''}</span>
                <span className={cn('rounded px-1 py-0.5 text-[10px] font-semibold', discountClass)}>{discountText}</span>
              </div>
            )}
          </div>
        </div>

        {/* Group quota info (compact) */}
        <div className="mb-3 grid grid-cols-2 gap-x-3 gap-y-1 rounded-lg surface-inset px-3 py-2 text-xs">
          <div className="flex items-center justify-between">
            <span className="surface-muted">{t('payment.planCard.rate')}</span>
            <span className="font-medium surface-muted">{rateDisplay}</span>
          </div>
          {hasPeakRate && (
            <div className="col-span-2 flex items-center justify-between gap-2">
              <span className="surface-muted">{t('payment.planCard.peakRate')}</span>
              <span className="text-right font-medium text-amber-700 dark:text-amber-300">{peakRateDisplay}</span>
            </div>
          )}
          {plan.daily_limit_usd != null && (
            <div className="flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.dailyLimit')}</span>
              <span className="font-medium surface-muted">${plan.daily_limit_usd}</span>
            </div>
          )}
          {plan.weekly_limit_usd != null && (
            <div className="flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.weeklyLimit')}</span>
              <span className="font-medium surface-muted">${plan.weekly_limit_usd}</span>
            </div>
          )}
          {plan.monthly_limit_usd != null && (
            <div className="flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.monthlyLimit')}</span>
              <span className="font-medium surface-muted">${plan.monthly_limit_usd}</span>
            </div>
          )}
          {plan.five_hour_limit_usd != null && (
            <div className="flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.fiveHourLimit')}</span>
              <span className="font-medium surface-muted">&#36;{plan.five_hour_limit_usd}</span>
            </div>
          )}
          {plan.daily_limit_usd == null && plan.weekly_limit_usd == null && plan.monthly_limit_usd == null && plan.five_hour_limit_usd == null && (
            <div className="flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.quota')}</span>
              <span className="font-medium surface-muted">{t('payment.planCard.unlimited')}</span>
            </div>
          )}
          {modelScopeLabels.length > 0 && (
            <div className="col-span-2 flex items-center justify-between">
              <span className="surface-muted">{t('payment.planCard.models')}</span>
              <div className="flex flex-wrap justify-end gap-1">
                {modelScopeLabels.map(scope => (
                  <span key={scope} className="rounded surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
                    {scope}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>

        <PlanModelCatalog catalog={plan.model_catalog} />

        {/* Features list (compact) */}
        {plan.features.length > 0 && (
          <div className="mb-3 space-y-1">
            {plan.features.map(feature => (
              <div key={feature} className="flex items-start gap-1.5">
                <svg className={cn('mt-0.5 h-3.5 w-3.5 flex-shrink-0', iconClass)} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                </svg>
                <span className="text-xs surface-muted">{feature}</span>
              </div>
            ))}
          </div>
        )}

        {/* Subscribe Button */}
        <button
          type="button"
          className="btn btn-primary plan-purchase-action mt-auto min-h-11 w-full rounded-xl py-2.5 text-sm font-semibold shadow-sm transition-all active:scale-[0.98]"
          onClick={() => onSelect(plan)}
        >
          {isRenewal ? t('payment.renewNow') : t('payment.subscribeNow')}
        </button>
      </div>
    </div>
  )
}

export default SubscriptionPlanCard
