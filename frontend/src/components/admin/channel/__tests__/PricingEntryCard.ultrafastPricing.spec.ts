import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

function createEntry(): PricingFormEntry {
  return {
    models: ['gpt-6-astra'],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    ultrafast_multiplier: null,
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

function ultrafastInput(wrapper: ReturnType<typeof shallowMount>) {
  const fields = wrapper.findAll('div').filter(node => node.find('label').exists() &&
    node.find('label').text() === 'admin.channels.form.ultrafastMultiplier')
  return fields.length > 0 ? fields[0].find('input') : null
}

describe('PricingEntryCard ultrafast multiplier', () => {
  it('offers the ultrafast multiplier for OpenAI channels only', () => {
    const openai = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), platform: 'openai', enableTierMultipliers: true },
    })
    expect(ultrafastInput(openai)).not.toBeNull()

    const anthropic = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), platform: 'anthropic', enableTierMultipliers: true },
    })
    expect(ultrafastInput(anthropic)).toBeNull()
  })

  it('hides the multiplier row when tier multipliers are disabled', () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), platform: 'openai', enableTierMultipliers: false },
    })
    expect(ultrafastInput(wrapper)).toBeNull()
  })

  it('emits the parsed ultrafast multiplier and clears it to null', async () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), platform: 'openai', enableTierMultipliers: true },
    })
    const input = ultrafastInput(wrapper)
    expect(input).not.toBeNull()

    await input!.setValue('1.5')
    const updates = wrapper.emitted('update')
    expect(updates).toBeTruthy()
    expect((updates![0][0] as PricingFormEntry).ultrafast_multiplier).toBe('1.5')

    await input!.setValue('')
    const cleared = wrapper.emitted('update')!
    expect((cleared[cleared.length - 1][0] as PricingFormEntry).ultrafast_multiplier).toBeNull()
  })

  it('keeps fast and flex multipliers independent from ultrafast', async () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: {
        entry: { ...createEntry(), fast_multiplier: '2.5', flex_multiplier: '0.4', ultrafast_multiplier: '3' },
        platform: 'openai',
        enableTierMultipliers: true,
      },
    })

    const labels = wrapper.findAll('label').map(label => label.text())
    expect(labels).toContain('admin.channels.form.fastMultiplier')
    expect(labels).toContain('admin.channels.form.flexMultiplier')
    expect(labels).toContain('admin.channels.form.ultrafastMultiplier')

    const input = ultrafastInput(wrapper)
    expect((input!.element as HTMLInputElement).value).toBe('3')
  })
})
