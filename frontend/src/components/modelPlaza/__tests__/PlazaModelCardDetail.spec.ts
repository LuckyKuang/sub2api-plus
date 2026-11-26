import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import PlazaModelCard from '../PlazaModelCard.vue'
import PlazaModelDetail from '../PlazaModelDetail.vue'
import PlazaPriceBreakdown from '../PlazaPriceBreakdown.vue'

const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { server_utc_offset: '+08:00' } }) }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params = {}) => [key, ...Object.values(params)].join(' ') })
}))

const base = {
  billing_mode: 'token' as const, input_price: 2e-6, output_price: 10e-6, per_request_price: null,
  cache_write_price: null, cache_write_1h_price: null, cache_write_12h_price: 4e-6, cache_read_price: 0,
  image_input_price: null, image_output_price: null
}
const model: PlazaModel = {
  platform: 'openai', name: 'Same-ID',
  pricing: { ...base, intervals: [
    { ...base, min_tokens: 272000, max_tokens: null, input_price: null, input_multiplier: 2 },
    { ...base, min_tokens: 0, max_tokens: 272000 }
  ] },
  official_pricing: { ...base, input_price: 5e-6, output_price: 20e-6 },
  long_context_basis: 'whole_request'
}
const group: ModelPlazaGroup = {
  id: 1, name: 'Plus', description: '', platform: 'openai', subscription_type: 'standard',
  is_exclusive: false, rate_multiplier: 0.8, user_rate_multiplier: 0.3, image_rate_independent: false,
  image_rate_multiplier: 1, video_rate_independent: false, video_rate_multiplier: 1, peak_rate_enabled: false,
  peak_start: '', peak_end: '', peak_rate_multiplier: 1, long_context_pricing_enabled: true, models: [model]
}

