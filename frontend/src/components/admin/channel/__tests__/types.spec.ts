import { describe, expect, it } from 'vitest'
import {
  apiIntervalsToForm,
  apiTimePricingToForm,
  buildSyncedPricingEntries,
  createDefaultTimePricingForm,
  formIntervalsToAPI,
  formReasoningEffortMultipliersToAPI,
  formTimePricingToAPI,
  isValidPositiveMultiplier,
  validateIntervals,
  validateReasoningEffortMultipliers,
  validateTimePricing,
  type IntervalFormEntry,
  type TimePricingFormEntry,
  type TimePricingPeriodFormEntry,
} from '../types'
import type { ModelPricingReference, ReferencePricingCard } from '@/api/admin/channels'

describe('reasoning effort multipliers', () => {
  it('serializes independent overrides without altering their values', () => {
    expect(formReasoningEffortMultipliersToAPI({ none: '0.5', high: 1, max: '3', low: '' }))
      .toEqual({ none: 0.5, high: 1, max: 3 })
  })

  it.each([null, undefined, {}, { max: '' }])('clears empty overrides with null: %j', value => {
    expect(formReasoningEffortMultipliersToAPI(value)).toBeNull()
    expect(validateReasoningEffortMultipliers(value, t)).toBeNull()
  })

  it('accepts supported levels with positive finite multipliers, including discounts', () => {
    expect(validateReasoningEffortMultipliers({
      none: 0.01, minimal: 0.5, low: 1, medium: '1.2', high: 2, xhigh: 2.5, max: 3,
    }, t)).toBeNull()
  })

  it.each([0, -1, Infinity, NaN, 'invalid', 'Infinity'])('rejects invalid multiplier %s', multiplier => {
    expect(validateReasoningEffortMultipliers({ high: multiplier }, t)).toContain('reasoningEffortMultiplierPositive')
  })

  it('rejects unsupported effort keys', () => {
    expect(validateReasoningEffortMultipliers({ unknown: 2 }, t)).toContain('reasoningEffortLevelInvalid')
  })
})

describe('interval multiplier conversion', () => {
  it('preserves component multipliers without MTok conversion', () => {
    const form = apiIntervalsToForm([{
      min_tokens: 272000,
      max_tokens: null,
      tier_label: '',
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_read_price: null,
      input_multiplier: 2,
      output_multiplier: 1.5,
      cache_write_multiplier: 2,
      cache_read_multiplier: 2,
      per_request_price: null,
      sort_order: 0,
    }])

    expect(form[0].input_multiplier).toBe(2)
    expect(form[0].output_multiplier).toBe(1.5)
    expect(formIntervalsToAPI(form)[0]).toMatchObject({
      input_multiplier: 2,
      output_multiplier: 1.5,
      cache_write_multiplier: 2,
      cache_read_multiplier: 2,
    })
  })
})

