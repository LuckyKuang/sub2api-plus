import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupRateSummary from '../GroupRateSummary.vue'
import { group, offer, rateQuote } from '@/components/channels/__tests__/catalogFixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))

describe('API key group rates', () => {
  it('displays backend quotes by supplier and unit without multiplying them again', () => {
    const catalog = group(1, [offer()], { platform: 'composite', rate_summary: {
      status: 'available', policy_version: 2, as_of: 'now', entries: [
        { platform: 'openai', billing_unit: 'token', default_quote: rateQuote(7, { base_multiplier: 3, platform_multiplier: 2 }), rate_min: 0, rate_max: 9, model_count: 2, has_model_exceptions: true },
        { platform: 'grok', billing_unit: 'image', default_quote: rateQuote(0), rate_min: 0, rate_max: 0, model_count: 1, has_model_exceptions: false }
      ]
    } })
    const w = mount(GroupRateSummary, { props: { group: catalog } })
    expect(w.get('[data-rate-entry="openai:token"]').text()).toContain('×7')
    expect(w.get('[data-rate-entry="openai:token"]').text()).toContain('×0–×9')
    expect(w.get('[data-rate-entry="grok:image"]').text()).toContain('×0')
    expect(w.text()).toContain('billingRates.units.image')
    expect(w.text()).not.toContain('×6')
  })
  it('never renders a default rate after quote failure, and provides retry', async () => {
    const w = mount(GroupRateSummary, { props: { failed: true } })
    expect(w.text()).toContain('billingRates.unknown')
    expect(w.text()).not.toContain('×1')
    await w.get('button').trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })
  it('treats absent, empty and unavailable server summaries explicitly', async () => {
    const w = mount(GroupRateSummary, { props: { group: group() } })
    expect(w.text()).toContain('billingRates.unknown')
    await w.setProps({ group: group(1, [], { rate_summary: { status: 'empty', policy_version: 1, as_of: 'now', entries: [] } }) })
    expect(w.text()).toContain('billingRates.noModels')
    await w.setProps({ group: group(1, [], { rate_summary: { status: 'unavailable', policy_version: 1, as_of: 'now', entries: [] } }) })
    expect(w.text()).toContain('billingRates.unknown')
  })
})
