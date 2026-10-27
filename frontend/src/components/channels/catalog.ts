import type { CatalogBillingUnit, CatalogGroup, CatalogOffer } from '@/api/channels'

export interface CatalogFilters {
  platform: string
  search: string
  sort: 'name' | 'price'
}
export interface CatalogQuote { group: CatalogGroup; offer: CatalogOffer }
export interface CatalogPriceRange { min: number; max: number }
export interface CatalogUnitPrice {
  unit: CatalogBillingUnit
  input: CatalogPriceRange | null
  output: CatalogPriceRange | null
  incomplete: boolean
}
export interface CatalogModel {
  key: string
  name: string
  platform: CatalogOffer['platform']
  quotes: CatalogQuote[]
  prices: CatalogUnitPrice[]
  tiered: 'none' | 'some' | 'all'
  dynamic: 'none' | 'some' | 'all'
}

export const defaultCatalogFilters = (): CatalogFilters => ({ platform: '', search: '', sort: 'name' })
export const modelIdentity = (offer: CatalogOffer) => `${offer.platform}\0${offer.name.toLowerCase()}`
export function rateSource(_group: CatalogGroup | undefined, offer: CatalogOffer): 'media' | 'personal' | 'group' {
  return offer.rate_quote?.base_source ?? 'group'
}
export function effectiveRate(_group: CatalogGroup | undefined, offer: CatalogOffer): number {
  return offer.rate_quote?.effective_multiplier ?? Number.NaN
}
export const hasTieredPricing = ({ group, offer }: CatalogQuote) => group.long_context_pricing_enabled && !!(offer.pricing?.intervals.length || offer.media_tiers?.length)
export const hasDynamicPricing = ({ group, offer }: CatalogQuote) => group.peak_rate_enabled || !!offer.time_pricing
const units: CatalogBillingUnit[] = ['token', 'image', 'request', 'video', 'second', 'minute', 'hour', 'million_characters', 'unknown']
function coverage(quotes: CatalogQuote[], predicate: (quote: CatalogQuote) => boolean): CatalogModel['tiered'] {
  const count = quotes.filter(predicate).length
  return count === 0 ? 'none' : count === quotes.length ? 'all' : 'some'
}
export function quotePrice({ group, offer }: CatalogQuote, output: boolean): number | null {
  // An incomplete token card can still declare one known dimension. Other
  // unknown reasons cannot advertise a request-independent unit price.
  if (offer.billing_unit === 'unknown' || (offer.price_status !== 'resolved' && offer.price_reason !== 'pricing_unavailable')) return null
  const price = offer.billing_unit === 'token' ? (output ? offer.pricing?.output_price : offer.pricing?.input_price) : offer.pricing?.per_request_price
  if (price == null) return null
  const value = price * effectivePriceRate(group, offer)
  return Number.isFinite(value) ? value : null
}
export function effectivePriceRate(group: CatalogGroup | undefined, offer: CatalogOffer): number {
  return effectiveRate(group, offer) * (offer.price_time_multiplier ?? 1)
}
function priceRange(values: (number | null)[]): CatalogPriceRange | null {
  const known = values.filter((v): v is number => v !== null)
  return known.length ? { min: Math.min(...known), max: Math.max(...known) } : null
}

// Aggregate only the already-authorized response. Quotes retain their original
// group, source, rates and conditions; no representative quote replaces them.
export function catalogModels(groups: CatalogGroup[]): CatalogModel[] {
  const models = new Map<string, CatalogQuote[]>()
  for (const group of groups) for (const offer of group.models) {
    const key = modelIdentity(offer)
    const quotes = models.get(key) || []
    quotes.push({ group, offer })
    models.set(key, quotes)
  }
  return Array.from(models, ([key, quotes]) => {
    quotes.sort((a, b) => a.group.name.toLowerCase().localeCompare(b.group.name.toLowerCase()) || a.group.id - b.group.id || a.offer.offer_key.localeCompare(b.offer.offer_key))
    const representative = [...quotes].sort((a, b) => a.offer.name.localeCompare(b.offer.name))[0]!.offer
    const prices = units.flatMap(unit => {
      const matching = quotes.filter(q => q.offer.billing_unit === unit)
      if (!matching.length) return []
      const inputs = matching.map(q => quotePrice(q, false))
      const outputs = unit === 'token' ? matching.map(q => quotePrice(q, true)) : []
      return [{ unit, input: priceRange(inputs), output: priceRange(outputs), incomplete: [...inputs, ...outputs].some(p => p === null) }]
    })
    return { key, name: representative.name, platform: representative.platform, quotes, prices, tiered: coverage(quotes, hasTieredPricing), dynamic: coverage(quotes, hasDynamicPricing) }
  })
}

export function normalizeCatalogFilters(models: CatalogModel[], filters: CatalogFilters): void {
  if (filters.platform && !models.some(m => m.platform === filters.platform)) filters.platform = ''
}
export function visibleModels(models: CatalogModel[], filters: CatalogFilters): CatalogModel[] {
  const query = filters.search.trim().toLowerCase()
  return models.filter(m => (!filters.platform || m.platform === filters.platform) && (!query || m.name.toLowerCase().includes(query))).sort((a, b) => {
    if (filters.sort === 'price') {
      // Mixed-unit cards follow single-unit cards; never pick one arbitrary unit.
      const au = a.prices.length === 1 ? units.indexOf(a.prices[0]!.unit) : units.length
      const bu = b.prices.length === 1 ? units.indexOf(b.prices[0]!.unit) : units.length
      if (au !== bu) return au - bu
      const ap = a.prices.length === 1 ? a.prices[0]!.input?.min : undefined
      const bp = b.prices.length === 1 ? b.prices[0]!.input?.min : undefined
      if (ap == null && bp != null) return 1
      if (ap != null && bp == null) return -1
      if (ap != null && bp != null && ap !== bp) return ap - bp
    }
    return a.name.localeCompare(b.name) || a.platform.localeCompare(b.platform) || a.key.localeCompare(b.key)
  })
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
