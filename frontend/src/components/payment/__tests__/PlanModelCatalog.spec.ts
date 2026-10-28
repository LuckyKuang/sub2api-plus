import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlanModelCatalog from '../PlanModelCatalog.vue'
import { offer, pricing, rateQuote } from '@/components/channels/__tests__/catalogFixtures'
import type { SaleModelCatalog } from '@/types/billingRate'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))

describe('subscription sale model catalog', () => {
  it.each([
    { name: 'standard', asOf: '2026-10-26T08:00:00Z', peak: 1, effective: 6, calculation: 'product_v2' as const, scope: 'all_meters' as const, policy: 6, charge: '$12' },
    { name: 'peak', asOf: '2026-10-26T10:00:00Z', peak: 1.5, effective: 9, calculation: 'product_v2' as const, scope: 'all_meters' as const, policy: 6, charge: '$18' },
    { name: 'historical override', asOf: '2026-10-26T10:00:00Z', peak: 1, effective: 3, calculation: 'override_v1' as const, scope: 'token_only' as const, policy: 3, charge: '$6' }
  ])('shows the accepted $name quote and literal request price at its fixed quote time', async ({ asOf, peak, effective, calculation, scope, policy, charge }) => {
    const catalog: SaleModelCatalog = { policy_version: 7, as_of: asOf, platforms: ['openai'], rate_min: effective, rate_max: effective,
      models: [offer('model001', { offer_key: 'openai-model001', billing_mode: 'per_request', billing_unit: 'request',
        pricing: { ...pricing(), billing_mode: 'per_request', input_price: null, output_price: null, per_request_price: 2 },
        rate_quote: rateQuote(effective, { base_multiplier: 1, platform_multiplier: 2, model_multiplier: 3, policy_multiplier: policy,
          peak_multiplier: peak, rate_calculation: calculation, peak_scope: scope, model: 'model001', billing_unit: 'request', as_of: asOf }) })] }
    const wrapper = mount(PlanModelCatalog, { props: { catalog } })
    expect(wrapper.get('details > details > summary').text()).toContain(`×${effective}`)
    await wrapper.get('details > summary').trigger('click')
    await wrapper.get('details > details > summary').trigger('click')
    expect(wrapper.get('table').findAll('tbody td').map(cell => cell.text())).toEqual([charge])
    expect(wrapper.text()).toContain(asOf)
    if (calculation === 'override_v1') {
      expect(wrapper.text()).toContain('legacyFactorBreakdown{"base":1,"policy":3,"peak":1}')
      expect(wrapper.text()).not.toContain('factorBreakdown{"base":1,"platform":2,"model":3')
      expect(wrapper.get('table').text()).not.toContain('$18')
    } else {
      expect(wrapper.text()).toContain(`factorBreakdown{"base":1,"platform":2,"model":3,"peak":${peak}}`)
    }
    wrapper.unmount()
  })

  it('shows actual server-quoted token prices, source breakdown and time rules before purchase', async () => {
    const catalog: SaleModelCatalog = {
      policy_version: 3, as_of: '2026-10-26T12:00:00Z', platforms: ['openai'], rate_min: 2, rate_max: 2,
      models: [offer('public-model', {
        pricing: pricing(2e-6), price_time_multiplier: 0.5,
        rate_quote: rateQuote(1, { base_multiplier: 1, platform_multiplier: 1, model_multiplier: 2, policy_multiplier: 2, effective_multiplier: 2, source: 'model', as_of: '2026-10-26T12:00:00Z' }),
      })],
    }
    const wrapper = mount(PlanModelCatalog, { props: { catalog } })
    expect(wrapper.text()).toContain('public-model')
    expect(wrapper.text()).toContain('×2')
    expect(wrapper.find('table').exists()).toBe(false)
    await wrapper.get('details > details > summary').trigger('click')
    expect(wrapper.find('table').text()).toContain('$2')
    expect(wrapper.find('table').text()).toContain('$10')
    expect(wrapper.text()).toContain('factorBreakdown{"base":1,"platform":1,"model":2,"peak":1}')
    expect(wrapper.text()).toContain('source.model')
    expect(wrapper.text()).toContain('2026-10-26T12:00:00Z')
    expect(wrapper.text()).toContain('priceTimeFactor{"factor":0.5}')
    expect(wrapper.text()).not.toContain('availableChannels.public')
  })

  it('keeps explicit free and unknown quotes distinct', async () => {
    const catalog: SaleModelCatalog = { policy_version: 1, as_of: '', platforms: ['openai'], rate_min: 0, rate_max: 0, models: [
      offer('free-model', { rate_quote: rateQuote(0) }),
      offer('unknown-model', { rate_quote: null }),
    ] }
    const wrapper = mount(PlanModelCatalog, { props: { catalog } })
    const rows = wrapper.findAll('details > details')
    await rows[0]!.get('summary').trigger('click')
    await rows[1]!.get('summary').trigger('click')
    expect(rows[0]!.text()).toContain('×0')
    expect(rows[0]!.find('table').text()).toContain('$0')
    expect(rows[1]!.text()).toContain('billingRates.unknown')
    expect(rows[1]!.find('table').text()).not.toContain('$0')
  })
})
