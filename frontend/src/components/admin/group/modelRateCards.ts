import type { PricingFormEntry } from '@/components/admin/channel/types'
import type { BillingRatePolicy } from '@/types/billingRate'
import { cloneBillingRatePolicy } from '@/utils/billingRate'

/** Empty price cards configure only policy rates and must not shadow inherited prices. */
export function hasGroupPriceOverride(entry: PricingFormEntry): boolean {
  return [entry.input_price, entry.output_price, entry.cache_write_price, entry.cache_write_1h_price,
    entry.cache_read_price, entry.image_input_price, entry.image_output_price, entry.per_request_price,
    entry.fast_multiplier, entry.flex_multiplier]
    .some(value => value !== null && value !== undefined && value !== '') ||
    Object.values(entry.reasoning_effort_multipliers ?? {}).some(value => value !== '' && value !== null && value !== undefined) ||
    (entry.billing_mode !== 'token' && entry.intervals.length > 0)
}

export function modelRateCardKeys(entries: PricingFormEntry[], groupPlatform: string): string[] {
  return entries.flatMap(entry => {
    const platform = groupPlatform === 'composite' ? entry.platform : groupPlatform
    if (!platform || platform === 'composite') return []
    return entry.models.map(model => `${platform}:${model.toLowerCase()}`)
  })
}

export function removeModelCardRates(policy: BillingRatePolicy, entry: PricingFormEntry, groupPlatform: string): BillingRatePolicy {
  const next = cloneBillingRatePolicy(policy)
  const keys = new Set(modelRateCardKeys([entry], groupPlatform))
  next.model_multipliers = next.model_multipliers.filter(row => !keys.has(`${row.platform}:${row.model.toLowerCase()}`))
  return next
}
