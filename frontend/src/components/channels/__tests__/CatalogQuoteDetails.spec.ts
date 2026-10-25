import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CatalogQuoteDetails from '../CatalogQuoteDetails.vue'
import { group, offer, pricing } from './catalogFixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))

describe('catalog price details', () => {
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
      offer: offer('video', { billing_mode: 'video', billing_unit: 'second', pricing: { ...pricing(), input_price: null, output_price: null, per_request_price: 0.4 }, media_tiers: [{ label: '720p', unit: 'second', price: 0.4 }] }),
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
    const w = mount(CatalogQuoteDetails, { props: { group: group(1, [], { is_exclusive: true, rate_multiplier: 2, user_rate_multiplier: 0 }), offer: offer(), rateUnavailable: false } })
    expect(w.get('.catalog-effective-rate').text()).toContain('rateLabels.personal{"rate":0}')
    expect(w.text()).toContain('defaultRate{"rate":2}')
    expect(w.text()).toContain('availableChannels.exclusive')
    expect(w.text()).toContain('$0')
    w.unmount()
  })
  it('shows a media override as the effective rate rather than the personal rate', () => {
    const w = mount(CatalogQuoteDetails, { props: { group: group(1, [], { user_rate_multiplier: 0, image_rate_independent: true, image_rate_multiplier: 0.5 }), offer: offer('image', { billing_mode: 'image', billing_unit: 'image', pricing: { ...pricing(), per_request_price: 0.1 } }), rateUnavailable: false } })
    expect(w.get('.catalog-effective-rate').text()).toContain('rateLabels.media{"rate":0.5}')
    expect(w.text()).toContain('overriddenPersonalRate{"rate":0}')
    expect(w.text()).toContain('$0.05')
    w.unmount()
  })

})
