import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CatalogPriceDetails from '../CatalogPriceDetails.vue'
import { catalogModels } from '../catalog'
import { group, offer, pricing } from './catalogFixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: object) => key + (params ? JSON.stringify(params) : '') }) }))
const copy = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
const stubs = { BaseDialog: { props: ['title'], template: '<div :aria-label="title"><slot /></div>' }, PlatformIcon: true, Icon: true }

describe('catalog quote disclosure', () => {
  // User/reference requirement: the opened model remains identifiable and its
  // exact request ID can be copied from inside the dialog, without closing it.
  it('omits duplicate h3 title, copies the currently opened model ID and resets disclosures when switching models', async () => {
    const models = catalogModels([group(1, [offer('Exact-ID'), offer('another-ID')])])
    const w = mount(CatalogPriceDetails, { props: { model: models[0]!, rateUnavailable: false }, global: { stubs } })
    expect(w.attributes('aria-label')).toBe('Exact-ID')
    expect(w.find('.catalog-dialog-topbar h3').exists()).toBe(false)
    await w.get('.catalog-copy-model').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('Exact-ID')
    expect(w.emitted('close')).toBeUndefined()
    await w.get('.catalog-toggle-all-btn').trigger('click')
    expect(w.get('.catalog-toggle-all-btn').attributes('aria-pressed')).toBe('true')
    await w.get('summary').trigger('click')
    expect(w.find('.catalog-quote').exists()).toBe(true)
    await w.setProps({ model: models[1]! })
    expect(w.attributes('aria-label')).toBe('another-ID')
    expect(w.find('.catalog-quote').exists()).toBe(false)
    expect(w.get('.catalog-toggle-all-btn').attributes('aria-pressed')).toBe('false')
    expect(w.get('.catalog-quote-disclosure').attributes('hidden')).toBeDefined()
    await w.get('.catalog-copy-model').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('another-ID')
    w.unmount()
  })

  it.each([26, 50, 200])('compares all %i authorized quotes immediately, hides closed disclosures and mounts detailed tables only on demand', async count => {
    const model = catalogModels(Array.from({ length: count }, (_, index) => group(index + 1, [offer('claude-sonnet-4-6', {
      offer_key: `quote-${index}`, service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }]
    })])))[0]!
    const w = mount(CatalogPriceDetails, { props: { model, rateUnavailable: false }, global: { stubs } })
    expect(w.findAll('.catalog-comparison tbody tr')).toHaveLength(count)
    const disclosures = w.findAll('.catalog-quote-disclosure')
    expect(disclosures).toHaveLength(count)
    expect(disclosures.every(d => d.attributes('hidden') !== undefined)).toBe(true)
    expect(w.findAll('.catalog-quote')).toHaveLength(0)
    expect(w.findAll('table')).toHaveLength(1)
    const requested = disclosures.find(disclosure => disclosure.get('summary .font-semibold').text() === `Group ${count}`)!
    await requested.get('summary').trigger('click')
    expect(requested.attributes('hidden')).toBeUndefined()
    expect(w.findAll('.catalog-quote')).toHaveLength(1)
    expect(requested.get('.catalog-quote').text()).toContain(`Group ${count}`)
    expect(requested.find('.catalog-quote details table').exists()).toBe(false)
    await requested.get('summary').trigger('click')
    expect(w.findAll('.catalog-quote')).toHaveLength(0)
    expect(requested.attributes('hidden')).toBeDefined()
    w.unmount()
  })

  it('links comparison table row clicks in single-focus mode and supports multi-open when showing all disclosures', async () => {
    const model = catalogModels([
      group(1, [offer('shared-model', { offer_key: 'q-1' })]),
      group(2, [offer('shared-model', { offer_key: 'q-2' })]),
      group(3, [offer('shared-model', { offer_key: 'q-3' })]),
    ])[0]!
    const w = mount(CatalogPriceDetails, { props: { model, rateUnavailable: false }, global: { stubs } })
    const rows = w.findAll('.catalog-comparison tbody tr')
    const disclosures = w.findAll('.catalog-quote-disclosure')

    expect(disclosures.every(d => d.attributes('hidden') !== undefined)).toBe(true)

    await rows[1]!.trigger('click')
    expect(rows[1]!.classes()).toContain('is-selected')
    expect(disclosures[1]!.attributes('hidden')).toBeUndefined()
    expect(disclosures[0]!.attributes('hidden')).toBeDefined()
    expect(w.findAll('.catalog-quote')).toHaveLength(1)
    expect(w.get('.catalog-quote').text()).toContain('Group 2')

    // Single-focus mode collapses the previous disclosure when clicking another row
    await rows[2]!.trigger('click')
    expect(rows[1]!.classes()).not.toContain('is-selected')
    expect(rows[2]!.classes()).toContain('is-selected')
    expect(disclosures[1]!.attributes('hidden')).toBeDefined()
    expect(disclosures[2]!.attributes('hidden')).toBeUndefined()
    expect(w.findAll('.catalog-quote')).toHaveLength(1)
    expect(w.get('.catalog-quote').text()).toContain('Group 3')

    // Clicking the active row again collapses it
    await rows[2]!.trigger('click')
    expect(rows[2]!.classes()).not.toContain('is-selected')
    expect(w.findAll('.catalog-quote')).toHaveLength(0)

    // Show-all mode reveals all disclosure summaries and allows opening multiple quotes
    await w.get('.catalog-toggle-all-btn').trigger('click')
    expect(disclosures.every(d => d.attributes('hidden') === undefined)).toBe(true)
    await rows[0]!.trigger('click')
    await rows[1]!.trigger('click')
    expect(w.findAll('.catalog-quote')).toHaveLength(2)
    expect(rows[0]!.classes()).toContain('is-selected')
    expect(rows[1]!.classes()).toContain('is-selected')
    w.unmount()
  })
})
