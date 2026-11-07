import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CatalogPriceDetails from '../CatalogPriceDetails.vue'
import { catalogModels } from '../catalog'
import { group, offer, pricing } from './catalogFixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))
const copy = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))

describe('catalog quote disclosure', () => {
  // User/reference requirement: the opened model remains identifiable and its
  // exact request ID can be copied from inside the dialog, without closing it.
  it('copies the currently opened model ID and resets disclosures when switching models', async () => {
    const models = catalogModels([group(1, [offer('Exact-ID'), offer('another-ID')])])
    const w = mount(CatalogPriceDetails, { props: { model: models[0]!, rateUnavailable: false }, global: { stubs: { BaseDialog: { props: ['title'], template: '<div :aria-label="title"><slot /></div>' } } } })
    expect(w.attributes('aria-label')).toBe('Exact-ID')
    await w.get('.catalog-copy-model').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('Exact-ID')
    expect(w.emitted('close')).toBeUndefined()
    await w.get('summary').trigger('click')
    expect(w.find('.catalog-quote').exists()).toBe(true)
    await w.setProps({ model: models[1]! })
    expect(w.attributes('aria-label')).toBe('another-ID')
    expect(w.find('.catalog-quote').exists()).toBe(false)
    await w.get('.catalog-copy-model').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('another-ID')
    w.unmount()
  })

  it.each([26, 50, 200])('compares all %i authorized quotes immediately but mounts detailed tables only on demand', async count => {
    const model = catalogModels(Array.from({ length: count }, (_, index) => group(index + 1, [offer('claude-sonnet-4-6', {
      offer_key: `quote-${index}`, service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }]
    })])))[0]!
    const w = mount(CatalogPriceDetails, { props: { model, rateUnavailable: false }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    expect(w.findAll('.catalog-comparison tbody tr')).toHaveLength(count)
    expect(w.findAll('.catalog-quote-disclosure')).toHaveLength(count)
    expect(w.findAll('.catalog-quote')).toHaveLength(0)
    expect(w.findAll('table')).toHaveLength(1)
    const requested = w.findAll('.catalog-quote-disclosure').find(disclosure => disclosure.get('summary .font-semibold').text() === `Group ${count}`)!
    await requested.get('summary').trigger('click')
    expect(w.findAll('.catalog-quote')).toHaveLength(1)
    expect(requested.get('.catalog-quote').text()).toContain(`Group ${count}`)
    expect(requested.find('.catalog-quote details table').exists()).toBe(false)
    await requested.get('summary').trigger('click')
    expect(w.findAll('.catalog-quote')).toHaveLength(0)
    w.unmount()
  })
})
