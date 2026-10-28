import type { CatalogOffer } from '@/api/channels'

export interface BillingModelMultiplier {
  platform: string
  model: string
  multiplier: number
  billing_unit?: string
}

export interface BillingRatePolicy {
  platform_multipliers: Record<string, number>
  model_multipliers: BillingModelMultiplier[]
}

/** Resolved by the same backend policy used when charging a request. */
export interface BillingRateQuote {
  base_multiplier: number
  platform_multiplier: number
  model_multiplier: number
  rate_calculation: 'product_v2' | 'override_v1'
  peak_scope: 'all_meters' | 'token_only'
  policy_multiplier: number
  peak_multiplier: number
  effective_multiplier: number
  source: 'default' | 'platform' | 'model'
  base_source: 'group' | 'personal' | 'media'
  policy_version: number
  platform: string
  model: string
  billing_unit: string
  as_of: string
}

/** Safe sale preview, independent of the purchaser's current group access. */
export interface SaleModelCatalog {
  policy_version: number
  as_of: string
  models: CatalogOffer[]
  platforms: string[]
  rate_min: number | null
  rate_max: number | null
}
