import type { UserPricingInterval } from '@/api/channels'
import type { ModelPlazaGroup, PlazaModel, PlazaTimePricingPeriod } from '@/api/modelPlaza'
import { BILLING_MODE_IMAGE, BILLING_MODE_TOKEN, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS } from '@/constants/channel'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'

export interface PlazaOffer {
  group: ModelPlazaGroup
  model: PlazaModel
}

export interface PlazaRates {
  rateMultiplier: number
  userRateMultiplier?: number | null
  imageRateIndependent?: boolean
  imageRateMultiplier?: number | null
  videoRateIndependent?: boolean
  videoRateMultiplier?: number | null
}

export function groupRates(group: ModelPlazaGroup): PlazaRates {
  return {
    rateMultiplier: group.rate_multiplier,
    userRateMultiplier: group.user_rate_multiplier,
    imageRateIndependent: group.image_rate_independent,
    imageRateMultiplier: group.image_rate_multiplier,
    videoRateIndependent: group.video_rate_independent,
    videoRateMultiplier: group.video_rate_multiplier
  }
}

export function billingMode(model: PlazaModel) {
  return model.pricing?.billing_mode || BILLING_MODE_TOKEN
}

export function usesIndependentMediaRate(model: PlazaModel, rates: PlazaRates) {
  return (billingMode(model) === BILLING_MODE_IMAGE && rates.imageRateIndependent === true) ||
    (billingMode(model) === BILLING_MODE_VIDEO && rates.videoRateIndependent === true)
}

export function modelRate(model: PlazaModel, rates: PlazaRates) {
  if (billingMode(model) === BILLING_MODE_VIDEO && rates.videoRateIndependent) {
    return Math.max(0, rates.videoRateMultiplier ?? 1)
  }
  if (billingMode(model) === BILLING_MODE_IMAGE && rates.imageRateIndependent) {
    return Math.max(0, rates.imageRateMultiplier ?? 1)
  }
  return rates.userRateMultiplier ?? rates.rateMultiplier
}

export function periodRate(rate: number, period: PlazaTimePricingPeriod) {
  return Math.round(rate * period.multiplier * 1000) / 1000
}

/** Only token billing admits scheduled periods. */
export function timePricing(model: PlazaModel) {
  return billingMode(model) === BILLING_MODE_TOKEN ? model.time_pricing : undefined
}

export function formatTimeWindow(period: PlazaTimePricingPeriod) {
  const clock = (time: string) => time.replace(/^(\d{2}:\d{2}):00$/, '$1')
  return `${clock(period.start_time)}–${clock(period.end_time)}`
}

export function tokenPrice(value: number | null | undefined, rate = 1) {
  return formatScaled(value == null ? null : value * rate, 1_000_000, 2)
}

export function requestPrice(value: number | null | undefined, rate: number) {
  return formatScaled(value == null ? null : value * rate, 1, 2)
}

export function priceUnit(model: PlazaModel) {
  switch (billingMode(model)) {
    case BILLING_MODE_TOKEN: return 'modelPlaza.catalog.perMillion'
    case BILLING_MODE_IMAGE: return 'modelPlaza.table.perUnitImage'
    case BILLING_MODE_VIDEO: return 'modelPlaza.table.perUnitSecond'
    default: return 'modelPlaza.table.perUnitRequest'
  }
}

export function tokenIntervals(model: PlazaModel) {
  const pricing = model.pricing
  if (!pricing) return []
  return [...(pricing.intervals ?? [])].sort((a, b) => a.min_tokens - b.min_tokens)
    .map(interval => resolveIntervalPrices(interval, pricing))
}

export type CachePrices = Pick<UserPricingInterval, 'cache_write_price' | 'cache_write_1h_price' | 'cache_write_12h_price' | 'cache_read_price'>

export function hasCachePrices(prices: CachePrices | null) {
  return prices?.cache_write_price != null || prices?.cache_write_1h_price != null ||
    prices?.cache_write_12h_price != null || prices?.cache_read_price != null
}

/** Use the flat cache quote only when no tier provides any cache pricing. */
export function hasTierCachePricing(intervals: UserPricingInterval[]) {
  return intervals.some(interval => hasCachePrices(interval) ||
    interval.cache_write_multiplier != null || interval.cache_read_multiplier != null)
}

export function officialIntervals(model: PlazaModel) {
  return [...(model.official_pricing?.intervals ?? [])].sort((a, b) => a.min_tokens - b.min_tokens)
}

export function requestIntervals(model: PlazaModel) {
  return (model.pricing?.intervals ?? []).filter(interval => interval.per_request_price != null)
}

export function tierLabel(interval: UserPricingInterval) {
  if (interval.tier_label) return interval.tier_label
  const { min_tokens: min, max_tokens: max } = interval
  return max == null ? `>${formatTokenCount(min)}` : `≤${formatTokenCount(max)}`
}

function formatTokenCount(count: number) {
  const trimmed = (value: number) => Math.round(value * 100) / 100
  if (count >= 1_000_000) return `${trimmed(count / 1_000_000)}M`
  if (count >= 1_000) return `${trimmed(count / 1_000)}K`
  return String(count)
}

export function reasoningEffortMultipliers(model: PlazaModel): [string, number][] {
  const multipliers = model.pricing?.reasoning_effort_multipliers
  return REASONING_EFFORT_LEVELS.flatMap(effort => {
    const multiplier = multipliers?.[effort]
    return typeof multiplier === 'number' && Number.isFinite(multiplier) && multiplier > 0
      ? [[effort, multiplier] as [string, number]]
      : []
  })
}

type BandPrices = Pick<UserPricingInterval, 'input_price' | 'output_price' | 'cache_write_price' | 'cache_write_1h_price' | 'cache_write_12h_price' | 'cache_read_price'> &
  Partial<Pick<UserPricingInterval, 'per_request_price'>>

export interface PriceBand {
  label: string
  prices: BandPrices
}

/** Paid and catalog tiers are independent, including when the group disables long-context billing. */
export function priceBands(model: PlazaModel, source: 'paid' | 'official'): PriceBand[] {
  const base = source === 'official' ? model.official_pricing : model.pricing
  if (!base) return []
  const intervals = source === 'official' ? officialIntervals(model)
    : billingMode(model) === BILLING_MODE_TOKEN ? tokenIntervals(model) : requestIntervals(model)
  return intervals.length
    ? intervals.map(interval => ({ label: tierLabel(interval), prices: interval }))
    : [{ label: '', prices: base }]
}

/** Always label the actual displayed tier. Its first price need not be the cheapest. */
export function summarizeOffer({ group, model }: PlazaOffer) {
  const band = priceBands(model, 'paid')[0]
  const rate = modelRate(model, groupRates(group))
  const isToken = billingMode(model) === BILLING_MODE_TOKEN
  return {
    rate,
    tier: band?.label,
    unit: priceUnit(model),
    prices: isToken ? [
      { label: 'modelPlaza.table.input', value: tokenPrice(band?.prices.input_price, rate) },
      { label: 'modelPlaza.table.output', value: tokenPrice(band?.prices.output_price, rate) }
    ] : [
      { label: 'modelPlaza.catalog.paid', value: requestPrice(band?.prices.per_request_price, rate) }
    ]
  }
}