// 「同步最新模型」/手动添加回归：同步回来的模型必须每个都带着自己的官方价进入
// 独立计价条目。曾经的实现把所有模型塞进同一条空价条目，运营者读到的是
// 「没有相应价格」；后来改成按价签名合并，又让一部分模型拿到另一部分模型的价。
describe('buildSyncedPricingEntries', () => {
  const card = (over: Partial<ReferencePricingCard> = {}): ReferencePricingCard => ({
    platform: 'openai',
    models: [],
    billing_mode: 'token',
    input_price: 2e-6,
    output_price: 10e-6,
    cache_write_price: 2.5e-6,
    cache_write_1h_price: null,
    cache_read_price: 2e-7,
    fast_multiplier: 2,
    flex_multiplier: 0.5,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    ...over,
  })

  const priced = (model: string, over: Partial<ModelPricingReference> = {}): ModelPricingReference => ({
    model,
    matched_model: model,
    platform: 'openai',
    status: 'priced',
    source: 'release_catalog',
    reason_code: '',
    pricing: card({ models: [model], ...over }),
    ...over,
  })

  const manual = (model: string): ModelPricingReference => ({
    model,
    matched_model: model,
    platform: 'opencode_go',
    status: 'manual_required',
    source: 'none',
    reason_code: 'exact_price_unavailable',
    pricing: null,
  })

  const ruleFor = (model: string, refs: ModelPricingReference[]) =>
    buildSyncedPricingEntries(refs).find(rule => rule.entry.models[0] === model)

  it('prefills the default prices as $/MTok for each synced model', () => {
    const [rule] = buildSyncedPricingEntries([priced('gpt-6-sol')])

    expect(rule.entry.models).toEqual(['gpt-6-sol'])
    expect(rule.entry.billing_mode).toBe('token')
    expect(rule.entry.input_price).toBe(2)
    expect(rule.entry.output_price).toBe(10)
    expect(rule.entry.cache_write_price).toBe(2.5)
    expect(rule.entry.cache_read_price).toBe(0.2)
    expect(rule.entry.fast_multiplier).toBe(2)
    expect(rule.entry.flex_multiplier).toBe(0.5)
    expect(rule.entry.intervals).toEqual([])
    expect(rule.reference.matched_model).toBe('gpt-6-sol')
  })

  it('creates one rule per model even when two models share the same price', () => {
    const rules = buildSyncedPricingEntries([priced('gpt-6-sol'), priced('gpt-5.6-sol')])

    expect(rules).toHaveLength(2)
    expect(rules.map(r => r.entry.models)).toEqual([['gpt-6-sol'], ['gpt-5.6-sol']])
    expect(rules.every(r => r.entry.input_price === 2)).toBe(true)
  })

  it('keeps each model own price instead of sharing the first model price', () => {
    const rules = buildSyncedPricingEntries([
      priced('gpt-6-sol'),
      priced('gpt-6-luna', { input_price: 1e-7, output_price: 5e-7 }),
    ])

    expect(rules[0].entry.models).toEqual(['gpt-6-sol'])
    expect(rules[0].entry.input_price).toBe(2)
    expect(rules[0].entry.output_price).toBe(10)
    expect(rules[1].entry.models).toEqual(['gpt-6-luna'])
    expect(rules[1].entry.input_price).toBe(0.1)
    expect(rules[1].entry.output_price).toBe(0.5)
  })

  it('keeps the 1h cache-write price when the model supports the breakdown', () => {
    const [rule] = buildSyncedPricingEntries([
      priced('claude-opus-5-5', { cache_write_1h_price: 8e-6 }),
    ])

    expect(rule.entry.cache_write_1h_price).toBe(8)
  })

  it('converts the long-context tier into a form interval', () => {
    const [rule] = buildSyncedPricingEntries([priced('gpt-6-sol', {
      intervals: [{
        min_tokens: 272001,
        max_tokens: null,
        tier_label: '>272000',
        input_price: 4e-6,
        output_price: 15e-6,
        cache_write_price: 5e-6,
        cache_write_1h_price: null,
        cache_read_price: 4e-7,
        input_multiplier: null,
        output_multiplier: null,
        cache_write_multiplier: null,
        cache_read_multiplier: null,
        per_request_price: null,
        sort_order: 0,
      }],
    })])

    expect(rule.entry.intervals).toHaveLength(1)
    expect(rule.entry.intervals[0].min_tokens).toBe(272001)
    expect(rule.entry.intervals[0].input_price).toBe(4)
    expect(rule.entry.intervals[0].output_price).toBe(15)
    // 长上下文档位同样要带上缓存价，只填 input/output 会让该档少计费。
    expect(rule.entry.intervals[0].cache_read_price).toBe(0.4)
  })

  it('keeps an unpriced model as its own incomplete rule and never claims a price', () => {
    const rules = buildSyncedPricingEntries([priced('gpt-6-sol'), manual('unknown-model')])

    expect(rules).toHaveLength(2)
    const unpriced = ruleFor('unknown-model', [priced('gpt-6-sol'), manual('unknown-model')])
    expect(unpriced?.entry.models).toEqual(['unknown-model'])
    expect(unpriced?.entry.input_price).toBeNull()
    expect(unpriced?.entry.output_price).toBeNull()
    expect(unpriced?.reference.status).toBe('manual_required')
    expect(unpriced?.reference.reason_code).toBe('exact_price_unavailable')
    expect(rules[0].entry.input_price).toBe(2)
  })

  it('falls back to one unpriced rule per model when no pricing is available', () => {
    const rules = buildSyncedPricingEntries([manual('a-model'), manual('another-model')])

    expect(rules).toHaveLength(2)
    expect(rules.map(r => r.entry.models)).toEqual([['a-model'], ['another-model']])
    expect(rules.every(r => r.entry.input_price === null)).toBe(true)
  })

  it('carries reasoning-effort multipliers without sharing the source object', () => {
    const multipliers = { none: 0.5, high: 1.5 }
    const [rule] = buildSyncedPricingEntries([
      priced('gpt-6-sol', { reasoning_effort_multipliers: multipliers }),
    ])

    expect(rule.entry.reasoning_effort_multipliers).toEqual(multipliers)
    expect(rule.entry.reasoning_effort_multipliers).not.toBe(multipliers)
  })

  it('maps a per-image card to image billing without converting it into a token price', () => {
    const [rule] = buildSyncedPricingEntries([priced('gpt-image-2', {
      billing_mode: 'image',
      input_price: null,
      output_price: null,
      per_request_price: 0.04,
    })])

    expect(rule.entry.billing_mode).toBe('image')
    expect(rule.entry.per_request_price).toBe(0.04)
    expect(rule.entry.input_price).toBeNull()
  })

  it('never turns an unsupported billing unit into a zero price card', () => {
    const [rule] = buildSyncedPricingEntries([{
      model: 'some-tts-model',
      matched_model: 'some-tts-model',
      platform: 'openai',
      status: 'unsupported_unit',
      source: 'none',
      reason_code: 'unsupported_billing_dimension',
      pricing: null,
    }])

    expect(rule.entry.input_price).toBeNull()
    expect(rule.entry.output_price).toBeNull()
    expect(rule.reference.status).toBe('unsupported_unit')
    expect(rule.reference.reason_code).toBe('unsupported_billing_dimension')
  })
})

