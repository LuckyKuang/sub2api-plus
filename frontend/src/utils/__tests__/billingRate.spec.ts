import { describe, expect, it } from 'vitest'
import { formatBillingRate, planRateLabel, saleQuoteChanged } from '../billingRate'
import type { SubscriptionPlan } from '@/types/payment'
import { offer, pricing, rateQuote } from '@/components/channels/__tests__/catalogFixtures'

describe('server billing quotes', () => {
  it('formats zero and actual quote ranges without substituting the group default', () => {
    expect(formatBillingRate(0)).toBe('×0')
    expect(planRateLabel({ rate_multiplier: 9, model_catalog: { rate_min: 0, rate_max: 2, models: [offer('free', { rate_quote: rateQuote(0) }), offer('paid', { rate_quote: rateQuote(2) })], platforms: ['openai'], policy_version: 1, as_of: '' } } as unknown as SubscriptionPlan)).toBe('×0–×2')
    expect(planRateLabel({ rate_multiplier: 9, model_catalog: { rate_min: null, rate_max: null, models: [], platforms: [], policy_version: 1, as_of: '' } } as unknown as SubscriptionPlan)).toBe(null)
    expect(planRateLabel({ rate_multiplier: 1 } as unknown as SubscriptionPlan)).toBeNull()
    expect(planRateLabel({ rate_multiplier: 1, model_catalog: { rate_min: 1, rate_max: 1, models: [], platforms: [], policy_version: 1, as_of: '' } } as unknown as SubscriptionPlan)).toBeNull()
  })
  it('requires reviewing a refreshed price, policy or current effective rate before creating an order', () => {
    const before = { id: 1, price: 10, currency: 'USD', model_catalog: { policy_version: 2, rate_min: 1, rate_max: 2, models: [], platforms: ['openai'], as_of: '2026-10-26T12:00:00Z' } } as unknown as SubscriptionPlan
    expect(saleQuoteChanged(before, { ...before })).toBe(false)
    expect(saleQuoteChanged(before, { ...before, price: 12 })).toBe(true)
    expect(saleQuoteChanged(before, { ...before, model_catalog: { ...before.model_catalog!, policy_version: 3 } })).toBe(true)
    expect(saleQuoteChanged(before, { ...before, model_catalog: { ...before.model_catalog!, rate_max: 3 } })).toBe(true)
  })
  it('detects base-price and time-factor changes while ignoring response timestamps', () => {
    const before = { id: 1, price: 10, model_catalog: { policy_version: 2, rate_min: 1, rate_max: 1, models: [offer()], platforms: ['openai'], as_of: '2026-10-26T12:00:00Z' } } as unknown as SubscriptionPlan
    const current = { ...before, model_catalog: { ...before.model_catalog!, as_of: '2026-10-26T12:01:00Z', models: [offer('model', { rate_quote: rateQuote(1, { as_of: '2026-10-26T12:01:00Z' }) })] } }
    expect(saleQuoteChanged(before, current)).toBe(false)
    expect(saleQuoteChanged(before, { ...current, model_catalog: { ...current.model_catalog, models: [offer('model', { price_time_multiplier: 0.5 })] } })).toBe(true)
    expect(saleQuoteChanged(before, { ...current, model_catalog: { ...current.model_catalog, models: [offer('model', { pricing: pricing(4e-6) })] } })).toBe(true)
  })
})
