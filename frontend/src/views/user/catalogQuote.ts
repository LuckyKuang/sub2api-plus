import type { CatalogBillingUnit, CatalogGroup, CatalogOffer } from '@/api/channels'

export interface CatalogPriceRange { min: number; max: number }

export function rateSource(_group: CatalogGroup | undefined, offer: CatalogOffer): 'media' | 'personal' | 'group' {
  return offer.rate_quote?.base_source ?? 'group'
}

export function effectiveRate(_group: CatalogGroup | undefined, offer: CatalogOffer): number {
  return offer.rate_quote?.effective_multiplier ?? Number.NaN
}

export function effectivePriceRate(group: CatalogGroup | undefined, offer: CatalogOffer): number {
  return effectiveRate(group, offer) * (offer.price_time_multiplier ?? 1)
}

const moneyFormatter = new Intl.NumberFormat('en-US', { maximumSignificantDigits: 6 })

export function catalogMoney(price: number | null | undefined, rate = 1, perMillion = false): string {
  if (price == null || !Number.isFinite(price)) return '—'
  const value = price * rate * (perMillion ? 1_000_000 : 1)
  if (!Number.isFinite(value)) return '—'
  if (value === 0) return '$0'
  return '$' + moneyFormatter.format(value)
}

export function catalogRange(range: CatalogPriceRange | null, unit: CatalogBillingUnit): string {
  if (!range) return '—'
  const min = catalogMoney(range.min, 1, unit === 'token'), max = catalogMoney(range.max, 1, unit === 'token')
  return min === max ? min : `${min}–${max}`
}