describe('positive multiplier validation', () => {
  it('accepts empty and positive values but rejects zero and negative values', () => {
    expect(isValidPositiveMultiplier(null)).toBe(true)
    expect(isValidPositiveMultiplier('')).toBe(true)
    expect(isValidPositiveMultiplier('0.5')).toBe(true)
    expect(isValidPositiveMultiplier(0)).toBe(false)
    expect(isValidPositiveMultiplier(-1)).toBe(false)
  })

  it('rejects a zero interval multiplier', () => {
    expect(validateIntervals([
      makeInterval({ min_tokens: 100, input_multiplier: 0 }),
    ], 'token', t)).toContain('multiplierPositive')
  })
})

function makeInterval(over: Partial<IntervalFormEntry>): IntervalFormEntry {
  return {
    min_tokens: 0,
    max_tokens: null,
    tier_label: '',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    input_multiplier: null,
    output_multiplier: null,
    cache_write_multiplier: null,
    cache_read_multiplier: null,
    per_request_price: null,
    sort_order: 0,
    ...over,
  }
}

function t(key: string, params?: Record<string, unknown>): string {
  return `${key}${params ? ` ${JSON.stringify(params)}` : ''}`
}

describe('validateIntervals', () => {
  describe('token mode', () => {
    it('rejects unbounded interval that is not last', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: null, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: 500000, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('unboundedLast')
    })

    it('accepts unbounded interval at the end', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: 200000, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: null, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toBeNull()
    })

    it('rejects overlapping intervals', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: 250000, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 200000, max_tokens: 500000, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('overlap')
    })

    it('rejects unbounded interval in token mode', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ min_tokens: 0, max_tokens: null, input_price: 1, output_price: 1 }),
        makeInterval({ min_tokens: 100, max_tokens: 200, input_price: 2, output_price: 2 }),
      ]
      expect(validateIntervals(intervals, 'token', t)).toContain('unboundedLast')
    })
  })

  describe('image / per_request mode', () => {
    it('allows multiple unbounded tiers identified by label', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', per_request_price: 0.04 }),
        makeInterval({ tier_label: '2K', per_request_price: 0.06 }),
        makeInterval({ tier_label: '4K', per_request_price: 0.08 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toBeNull()
      expect(validateIntervals(intervals, 'per_request', t)).toBeNull()
    })

    it('still rejects negative prices', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', per_request_price: -1 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toContain('negativePrice')
    })

    it('still rejects max <= min on a single tier', () => {
      const intervals: IntervalFormEntry[] = [
        makeInterval({ tier_label: '1K', min_tokens: 100, max_tokens: 50, per_request_price: 0.04 }),
      ]
      expect(validateIntervals(intervals, 'image', t)).toContain('maxGreaterThanMin')
    })
  })
})

