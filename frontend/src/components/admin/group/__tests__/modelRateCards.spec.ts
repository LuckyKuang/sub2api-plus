import { describe, expect, it } from 'vitest'
import type { PricingFormEntry } from '@/components/admin/channel/types'
import { createDefaultTimePricingForm } from '@/components/admin/channel/types'
import { hasGroupPriceOverride, modelRateCardKeys, removeModelCardRates } from '../modelRateCards'

const empty = (): PricingFormEntry => ({ models: ['model'], billing_mode: 'token', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: createDefaultTimePricingForm() })
describe('group model rate-only cards', () => {
  it('does not save empty price overrides, but preserves explicitly free unit prices', () => {
    expect(hasGroupPriceOverride(empty())).toBe(false)
    expect(hasGroupPriceOverride({ ...empty(), input_price: '' })).toBe(false)
    expect(hasGroupPriceOverride({ ...empty(), reasoning_effort_multipliers: { high: '' } })).toBe(false)
    expect(hasGroupPriceOverride({ ...empty(), output_price: 0 })).toBe(true)
    expect(hasGroupPriceOverride({ ...empty(), per_request_price: 0 })).toBe(true)
  })
  it('retains configured service-tier price overrides without inventing an override from blank values', () => {
    expect(hasGroupPriceOverride({ ...empty(), fast_multiplier: 3 })).toBe(true)
    expect(hasGroupPriceOverride({ ...empty(), flex_multiplier: 0.25 })).toBe(true)
    expect(hasGroupPriceOverride({ ...empty(), fast_multiplier: '', flex_multiplier: null })).toBe(false)
  })
  it('keeps supplier identity in card keys and exposes rate-only rules when their card is removed', () => {
    expect(modelRateCardKeys([{ ...empty(), platform: 'openai', models: ['A', 'B'] }, { ...empty(), platform: 'grok', models: ['A'] }], 'composite')).toEqual(['openai:a', 'openai:b', 'grok:a'])
    expect(modelRateCardKeys([], 'composite')).toEqual([])
    expect(modelRateCardKeys([empty()], 'composite')).toEqual([])
  })
  it('removes all rules owned by a deleted model card, preserving other suppliers and shared prefix rules', () => {
    const policy = { platform_multipliers: { openai: 2 }, model_multipliers: [
      { platform: 'openai', model: 'model', multiplier: 2 },
      { platform: 'openai', model: 'model', billing_unit: 'image', multiplier: 3 },
      { platform: 'openai', model: 'model*', multiplier: 4 },
      { platform: 'grok', model: 'model', multiplier: 5 }
    ] }
    expect(removeModelCardRates(policy, { ...empty(), platform: 'openai' }, 'composite')).toEqual({ ...policy, model_multipliers: policy.model_multipliers.slice(2) })
    expect(policy.model_multipliers).toHaveLength(4)
  })
})
