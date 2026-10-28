import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CatalogQuoteDetails from '../CatalogQuoteDetails.vue'
import { group, offer, pricing, rateQuote } from './catalogFixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))

describe('catalog price details', () => {
  it('shows separate platform and model factors, while explaining historical snapshots with their old semantics', async () => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(), offer: offer('model', { rate_quote: rateQuote(1.8, { base_multiplier: 1, platform_multiplier: 1.2, model_multiplier: 1.5, policy_multiplier: 1.8 }) }), rateUnavailable: false
    } })
    expect(w.text()).toContain('factorBreakdown{"base":1,"platform":1.2,"model":1.5,"peak":1}')
    await w.setProps({ offer: offer('old', { rate_quote: rateQuote(1.5, { rate_calculation: 'override_v1', peak_scope: 'token_only', base_multiplier: 1, policy_multiplier: 1.5 }) }) })
    expect(w.text()).toContain('legacyFactorBreakdown{"base":1,"policy":1.5,"peak":1}')
    w.unmount()
  })
  it.each([
    { peak: 1, effective: 6, charge: '$12' },
    { peak: 1.5, effective: 9, charge: '$18' }
  ])('shows the per-request total with independent platform, model and peak factors ($charge)', ({ peak, effective, charge }) => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(),
      offer: offer('model001', {
        billing_mode: 'per_request', billing_unit: 'request',
        pricing: { ...pricing(), billing_mode: 'per_request', input_price: null, output_price: null, per_request_price: 2 },
        rate_quote: rateQuote(effective, {
          model: 'model001', billing_unit: 'request', base_multiplier: 1,
          platform_multiplier: 2, model_multiplier: 3, policy_multiplier: 6,
          peak_multiplier: peak, peak_scope: 'all_meters'
        })
      }), rateUnavailable: false
    } })
    expect(w.get('table').text()).toContain(charge)
    expect(w.get('.catalog-effective-rate').text()).toContain(`"rate":${effective}`)
    expect(w.text()).toContain(`factorBreakdown{"base":1,"platform":2,"model":3,"peak":${peak}}`)
    w.unmount()
  })

  it('does not mount hidden service-tier price tables before the user expands them', async () => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(), offer: offer('token', { service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }] }), rateUnavailable: false
    } })
    expect(w.find('details table').exists()).toBe(false)
    await w.get('summary').trigger('click')
    expect(w.get('details table').text()).toContain('$6')
    await w.get('summary').trigger('click')
    expect(w.find('details table').exists()).toBe(false)
    w.unmount()
  })

  it('quotes video seconds with an independent rate and a unit-neutral column label', () => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(1, [], { rate_multiplier: 3, video_rate_independent: true, video_rate_multiplier: 0.5 }),
      offer: offer('video', { rate_quote: rateQuote(0.5, { base_source: 'media' }), billing_mode: 'video', billing_unit: 'second', pricing: { ...pricing(), input_price: null, output_price: null, per_request_price: 0.4 }, media_tiers: [{ label: '720p', unit: 'second', price: 0.4 }] }),
      rateUnavailable: false
    } })
    expect(w.text()).toContain('$0.2')
    expect(w.text()).toContain('catalog.units.second')
    expect(w.text()).toContain('catalog.unitPrice')
    expect(w.text()).toContain('catalog.independentRate')
    expect(w.text()).not.toContain('pricing.perRequestPrice')
    w.unmount()
  })
  it('preserves token thresholds, null cache fields and separate service-tier prices', async () => {
    const p = { ...pricing(), cache_write_1h_price: 4e-6, intervals: [{ min_tokens: 100, max_tokens: null, input_price: 4e-6 }] }
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(), offer: offer('token', { pricing: p, service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }] }), rateUnavailable: false
    } })
    expect(w.text()).toContain('(100, ∞]')
    expect(w.text()).toContain('cacheWrite1hPrice')
    expect(w.text()).not.toContain('cacheReadPrice')
    expect(w.text()).toContain('tiers.priority')
    await w.get('summary').trigger('click')
    expect(w.text()).toContain('$6')
    w.unmount()
  })
  it.each([
    { output: null, status: 'unknown' as const, free: false },
    { output: 0, status: 'resolved' as const, free: true }
  ])('preserves known input prices and distinguishes missing output from explicit zero ($status)', ({ output, status, free }) => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(),
      offer: offer('partial-model', {
        pricing: { ...pricing(2e-6), output_price: output },
        price_status: status,
        price_reason: status === 'unknown' ? 'pricing_unavailable' : undefined,
        rate_quote: rateQuote(6, { base_multiplier: 1, platform_multiplier: 2, model_multiplier: 3, policy_multiplier: 6 })
      }), rateUnavailable: false
    } })
    const table = w.get('table')
    expect(table.text()).toContain('$12')
    expect(table.text().includes('pricing.outputPrice')).toBe(free)
    expect(table.text().includes('$0')).toBe(free)
    w.unmount()
  })

  it('explains request-dependent unknown prices without rendering a free quote', () => {
    const w = mount(CatalogQuoteDetails, { props: { group: group(), offer: offer('unknown', { price_status: 'unknown', price_reason: 'request_dependent', pricing: null }), rateUnavailable: true } })
    expect(w.text()).toContain('reasons.request_dependent')
    expect(w.text()).toContain('ratesUnavailable')
    expect(w.text()).not.toContain('$0')
    expect(w.find('table').exists()).toBe(false)
    w.unmount()
  })
  it('collapses each service tier by default while keeping official prices visible', async () => {
    const w = mount(CatalogQuoteDetails, { props: {
      group: group(), offer: offer('token', {
        service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }, { name: 'flex', pricing: pricing(1e-6) }, { name: 'ultrafast', pricing: pricing(9e-6) }],
        official_pricing: { input_price: 3e-6, output_price: 15e-6, cache_write_price: null, cache_read_price: null }
      }), rateUnavailable: false
    } })
    const disclosures = w.findAll('details')
    expect(disclosures).toHaveLength(3)
    expect(disclosures.every(d => !(d.element as HTMLDetailsElement).open)).toBe(true)
    expect(w.findAll('table')).toHaveLength(2)
    await disclosures[0].get('summary').trigger('click')
    expect(disclosures[0].get('table').isVisible()).toBe(true)
    expect(disclosures[0].get('table').text()).toContain('$6')
    expect((disclosures[1].element as HTMLDetailsElement).open).toBe(false)
    await disclosures[1].get('summary').trigger('click')
    expect(disclosures[0].get('table').isVisible()).toBe(true)
    expect(disclosures[1].get('table').isVisible()).toBe(true)
    expect(disclosures[1].get('table').text()).toContain('$1')
    expect((disclosures[2].element as HTMLDetailsElement).open).toBe(false)
    await disclosures[2].get('summary').trigger('click')
    expect(disclosures[2].get('table').text()).toContain('$9')
    await disclosures[0].get('summary').trigger('click')
    expect(disclosures[0].find('table').exists()).toBe(false)
    expect(disclosures[1].get('table').isVisible()).toBe(true)
    expect(disclosures[2].get('table').isVisible()).toBe(true)
    expect(w.text()).toContain('tiers.priority')
    expect(w.text()).toContain('tiers.flex')
    expect(w.text()).toContain('catalog.official')
    w.unmount()
  })
  it('highlights a zero personal rate separately from exclusive membership and default rate', () => {
    const w = mount(CatalogQuoteDetails, { props: { group: group(1, [], { is_exclusive: true, rate_multiplier: 2, user_rate_multiplier: 0 }), offer: offer('model', { rate_quote: rateQuote(0, { base_source: 'personal' }) }), rateUnavailable: false } })
    expect(w.get('.catalog-effective-rate').text()).toContain('rateLabels.personal{"rate":0}')
    expect(w.text()).toContain('defaultRate{"rate":2}')
    expect(w.text()).toContain('availableChannels.exclusive')
    expect(w.text()).toContain('$0')
    w.unmount()
  })
  it('shows a media override as the effective rate rather than the personal rate', () => {
    const w = mount(CatalogQuoteDetails, { props: { group: group(1, [], { user_rate_multiplier: 0, image_rate_independent: true, image_rate_multiplier: 0.5 }), offer: offer('image', { rate_quote: rateQuote(0.5, { base_source: 'media' }), billing_mode: 'image', billing_unit: 'image', pricing: { ...pricing(), per_request_price: 0.1 } }), rateUnavailable: false } })
    expect(w.get('.catalog-effective-rate').text()).toContain('rateLabels.media{"rate":0.5}')
    expect(w.text()).toContain('overriddenPersonalRate{"rate":0}')
    expect(w.text()).toContain('$0.05')
    w.unmount()
  })

})
