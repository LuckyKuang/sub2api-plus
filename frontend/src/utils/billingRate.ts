import { listPlatforms } from '@/constants/platformCatalog'
import type { BillingRatePolicy } from '@/types/billingRate'
import type { SubscriptionPlan } from '@/types/payment'
import type { SaleModelCatalog } from '@/types/billingRate'

export function cloneBillingRatePolicy(value?: BillingRatePolicy | null): BillingRatePolicy {
  return {
    platform_multipliers: { ...value?.platform_multipliers },
    model_multipliers: (value?.model_multipliers ?? []).map(row => ({ ...row })),
  }
}

export const billingRateUnits = ['', 'token', 'image', 'request', 'second', 'minute', 'hour', 'million_characters'] as const
export type BillingRatePolicyError = 'invalidMultiplier' | 'invalidPlatform' | 'invalidModel' | 'invalidUnit' | 'duplicateRule' | 'tooManyRules'
export function billingRatePolicyError(policy: BillingRatePolicy): BillingRatePolicyError | null {
  const platforms = new Set(listPlatforms().map(platform => platform.id))
  const validRate = (value: unknown) => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 999999.9999 && Math.abs(value * 10000 - Math.round(value * 10000)) < 1e-6
  for (const [platform, rate] of Object.entries(policy.platform_multipliers)) {
    if (!platforms.has(platform)) return 'invalidPlatform'
    if (!validRate(rate)) return 'invalidMultiplier'
  }
  if (policy.model_multipliers.length > 256) return 'tooManyRules'
  const seen = new Set<string>()
  for (const row of policy.model_multipliers) {
    if (!platforms.has(row.platform)) return 'invalidPlatform'
    if (!validRate(row.multiplier)) return 'invalidMultiplier'
    const model = row.model
    if (!model || /\s/.test(model) || new TextEncoder().encode(model).length > 256 || (model.includes('*') && (!model.endsWith('*') || model.slice(0, -1).includes('*')))) return 'invalidModel'
    if (!(billingRateUnits as readonly string[]).includes(row.billing_unit ?? '')) return 'invalidUnit'
    const key = `${row.platform}\0${model.toLowerCase()}\0${row.billing_unit ?? ''}`
    if (seen.has(key)) return 'duplicateRule'
    seen.add(key)
  }
  return null
}

export function formatBillingRate(rate: number): string {
  return `×${Number(rate.toPrecision(10))}`
}

export function planRateLabel(plan: SubscriptionPlan): string | null {
  if (!plan.model_catalog || plan.model_catalog.models.length === 0) return null
  const { rate_min: min, rate_max: max } = plan.model_catalog
  if (min == null || max == null || !Number.isFinite(min) || !Number.isFinite(max)) return null
  return min === max ? formatBillingRate(min) : `${formatBillingRate(min)}–${formatBillingRate(max)}`
}

export function saleQuoteChanged(previous: SubscriptionPlan, current: SubscriptionPlan): boolean {
  return previous.price !== current.price || previous.currency !== current.currency ||
    previous.group_id !== current.group_id || previous.validity_days !== current.validity_days || previous.validity_unit !== current.validity_unit ||
    previous.rate_multiplier !== current.rate_multiplier || previous.peak_rate_enabled !== current.peak_rate_enabled ||
    previous.peak_start !== current.peak_start || previous.peak_end !== current.peak_end || previous.peak_rate_multiplier !== current.peak_rate_multiplier ||
    previous.daily_limit_usd !== current.daily_limit_usd || previous.weekly_limit_usd !== current.weekly_limit_usd ||
    previous.monthly_limit_usd !== current.monthly_limit_usd || previous.five_hour_limit_usd !== current.five_hour_limit_usd ||
    previous.model_catalog?.policy_version !== current.model_catalog?.policy_version ||
    previous.model_catalog?.rate_min !== current.model_catalog?.rate_min ||
    previous.model_catalog?.rate_max !== current.model_catalog?.rate_max ||
    saleCatalogFingerprint(previous.model_catalog) !== saleCatalogFingerprint(current.model_catalog)
}

// Compare server-provided commercial terms, never calculate model matching or
// choose a rate in the browser. A refreshed timestamp alone is not a price change.
function saleCatalogFingerprint(catalog?: SaleModelCatalog): string | undefined {
  if (!catalog) return undefined
  return JSON.stringify(catalog.models.map(model => ({
    key: model.offer_key,
    platform: model.platform,
    name: model.name,
    unit: model.billing_unit,
    mode: model.billing_mode,
    status: model.price_status,
    pricing: model.pricing,
    tiers: model.media_tiers,
    serviceTiers: model.service_tier_pricing,
    timePricing: model.time_pricing,
    priceTimeMultiplier: model.price_time_multiplier,
    rate: model.rate_quote && {
      effective: model.rate_quote.effective_multiplier,
      base: model.rate_quote.base_multiplier,
      policy: model.rate_quote.policy_multiplier,
      platform: model.rate_quote.platform_multiplier,
      model: model.rate_quote.model_multiplier,
      calculation: model.rate_quote.rate_calculation,
      peak: model.rate_quote.peak_multiplier,
      source: model.rate_quote.source,
      baseSource: model.rate_quote.base_source,
    },
  })).sort((a, b) => a.key.localeCompare(b.key)))
}
