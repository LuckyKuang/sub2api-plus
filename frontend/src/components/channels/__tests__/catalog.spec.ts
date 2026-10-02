import { describe, expect, it } from 'vitest'
import { catalogModels, catalogRange, catalogMoney, defaultCatalogFilters, effectiveRate, normalizeCatalogFilters, rateSource, visibleModels } from '../catalog'
import { group, offer, pricing } from './catalogFixtures'

describe('model-centric available catalog', () => {
  it('merges case-insensitive model IDs within a platform and retains all authorized quotes', () => {
    const models = catalogModels([group(1, [offer('SAME', { offer_key: 'one' })]), group(2, [offer('same', { offer_key: 'two' }), offer('same', { offer_key: 'three', platform: 'anthropic' })]), group(3, [])])
    expect(models).toHaveLength(2)
    const openai = models.find(m => m.platform === 'openai')!
    expect(openai.quotes.map(q => q.offer.offer_key)).toEqual(['one', 'two'])
    expect(openai.key).not.toBe(models.find(m => m.platform === 'anthropic')!.key)
    expect(catalogModels([group(2, [offer('same')])])[0]!.key).toBe(openai.key)
  })
  it('ranges effective prices independently, preserving zero personal rates and media precedence', () => {
    const models = catalogModels([group(1, [offer()], { rate_multiplier: 2 }), group(2, [offer()], { rate_multiplier: 9, user_rate_multiplier: 0 })])
    expect(models[0]!.prices[0]).toMatchObject({ unit: 'token', input: { min: 0, max: 4e-6 }, output: { min: 0, max: 2e-5 }, incomplete: false })
    expect(catalogRange(models[0]!.prices[0]!.input, 'token')).toBe('$0–$4')
    const g = group(1, [], { user_rate_multiplier: 0, image_rate_independent: true, image_rate_multiplier: 0.5, video_rate_independent: true, video_rate_multiplier: 0.7 })
    expect(effectiveRate(g, offer())).toBe(0)
    expect(rateSource(g, offer())).toBe('personal')
    expect(effectiveRate(g, offer('image', { billing_mode: 'image' }))).toBe(0.5)
    expect(effectiveRate(g, offer('video', { billing_mode: 'video' }))).toBe(0.7)
    expect(rateSource(g, offer('image', { billing_mode: 'image' }))).toBe('media')
    expect(effectiveRate(group(1, [], { user_rate_multiplier: -1 }), offer())).toBe(0)
    expect(effectiveRate(group(1, [], { image_rate_independent: true, image_rate_multiplier: -1 }), offer('image', { billing_mode: 'image' }))).toBe(0)
  })
  it('keeps units separate and excludes unknown quotes without calling them free', () => {
    const models = catalogModels([group(1, [offer()]), group(2, [offer('model', { billing_mode: 'image', billing_unit: 'image', pricing: { ...pricing(), per_request_price: 0.1 } })]), group(3, [offer('model', { price_status: 'unknown', pricing: null })], { user_rate_multiplier: 0 }), group(4, [offer('model', { pricing: { ...pricing(), per_request_price: 0.01 } })])])
    expect(models).toHaveLength(1)
    expect(models[0]!.prices).toHaveLength(2)
    expect(models[0]!.prices[0]).toMatchObject({ input: { min: 2e-6, max: 2e-6 }, incomplete: true })
    expect(models[0]!.prices[1]).toMatchObject({ unit: 'image', input: { min: 0.1, max: 0.1 }, incomplete: false })
    expect(catalogRange(null, 'token')).toBe('—')
    expect(catalogMoney(0)).toBe('$0')
    expect(catalogMoney(1e-11)).not.toBe('$0')
    expect(catalogMoney(1, Number.NaN)).toBe('—')
    expect(catalogMoney(Number.MAX_VALUE, 2)).toBe('—')
  })
  it('uses token prices even when a configured card retains an unused per-request field', () => {
    const model = catalogModels([group(1, [offer('token', { pricing: { ...pricing(6e-6), per_request_price: 99 } })])])[0]!
    expect(model.prices[0]).toMatchObject({ input: { min: 6e-6, max: 6e-6 }, incomplete: false })
  })
  it('distinguishes partial conditions and gates tiers on each group switch', () => {
    const m = offer('model', { media_tiers: [{ label: '2K', unit: 'image', price: 0.1 }] })
    const groups = [group(1, [m], { long_context_pricing_enabled: true, peak_rate_enabled: true }), group(2, [m], { long_context_pricing_enabled: false })]
    expect(catalogModels(groups)[0]).toMatchObject({ tiered: 'some', dynamic: 'some' })
    expect(catalogModels(groups.slice(0, 1))[0]).toMatchObject({ tiered: 'all', dynamic: 'all' })
    expect(catalogModels(groups.slice(1))[0]).toMatchObject({ tiered: 'none', dynamic: 'none' })
  })
  it('filters only models and platforms without trimming the quote set', () => {
    const models = catalogModels([group(1, [offer('Keep')]), group(2, [offer('keep'), offer('Other', { platform: 'anthropic' })], { platform: 'composite' })])
    const filters = { ...defaultCatalogFilters(), search: '  kEEp  ' }
    const result = visibleModels(models, filters)
    expect(result).toHaveLength(1)
    expect(result[0]!.quotes).toHaveLength(2)
    filters.search = 'Source channel'
    expect(visibleModels(models, filters)).toEqual([])
    filters.platform = 'removed'
    normalizeCatalogFilters(models, filters)
    expect(filters.platform).toBe('')
    expect(filters.search).toBe('Source channel')
  })
  it('sorts single-unit cards by minimum effective price and leaves mixed units separate', () => {
    const models = catalogModels([group(1, [offer('paid'), offer('unknown', { pricing: null, price_status: 'unknown' }), offer('zero', { pricing: pricing(0) }), offer('mixed')]), group(2, [offer('mixed', { billing_unit: 'image', billing_mode: 'image', pricing: { ...pricing(), per_request_price: 0 } })])])
    expect(visibleModels(models, { ...defaultCatalogFilters(), sort: 'price' }).map(m => m.name)).toEqual(['zero', 'paid', 'unknown', 'mixed'])
  })
})
