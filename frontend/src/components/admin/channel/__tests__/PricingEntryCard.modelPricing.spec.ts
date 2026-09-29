import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { IntervalFormEntry, PricingFormEntry } from '../types'
import channelsAPI from '@/api/admin/channels'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const getModelDefaultPricing = vi.mocked(channelsAPI.getModelDefaultPricing)

beforeEach(() => {
  getModelDefaultPricing.mockReset()
})

function createEntry(): PricingFormEntry {
  return {
    models: [],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: false,
      periods: [],
    },
  }
}

function interval(overrides: Partial<IntervalFormEntry>): IntervalFormEntry {
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
    ...overrides,
    sort_order: overrides.sort_order ?? 0,
  }
}

const modelsTagInput = (wrapper: ReturnType<typeof shallowMount>) =>
  wrapper.findComponent({ name: 'ModelTagInput' })

const lastUpdate = (wrapper: ReturnType<typeof shallowMount>) =>
  wrapper.emitted('update')!.at(-1)![0] as PricingFormEntry

// 覆盖「删除模型」：误加的模型要能通过 × 移除并把新列表写回父组件；
// 且当删除的是非主模型时，不得顺手清掉本规则已有的价格（不清空过头）。
describe('PricingEntryCard deleting a model', () => {
  it('writes back a removal instead of silently dropping it', async () => {
    const entry: PricingFormEntry = {
      ...createEntry(),
      models: ['alpha-model', 'beta-model'],
      input_price: 9,
      output_price: 9,
    }
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, platform: 'openai' },
    })

    // 用户点击 beta-model 的 ×：ModelTagInput 发出移除后的新列表。
    modelsTagInput(wrapper).vm.$emit('update:models', ['alpha-model'])
    await flushPromises()

    const update = lastUpdate(wrapper)
    expect(update.models).toEqual(['alpha-model'])
    // 主模型 alpha 未变，本规则已填的价格必须原样保留。
    expect(update.input_price).toBe(9)
    expect(getModelDefaultPricing).not.toHaveBeenCalled()
  })

  it('clears the whole rule when the sole model is removed', async () => {
    const entry: PricingFormEntry = {
      ...createEntry(),
      models: ['alpha-model'],
      input_price: 2,
      output_price: 10,
      cache_write_price: 2.5,
      fast_multiplier: 2,
      flex_multiplier: 0.5,
      reasoning_effort_multipliers: { high: 0.5 },
      intervals: [interval({ min_tokens: 0, max_tokens: 1000, input_price: 2 })],
    }
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, platform: 'openai' },
    })

    modelsTagInput(wrapper).vm.$emit('update:models', [])
    await flushPromises()

    const update = lastUpdate(wrapper)
    expect(update.models).toEqual([])
    // 之前按 alpha 自动填充的 token 价与上下文区间必须清空，不能把一组悬空价格
    // 留在空规则上造成误解。
    expect(update.input_price).toBeNull()
    expect(update.output_price).toBeNull()
    expect(update.cache_write_price).toBeNull()
    expect(update.intervals).toEqual([])
    // 但推理/档位倍率属于用户配置，auto-fill 契约（见既有测试）要求原样保留。
    expect(update.fast_multiplier).toBe(2)
    expect(update.flex_multiplier).toBe(0.5)
    expect(update.reasoning_effort_multipliers).toEqual({ high: 0.5 })
    expect(getModelDefaultPricing).not.toHaveBeenCalled()
  })
})

// 覆盖「主模型切换清空价格/区间/倍率并重查」：把 alpha 删掉换成 beta 时，
// 必须丢掉 alpha 的参考价，并按 beta 重新查一次，否则 beta 会顶着 alpha 的价格。
describe('PricingEntryCard switching the primary model', () => {
  it('re-looks up the reference price for the new primary model', async () => {
    getModelDefaultPricing.mockImplementation(async (_platform, model) => ({
      model,
      matched_model: model,
      platform: 'openai',
      status: 'priced',
      source: 'release_catalog',
      reason_code: '',
      pricing: {
        platform: 'openai',
        models: [model],
        billing_mode: 'token',
        input_price: model === 'alpha-model' ? 2e-6 : 0.1e-6,
        output_price: model === 'alpha-model' ? 10e-6 : 0.5e-6,
        cache_write_price: null,
        cache_write_1h_price: null,
        cache_read_price: null,
        fast_multiplier: null,
        flex_multiplier: null,
        reasoning_effort_multipliers: null,
        image_input_price: null,
        image_output_price: null,
        per_request_price: null,
        intervals:
          model === 'alpha-model'
            ? []
            : [{
                min_tokens: 0,
                max_tokens: 5000,
                tier_label: 'tier',
                input_price: 3e-6,
                output_price: 5e-6,
                cache_write_price: null,
                cache_write_1h_price: null,
                cache_read_price: null,
                input_multiplier: 1,
                output_multiplier: 1,
                cache_write_multiplier: 1,
                cache_read_multiplier: 1,
                per_request_price: null,
                sort_order: 0,
              }],
      },
    }))

    // 已存在一条按 alpha 自动填充好的规则（token 价 + 区间）。此处不放 reasoning/
    // 档位倍率——它们是用户配置，按 auto-fill 契约会被保留并阻止重新查价；本测试
    // 聚焦"纯净的自动填充规则"在主模型切换时清空价格/区间并对新模型重新查价。
    const starting: PricingFormEntry = {
      ...createEntry(),
      models: ['alpha-model'],
      input_price: 2,
      output_price: 10,
      intervals: [interval({ min_tokens: 0, max_tokens: 1000, input_price: 2 })],
    }
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: starting, platform: 'openai' },
    })

    // 第一步：删掉 alpha。
    modelsTagInput(wrapper).vm.$emit('update:models', [])
    await flushPromises()
    expect(lastUpdate(wrapper).input_price).toBeNull()
    await wrapper.setProps({ entry: lastUpdate(wrapper) })

    // 第二步：换成 beta（新主模型）。
    modelsTagInput(wrapper).vm.$emit('update:models', ['beta-model'])
    await flushPromises()

    // 必须按 beta 重新查价，而不是沿用已清空的 alpha 价格。
    expect(getModelDefaultPricing).toHaveBeenCalledWith('openai', 'beta-model')

    const update = lastUpdate(wrapper)
    expect(update.models).toEqual(['beta-model'])
    // 显示的是 beta 的参考价（0.1/0.5 per MTok），不是 alpha 的 2/10。
    expect(update.input_price).toBe(0.1)
    expect(update.output_price).toBe(0.5)
    expect(update.input_price).not.toBe(2)
    // alpha 的旧区间被清掉，换成 beta 参考价里的新区间。
    expect(update.intervals).toHaveLength(1)
    expect(update.intervals![0].input_price).toBe(3)
  })
})
