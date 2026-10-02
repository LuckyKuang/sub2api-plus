import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import AvailableChannelsView from '../AvailableChannelsView.vue'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { group, offer, pricing } from '@/components/channels/__tests__/catalogFixtures'
import type { ChannelCatalog } from '@/api/channels'

const mocks = vi.hoisted(() => ({ getCatalog: vi.fn(), copy: vi.fn(), auth: { user: { id: 1 } } as { user: { id: number } | null } }))
vi.mock('@/api/channels', () => ({ default: { getCatalog: mocks.getCatalog } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
const data = (): ChannelCatalog => ({ groups: [group(1, [offer('first'), offer('second')]), group(2, [offer('claude', { platform: 'anthropic' })])], user_rate_status: 'loaded' })
const global = { stubs: { AppLayout: { template: '<main><slot /></main>' }, PlatformIcon: { template: '<span />' }, Icon: { template: '<span />' } } }
function page() { const wrapper = mount(AvailableChannelsView, { global, attachTo: document.body }); wrappers.push(wrapper); return wrapper }
beforeEach(() => { vi.clearAllMocks(); mocks.auth = reactive({ user: { id: 1 } }); mocks.getCatalog.mockResolvedValue(data()) })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = '' })

describe('available channels user page', () => {
  it('loads catalog once, filters individual cards and copies the complete ID', async () => {
    const w = page(); await flushPromises()
    expect(mocks.getCatalog).toHaveBeenCalledOnce()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    await w.get('input[type="search"]').setValue('first')
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    await w.get('.catalog-model-card button').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith('first')
    expect(w.findAll('.catalog-filter')).toHaveLength(3)
  })
  it('shows rate degradation and keeps zero prices visible', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [offer()], { user_rate_multiplier: 0 })], user_rate_status: 'unavailable' })
    const w = page(); await flushPromises()
    expect(w.get('[role="status"]').text()).toContain('ratesUnavailable')
    expect(w.text()).toContain('$0')
  })
  it('merges duplicate cards, removes group navigation and lists every quote in details', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [offer('same', { offer_key: 'one' })]), group(2, [offer('same', { offer_key: 'two' })], { is_exclusive: true, user_rate_multiplier: 0 })], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    expect(w.findAll('.catalog-grid')).toHaveLength(1)
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    expect(w.find('[aria-label="modelPlaza.filters.groupLabel"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Group 1')
    expect(w.get('.catalog-model-card').text()).toContain('$0–$2')
    expect(w.get('.catalog-model-card').text()).toContain('quoteCount availableChannels.exclusive')
    await w.findAll('.catalog-model-card button')[1]!.trigger('click'); await flushPromises()
    expect(document.querySelectorAll('.catalog-quote')).toHaveLength(2)
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Group 1')
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Group 2')
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('billingGroupNote')
  })
  it('marks peak-enabled offers independently from context tier pricing', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('peak')], { subscription_type: 'subscription', peak_rate_enabled: true }),
      group(2, [offer('plain')]),
      group(3, [offer('tiered', { media_tiers: [{ label: '2K', unit: 'image', price: 0.1 }] })])
    ], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    const cards = w.findAll('.catalog-model-card')
    expect(cards[0]!.text()).toContain('catalog.dynamicBilling')
    expect(cards[0]!.text()).not.toContain('pricing.intervals')
    expect(cards[1]!.text()).not.toContain('catalog.dynamicBilling')
    expect(cards[2]!.text()).toContain('pricing.intervals')
    expect(cards[2]!.text()).not.toContain('catalog.dynamicBilling')
  })
  it('hides tier badges when the group disables long-context pricing even with returned tiers', async () => {
    const tiered = offer('tiered', { pricing: { ...pricing(), intervals: [{ min_tokens: 100, max_tokens: null, input_price: 4e-6 }] }, media_tiers: [{ label: '2K', unit: 'image', price: 0.1 }] })
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [tiered], { long_context_pricing_enabled: false, peak_rate_enabled: true })], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    expect(w.get('.catalog-model-card').text()).not.toContain('pricing.intervals')
    expect(w.get('.catalog-model-card').text()).toContain('catalog.dynamicBilling')
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [tiered], { long_context_pricing_enabled: true })], user_rate_status: 'loaded' })
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.get('.catalog-model-card').text()).toContain('pricing.intervals')
  })
  it('clears old quotes and open details on refresh error, then retries', async () => {
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card button')[1]!.trigger('click')
    expect(w.findComponent(CatalogPriceDetails).props('model')).not.toBeNull()
    mocks.getCatalog.mockRejectedValueOnce(new Error('offline'))
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(0)
    expect(w.get('[role="alert"]').exists()).toBe(true)
    expect(w.findComponent(CatalogPriceDetails).props('model')).toBeNull()
    await w.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
  })
  it('discards stale responses across user changes and logout', async () => {
    let finish!: (data: ChannelCatalog) => void
    mocks.getCatalog.mockImplementationOnce(() => new Promise<ChannelCatalog>(resolve => { finish = resolve }))
    const w = page(); await nextTick()
    mocks.auth.user = { id: 2 }; await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    finish({ groups: [group(9, [offer('previous-user-secret')])], user_rate_status: 'loaded' }); await flushPromises()
    expect(w.text()).not.toContain('previous-user-secret')
    mocks.auth.user = null; await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(0)
  })
  it('shows no-models when the authorized directory contains only empty groups', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    expect(w.text()).toContain('availableChannels.noModels')
    await w.get('input').setValue('absent')
    expect(w.text()).toContain('availableChannels.noModels')
  })
  it('reconciles filters and closes a removed offer after a successful refresh', async () => {
    const w = page(); await flushPromises()
    await w.findAll('.catalog-filter').find(b => b.text().startsWith('anthropic'))!.trigger('click')
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    await w.findAll('.catalog-model-card button')[1]!.trigger('click')
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('replacement')])], user_rate_status: 'loaded' })
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    expect(w.text()).toContain('replacement')
    expect(w.findComponent(CatalogPriceDetails).props('model')).toBeNull()
    expect(document.activeElement).toBe(w.get('button[aria-label="common.refresh"]').element)
  })
  it('updates open model details when one referenced group is removed', async () => {
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('same', { offer_key: 'one' })]), group(2, [offer('same', { offer_key: 'two' })])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card button')[1]!.trigger('click')
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(2, [offer('same', { offer_key: 'two' })])], user_rate_status: 'loaded' })
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes).toHaveLength(1)
    expect(document.querySelectorAll('.catalog-quote')).toHaveLength(1)
    expect(document.querySelector('[role="dialog"]')!.textContent).not.toContain('Group 1')
  })
  it('traps keyboard focus and restores the details trigger after Escape', async () => {
    const w = page(); await flushPromises()
    const trigger = w.findAll('.catalog-model-card button')[1]!
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click'); await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')!
    const close = dialog.querySelector('button')!
    expect(document.activeElement).toBe(close)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(dialog.contains(document.activeElement)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await flushPromises()
    expect(document.activeElement).toBe(trigger.element)
  })
})