describe('time pricing', () => {
  it('uses a disabled Shanghai default', () => {
    const form = createDefaultTimePricingForm()
    expect(form).toEqual({ timezone: 'Asia/Shanghai', periods: [], weekdays_only: false })
    expect(formTimePricingToAPI(form)).toBeNull()
  })

  it('defaults missing API day scope to every day', () => {
    expect(apiTimePricingToForm({
      timezone: 'Asia/Shanghai',
      periods: [{ start_time: '09:00', end_time: '12:00', multiplier: 2 }],
    }).weekdays_only).toBe(false)
  })

  it('round-trips day scope and formats multiplier', () => {
    const form = apiTimePricingToForm({
      timezone: 'Asia/Shanghai',
      weekdays_only: true,
      periods: [{ start_time: '09:00', end_time: '12:00', multiplier: 2 }],
    })
    expect(form.weekdays_only).toBe(true)
    expect(form.periods[0]).toEqual({
      start_time: '09:00:00',
      end_time: '12:00:00',
      multiplier: '2.00',
    })
    expect(formTimePricingToAPI(form)).toEqual({
      timezone: 'Asia/Shanghai',
      weekdays_only: true,
      periods: [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: 2 }],
    })
  })

  it.each([
    ['separated', [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '2.00' }, { start_time: '14:00:00', end_time: '18:00:00', multiplier: '2.00' }], null],
    ['adjacent', [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '2.00' }, { start_time: '12:00:00', end_time: '14:00:00', multiplier: '1.50' }], null],
    ['midnight split', [{ start_time: '22:00:00', end_time: '00:00:00', multiplier: '2.00' }, { start_time: '00:00:00', end_time: '02:00:00', multiplier: '2.00' }], null],
    ['overlap by one second', [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '2.00' }, { start_time: '11:59:59', end_time: '14:00:00', multiplier: '2.00' }], 'overlap'],
    ['cross midnight', [{ start_time: '22:00:00', end_time: '02:00:00', multiplier: '2.00' }], 'range'],
    ['equal midnight', [{ start_time: '00:00:00', end_time: '00:00:00', multiplier: '2.00' }], 'range'],
    ['missing seconds', [{ start_time: '09:00', end_time: '12:00', multiplier: '2.00' }], 'format'],
    ['zero', [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '0.00' }], 'multiplier'],
    ['three decimals', [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '1.001' }], 'multiplier'],
  ])('%s', (_name, periods, errorKey) => {
    const result = validateTimePricing({
      timezone: 'Asia/Shanghai',
      periods: periods as TimePricingPeriodFormEntry[],
    }, t)
    if (errorKey === null) expect(result).toBeNull()
    else expect(result).toContain(String(errorKey))
  })

  it('rejects non-IANA timezone', () => {
    expect(validateTimePricing({
      timezone: 'UTC+8',
      periods: [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '2.00' }],
    }, t)).toContain('timezone')
  })

  it.each([
    ['missing', undefined],
    ['blank', '   '],
  ])('rejects a %s timezone without throwing during conversion', (_name, timezone) => {
    const form = {
      timezone,
      periods: [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: '2.00' }],
    } as unknown as TimePricingFormEntry

    expect(validateTimePricing(form, t)).toContain('timezone')
    expect(() => formTimePricingToAPI(form)).not.toThrow()
    expect(formTimePricingToAPI(form)?.timezone).toBe('')
  })
})
