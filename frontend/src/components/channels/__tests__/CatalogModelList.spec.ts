import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CatalogModelList from '../CatalogModelList.vue'
import { catalogModels } from '../catalog'
import { group, offer, pricing, rateQuote } from './catalogFixtures'

const copy = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const global = { stubs: { Icon: true, PlatformIcon: true } }

// docs/AVAILABLE_CHANNELS.md: both layouts retain all authorized quotes and use
// the same effective USD prices, with no second application of rate factors.
describe('available-channel list', () => {
  it('groups by concrete platform, keeps duplicate names across platforms and copies the actual ID', async () => {
    const models = catalogModels([
      group(1, [offer('Exact-ID'), offer('second')]),
      group(2, [offer('Exact-ID', { platform: 'anthropic' })]),
    ])
    const w = mount(CatalogModelList, { props: { models, rateUnavailable: false }, global })
    expect(w.findAll('section').map(section => section.attributes('aria-label'))).toEqual(['openai', 'anthropic'])
    expect(w.findAll('h3').map(title => title.text())).toEqual(['Exact-ID', 'second', 'Exact-ID'])
    await w.get('h3 button').trigger('click')
    expect(w.emitted('details')?.[0]?.[0]).toMatchObject({ name: 'Exact-ID', platform: 'openai', quotes: [{ group: { id: 1 }, offer: { name: 'Exact-ID' } }] })
    await w.findAll('button')[1]!.trigger('click')
    expect(copy).toHaveBeenLastCalledWith('Exact-ID')
    expect(w.emitted('details')).toHaveLength(1)
  })

  it('shows independently calculated effective input/output ranges and all condition labels', () => {
    // $2/$10 per million, at effective rates 0.5 and 2, yields $1–$4/$5–$20.
    const models = catalogModels([
      group(1, [offer('shared', { rate_quote: rateQuote(0.5, { base_source: 'personal' }) })], { is_exclusive: true }),
      group(2, [offer('shared', { rate_quote: rateQuote(2), pricing: { ...pricing(), intervals: [{ ...pricing(), min_tokens: 0, max_tokens: 1000, per_request_price: null }] } })], { peak_rate_enabled: true }),
    ])
    const w = mount(CatalogModelList, { props: { models, rateUnavailable: false }, global })
    expect(w.findAll('li')).toHaveLength(1)
    expect(w.text()).toContain('$1–$4')
    expect(w.text()).toContain('$5–$20')
    expect(w.text()).toContain('availableChannels.catalog.personalRate')
    expect(w.text()).toContain('availableChannels.exclusive')
    expect(w.text()).toContain('availableChannels.catalog.someTiered')
    expect(w.text()).toContain('availableChannels.catalog.someDynamic')
    expect(models[0]!.quotes).toHaveLength(2)
  })

  it('keeps zero, unknown and mixed billing units distinct, and labels unavailable rates', () => {
    const models = catalogModels([
      group(1, [
        offer('free', { pricing: { ...pricing(0), output_price: 0 } }),
        offer('unknown', { pricing: null, price_status: 'unknown', price_reason: 'pricing_unavailable' }),
        offer('mixed'),
      ]),
      group(2, [offer('mixed', { billing_unit: 'image', billing_mode: 'image', pricing: { ...pricing(), billing_mode: 'image', per_request_price: 0.25 } })]),
    ])
    const w = mount(CatalogModelList, { props: { models, rateUnavailable: true }, global })
    const rows = w.findAll('li')
    expect(rows[0]!.text()).toContain('$0')
    expect(rows[1]!.text()).toContain('—')
    expect(rows[1]!.text()).not.toContain('$0')
    expect(rows[1]!.text()).toContain('availableChannels.catalog.reasons.pricing_unavailable')
    expect(rows[2]!.text()).toContain('availableChannels.catalog.units.token')
    expect(rows[2]!.text()).toContain('availableChannels.catalog.units.image')
    expect(rows[2]!.text()).toContain('$0.25')
    expect(w.text()).toContain('availableChannels.catalog.referenceRange')
  })
})