describe('model card pricing and actions', () => {
  it('names the quoted group and tier and uses personal rates in the source currency', async () => {
    const card = mount(PlazaModelCard, { props: { model, group, groupCount: 2 } })
    expect(card.text()).toContain('Plus')
    expect(card.text()).toContain('≤272K')
    expect(card.text()).toContain('$0.60')
    expect(card.text()).toContain('$3.00')
    expect(card.text()).toContain('modelPlaza.catalog.compareGroups 2')
    expect(card.text()).not.toContain('$1.20')
    await card.get('button[title]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('Same-ID')
    expect(card.emitted('open')).toBeUndefined()
    await card.get('button[aria-haspopup="dialog"]').trigger('click')
    expect(card.emitted('open')).toHaveLength(1)
  })

  it.each([
    ['image', true, -0.2, '$0.00', 'perUnitImage'],
    ['video', true, 0.25, '$0.50', 'perUnitSecond'],
    ['per_request', false, 1, '$0.60', 'perUnitRequest']
  ] as const)('uses the configured unit and meter rate for %s', (mode, independent, multiplier, price, unit) => {
    const media = { ...model, pricing: { ...base, billing_mode: mode, per_request_price: 2, intervals: [] } }
    const rates = { ...group, image_rate_independent: independent, image_rate_multiplier: multiplier,
      video_rate_independent: independent, video_rate_multiplier: multiplier }
    const card = mount(PlazaModelCard, { props: { model: media, group: rates, groupCount: 1 } })
    expect(card.text()).toContain(price)
    expect(card.text()).toContain('modelPlaza.table.' + unit)
    expect(card.text()).not.toContain('perMillion')
  })

  it('distinguishes a missing paid price from an explicit zero', () => {
    const card = mount(PlazaModelCard, { props: { model: { ...model, pricing: null }, group, groupCount: 1 } })
    expect(card.text()).toContain('modelPlaza.detail.noPricing')
    expect(card.text()).not.toContain('$')
  })
})

describe('model price detail', () => {
  it('keeps untiered official cache quotes separate and never fills gaps between explicit cache tiers', async () => {
    const first = { ...base, min_tokens: 0, max_tokens: 272000, cache_read_price: null, cache_write_12h_price: null }
    const last = { ...first, min_tokens: 272000, max_tokens: null, input_price: 4e-6 }
    const official = { ...base, cache_write_price: 7e-6, cache_write_1h_price: 8e-6,
      cache_write_12h_price: 9e-6, cache_read_price: 0, intervals: [first, last] }
    const prices = mount(PlazaPriceBreakdown, {
      props: { model: { ...model, official_pricing: official }, source: 'official', title: 'Official', rate: 1 }
    })
    const rows = () => prices.get('details').findAll('.price-line')
      .map(row => [row.get('dt').text(), row.get('dd').text()])
    const cacheRows = () => rows().filter(([label]) => label.startsWith('modelPlaza.catalog.cache'))
    expect(cacheRows()).toEqual([
      ['modelPlaza.catalog.cacheWriteDuration 5m', '$7.00'],
      ['modelPlaza.catalog.cacheWriteDuration 1h', '$8.00'],
      ['modelPlaza.catalog.cacheWriteDuration 12h', '$9.00'],
      ['modelPlaza.catalog.cacheRead', '$0.00']
    ])
    expect(rows()).toContainEqual(['modelPlaza.table.input', '$4.00'])

    await prices.setProps({ model: { ...model, official_pricing: { ...official,
      intervals: [first, { ...last, cache_write_price: 0 }] } } })
    expect(cacheRows()).toEqual([['modelPlaza.catalog.cacheWriteDuration 5m', '$0.00']])
    expect(prices.text()).not.toContain('$7.00')
  })

  it('shows the selected quote and official reference, discloses each tier and changes all pricing on group selection', async () => {
    const timed = { ...model, time_pricing: { timezone: 'Asia/Shanghai', weekdays_only: true,
      periods: [{ start_time: '10:30:00', end_time: '11:00:20', multiplier: 0.5 }] } }
    const plus = { ...group, peak_rate_enabled: true, peak_start: '10:00', peak_end: '12:00', peak_rate_multiplier: 1.5 }
    const pro = { ...group, id: 2, name: 'Pro', user_rate_multiplier: 0.5 }
    const proModel = { ...model, pricing: { ...base, input_price: 8e-6, output_price: 12e-6, intervals: [] } }
    const detail = mount(PlazaModelDetail, {
      props: { show: true, model: timed, group: plus, offers: [{ model: timed, group: plus }, { model: proModel, group: pro }] },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } }
    })
    const sections = detail.findAll('.price-section')
    expect(sections[0].text()).toContain('$0.60')
    expect(sections[0].get('details').text()).toContain('>272K')
    expect(sections[0].get('details').text()).toContain('modelPlaza.catalog.cacheWriteDuration 12h')
    expect(sections[0].get('details').text()).toContain('$1.20')
    expect(sections[0].get('details').text()).toContain('$0.00')
    expect(sections[1].text()).toContain('$5.00')
    expect(sections[1].text()).toContain('$20.00')
    expect(detail.get('.billing-rules').text()).toContain('modelPlaza.table.timePricingRowHintWeekdays Asia/Shanghai')
    expect(detail.get('.billing-rules').text()).toContain('modelPlaza.table.timePricingRowHintPeak 10:00-12:00 ×1.5 (UTC+08:00)')
    expect(sections[2].text()).toContain('10:30–11:00:20')
    expect(sections[2].text()).toContain('$0.30')
    expect(detail.get('header').text()).toContain('Plus')
    await detail.get('button[aria-pressed="false"]').trigger('click')
    expect(detail.get('header').text()).toContain('Pro')
    const changed = detail.findAll('.price-section')
    expect(changed[0].text()).toContain('$4.00')
    expect(changed[0].text()).toContain('$6.00')
    expect(changed[0].text()).not.toContain('272K')
    expect(changed[1].text()).toContain('$5.00')
    expect(detail.text()).not.toContain('UTC+08:00')
  })
})
