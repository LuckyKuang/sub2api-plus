import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import AvailableChannelsView from '../AvailableChannelsView.vue'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { group, offer, pricing, rateQuote } from '@/components/channels/__tests__/catalogFixtures'
import type { ChannelCatalog } from '@/api/channels'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import { clearAvailableChannelCatalogCache } from '@/utils/availableChannelCatalogCache'

const mocks = vi.hoisted(() => ({
  getCatalog: vi.fn(), copy: vi.fn(),
  auth: { user: { id: 1 }, isAuthenticated: true } as { user: { id: number } | null; isAuthenticated: boolean },
  support: { target: null } as { target: { id: number } | null },
  locale: { value: 'en' }
}))
vi.mock('@/api/channels', () => ({ default: { getCatalog: mocks.getCatalog } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/adminSupportView', () => ({ useAdminSupportViewStore: () => mocks.support }))
vi.mock('@/i18n', () => ({ getLocale: () => mocks.locale.value }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
const data = (): ChannelCatalog => ({ groups: [group(1, [offer('first'), offer('second')]), group(2, [offer('claude', { platform: 'anthropic' })])], user_rate_status: 'loaded' })
const global = { stubs: { AppLayout: { name: 'AppLayout', props: { workspace: Boolean }, template: '<main><slot /></main>' }, PlatformIcon: { template: '<span />' }, Icon: { template: '<span />' } } }
function page() { const wrapper = mount(AvailableChannelsView, { global, attachTo: document.body }); wrappers.push(wrapper); return wrapper }
function leave(wrapper: VueWrapper) { wrappers.splice(wrappers.indexOf(wrapper), 1); wrapper.unmount() }
beforeEach(() => {
  vi.clearAllMocks()
  clearAvailableChannelCatalogCache()
  setAdminSupportContext(null)
  mocks.auth = reactive({ user: { id: 1 }, isAuthenticated: true })
  mocks.support = reactive({ target: null })
  mocks.locale = reactive({ value: 'en' })
  mocks.getCatalog.mockResolvedValue(data())
})
afterEach(() => {
  wrappers.splice(0).forEach(w => w.unmount())
  setAdminSupportContext(null)
  clearAvailableChannelCatalogCache()
  document.body.innerHTML = ''
})

describe('available channels user page', () => {
  // Approved loading behavior: a fresh authorized success renders immediately
  // on return; only the user's refresh bypasses the 180-second memory cache.
  it('immediately reuses the last successful catalog on return and forces a new manual refresh', async () => {
    const first = page(); await flushPromises()
    leave(first)
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('latest-model')])], user_rate_status: 'loaded' })

    const returning = page()
    expect(returning.findAll('h3').map(title => title.text())).toEqual(['claude', 'first', 'second'])
    expect(returning.find('[aria-busy="true"]').exists()).toBe(false)
    expect(mocks.getCatalog).toHaveBeenCalledTimes(1)

    await returning.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
    expect(returning.findAll('h3').map(title => title.text())).toEqual(['latest-model'])
    leave(returning)
    const refreshed = page()
    expect(refreshed.findAll('h3').map(title => title.text())).toEqual(['latest-model'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it('expires the catalog at 180 seconds without extending freshness on cache reads', async () => {
    vi.useFakeTimers()
    try {
      const started = new Date('2026-10-29T12:00:00Z')
      vi.setSystemTime(started)
      const first = page(); await flushPromises(); leave(first)

      vi.setSystemTime(started.getTime() + 179_999)
      const fresh = page()
      expect(fresh.findAll('.catalog-model-card')).toHaveLength(3)
      expect(mocks.getCatalog).toHaveBeenCalledTimes(1)
      leave(fresh)

      vi.setSystemTime(started.getTime() + 180_000)
      mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('after-expiry')])], user_rate_status: 'loaded' })
      const expired = page()
      expect(expired.find('[aria-busy="true"]').exists()).toBe(true)
      expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
      await flushPromises()
      expect(expired.findAll('h3').map(title => title.text())).toEqual(['after-expiry'])
    } finally { vi.useRealTimers() }
  })

  it('isolates cached user catalogs and ignores same-user profile refreshes', async () => {
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('first-user')])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    mocks.auth.user = { id: 1 }; await flushPromises()
    expect(mocks.getCatalog).toHaveBeenCalledTimes(1)

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(2, [offer('second-user')])], user_rate_status: 'loaded' })
    mocks.auth.user = { id: 2 }; await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['second-user'])
    mocks.auth.user = { id: 1 }; await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['first-user'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it('isolates assistance targets, assistance actors and the actor own-user view', async () => {
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('own-view')])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()

    mocks.support.target = { id: 1 }
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('assisted-view')])], user_rate_status: 'loaded' })
    setAdminSupportContext({ actorId: 1, userId: 1 }); await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['assisted-view'])

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(2, [offer('other-target')])], user_rate_status: 'loaded' })
    setAdminSupportContext({ actorId: 1, userId: 2 })
    mocks.support.target = { id: 2 }; await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['other-target'])

    setAdminSupportContext({ actorId: 1, userId: 1 })
    mocks.support.target = { id: 1 }; await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['assisted-view'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(3)

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('other-actor')])], user_rate_status: 'loaded' })
    mocks.auth.user = { id: 9 }
    setAdminSupportContext({ actorId: 9, userId: 1 }); await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['other-actor'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(4)
  })

  it('keeps localized catalogs separate while preserving filters and current model details', async () => {
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('first')], { name: 'English group' })], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    await w.get('input[type="search"]').setValue('first')
    await w.findAll('.catalog-model-card button')[1]!.trigger('click')

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('first')], { name: '中文分组' })], user_rate_status: 'loaded' })
    mocks.locale.value = 'zh'; await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes[0].group.name).toBe('中文分组')
    expect((w.get('input[type="search"]').element as HTMLInputElement).value).toBe('first')

    mocks.locale.value = 'en'; await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes[0].group.name).toBe('English group')
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it.each(['unavailable', 'not_requested'] as const)('does not reuse a %s response as a fresh successful catalog', async status => {
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('incomplete')])], user_rate_status: status })
    const first = page(); await flushPromises(); leave(first)
    const returning = page(); await flushPromises()
    expect(returning.findAll('h3').map(title => title.text())).toEqual(['claude', 'first', 'second'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it.each(['error', 'unavailable'] as const)('invalidates an older success when a manual refresh returns %s', async outcome => {
    const first = page(); await flushPromises()
    if (outcome === 'error') mocks.getCatalog.mockRejectedValueOnce(new Error('offline'))
    else mocks.getCatalog.mockResolvedValueOnce({ groups: [], user_rate_status: 'unavailable' })
    await first.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    leave(first)
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('new-success')])], user_rate_status: 'loaded' })
    const returning = page(); await flushPromises()
    expect(returning.findAll('h3').map(title => title.text())).toEqual(['new-success'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(3)
  })

  it('aborts an abandoned request and never caches its late successful response', async () => {
    let finish!: (catalog: ChannelCatalog) => void
    mocks.getCatalog.mockImplementationOnce(() => new Promise<ChannelCatalog>(resolve => { finish = resolve }))
    const first = page()
    const signal = mocks.getCatalog.mock.calls[0]![0].signal as AbortSignal
    leave(first)
    expect(signal.aborted).toBe(true)
    finish({ groups: [group(1, [offer('abandoned')])], user_rate_status: 'loaded' }); await flushPromises()
    const returning = page(); await flushPromises()
    expect(returning.text()).not.toContain('abandoned')
    expect(returning.findAll('.catalog-model-card')).toHaveLength(3)
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it('clears current details and rejects old responses when a new session uses the same user ID', async () => {
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card button')[1]!.trigger('click')
    let finish!: (catalog: ChannelCatalog) => void
    mocks.getCatalog.mockImplementationOnce(() => new Promise<ChannelCatalog>(resolve => { finish = resolve }))
    await w.get('button[aria-label="common.refresh"]').trigger('click')
    const oldSignal = mocks.getCatalog.mock.calls[1]![0].signal as AbortSignal

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('new-session')])], user_rate_status: 'loaded' })
    clearAvailableChannelCatalogCache()
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(w.findComponent(CatalogPriceDetails).props('model')).toBeNull()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['new-session'])

    finish({ groups: [group(1, [offer('old-session')])], user_rate_status: 'loaded' }); await flushPromises()
    leave(w)
    expect(page().findAll('h3').map(title => title.text())).toEqual(['new-session'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(3)
  })

  it('aborts the previous locale read and does not cache a late response under another language', async () => {
    let finish!: (catalog: ChannelCatalog) => void
    mocks.getCatalog.mockImplementationOnce(() => new Promise<ChannelCatalog>(resolve => { finish = resolve }))
    const w = page()
    const englishSignal = mocks.getCatalog.mock.calls[0]![0].signal as AbortSignal
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('中文模型')])], user_rate_status: 'loaded' })
    mocks.locale.value = 'zh'; await flushPromises()
    expect(englishSignal.aborted).toBe(true)
    finish({ groups: [group(1, [offer('late-english')])], user_rate_status: 'loaded' }); await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['中文模型'])

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('fresh-english')])], user_rate_status: 'loaded' })
    mocks.locale.value = 'en'; await flushPromises()
    expect(w.findAll('h3').map(title => title.text())).toEqual(['fresh-english'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(3)
  })

  it('does not let one consumer cancellation cancel another consumer request', async () => {
    const finishes: Array<(catalog: ChannelCatalog) => void> = []
    mocks.getCatalog.mockImplementation(() => new Promise<ChannelCatalog>(resolve => finishes.push(resolve)))
    const first = page()
    const second = page()
    const signals = mocks.getCatalog.mock.calls.map(call => call[0].signal as AbortSignal)
    leave(first)
    expect(signals.map(signal => signal.aborted)).toEqual([true, false])
    finishes[1]!({ groups: [group(1, [offer('remaining-consumer')])], user_rate_status: 'loaded' })
    finishes[0]!({ groups: [group(1, [offer('cancelled-consumer')])], user_rate_status: 'loaded' })
    await flushPromises()
    expect(second.findAll('h3').map(title => title.text())).toEqual(['remaining-consumer'])
    leave(second)
    expect(page().findAll('h3').map(title => title.text())).toEqual(['remaining-consumer'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  // Approved user review: channel prices refresh only on the page's manual
  // action; elapsed time, browser focus and visibility changes must not reload
  // the card list or discard the user's current position.
  it('keeps catalog requests manual across timers, focus and visibility changes', async () => {
    vi.useFakeTimers()
    try {
      const w = page(); await flushPromises()
      expect(mocks.getCatalog).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(180_000)
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.getCatalog).toHaveBeenCalledTimes(1)
      await w.get('button[aria-label="common.refresh"]').trigger('click')
      await flushPromises()
      expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
    } finally { vi.useRealTimers() }
  })

  it('keeps heading, filters and count outside the independently scrolling channel cards', async () => {
    const w = page(); await flushPromises()
    const results = w.get('[data-testid="catalog-results"]')
    const heading = w.get('[data-testid="catalog-heading"]')
    const filters = w.get('[data-testid="catalog-filters"]')
    expect(w.getComponent({ name: 'AppLayout' }).props('workspace')).toBe(true)
    expect(w.get('.catalog-page').classes()).toContain('workspace-page')
    expect(results.classes()).toContain('workspace-scroll')
    expect(results.findAll('.catalog-model-card')).toHaveLength(3)
    for (const fixed of [heading, filters, w.get('[aria-live="polite"]')]) {
      expect(fixed.classes()).toContain('workspace-fixed')
      expect(fixed.element.parentElement).toBe(results.element.parentElement)
      expect(results.element.contains(fixed.element)).toBe(false)
    }
    expect(heading.find('h1').exists()).toBe(true)
    expect(filters.find('input[type="search"]').exists()).toBe(true)
  })

  it('preserves platform, search and dropdown sorting through refresh in the pinned filter card', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('model-a', { pricing: pricing(6e-6) }), offer('model-z', { pricing: pricing(1e-6) })]),
      group(2, [offer('model-other', { platform: 'anthropic' })])
    ], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    const filters = w.get('[data-testid="catalog-filters"]')
    const results = w.get('[data-testid="catalog-results"]')
    await filters.findAll('[role="group"] button[aria-pressed]').find(button => button.text().startsWith('openai'))!.trigger('click')
    await filters.get('input[type="search"]').setValue('model-')
    await filters.get('[role="combobox"]').trigger('click'); await flushPromises()
    const dropdown = document.querySelector('[role="listbox"]')!
    expect(results.element.contains(dropdown)).toBe(false)
    expect(dropdown.querySelector('input')).toBeNull()
    const priceOption = Array.from(dropdown.querySelectorAll('[role="option"]')).find(option => option.textContent?.includes('sortPrice'))!
    await new DOMWrapper(priceOption).trigger('click')
    expect(results.findAll('h3').map(title => title.text())).toEqual(['model-z', 'model-a'])
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.get('[data-testid="catalog-results"]').element).toBe(results.element)
    expect((filters.get('input[type="search"]').element as HTMLInputElement).value).toBe('model-')
    expect(filters.get('[aria-pressed="true"]').text()).toContain('openai')
    expect(filters.get('[role="combobox"]').text()).toContain('sortPrice')
    expect(results.findAll('h3').map(title => title.text())).toEqual(['model-z', 'model-a'])
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it('loads catalog once, filters individual cards and copies the complete ID', async () => {
    const w = page(); await flushPromises()
    expect(mocks.getCatalog).toHaveBeenCalledOnce()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    await w.get('input[type="search"]').setValue('first')
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    await w.get('.catalog-model-card button').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith('first')
    expect(w.findAll('[role="group"] button[aria-pressed]')).toHaveLength(3)
  })
  it('shows rate degradation and keeps zero prices visible', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [offer('model', { rate_quote: rateQuote(0) })], { user_rate_multiplier: 0 })], user_rate_status: 'unavailable' })
    const w = page(); await flushPromises()
    expect(w.get('[role="status"]').text()).toContain('ratesUnavailable')
    expect(w.text()).toContain('$0')
  })
  it('merges duplicate cards, removes group navigation and lists every quote in details', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [offer('same', { offer_key: 'one' })]), group(2, [offer('same', { offer_key: 'two', rate_quote: rateQuote(0, { base_source: 'personal' }) })], { is_exclusive: true, user_rate_multiplier: 0 })], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    expect(w.findAll('.catalog-grid')).toHaveLength(1)
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    expect(w.find('[aria-label="modelPlaza.filters.groupLabel"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Group 1')
    expect(w.get('.catalog-model-card').text()).toContain('$0–$2')
    expect(w.get('.catalog-model-card').text()).toContain('quoteCount availableChannels.exclusive')
    await w.findAll('.catalog-model-card button')[1]!.trigger('click'); await flushPromises()
    expect(document.querySelectorAll('.catalog-quote-disclosure')).toHaveLength(2)
    expect(document.querySelectorAll('.catalog-quote')).toHaveLength(0)
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
    await w.findAll('[role="group"] button[aria-pressed]').find(b => b.text().startsWith('anthropic'))!.trigger('click')
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
    expect(document.querySelectorAll('.catalog-quote-disclosure')).toHaveLength(1)
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
  // Accessible disclosure contract: hidden tier content cannot enter the
  // dialog's keyboard loop; each collapsed tier's summary remains reachable.
  it('skips collapsed tier prices when wrapping dialog keyboard focus', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [offer('tiered', {
      service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }, { name: 'flex', pricing: pricing(1e-6) }]
    })])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card button')[1]!.trigger('click'); await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')!
    const close = dialog.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!
    dialog.querySelector<HTMLElement>('.catalog-quote-disclosure > summary')!.click(); await flushPromises()
    const flex = Array.from(dialog.querySelectorAll<HTMLElement>('summary')).find(s => s.textContent!.includes('tiers.flex'))!
    close.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(flex)
    const forward = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    document.dispatchEvent(forward)
    expect(forward.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(close)
    flex.click(); await flushPromises()
    const visiblePrice = flex.parentElement!.querySelector<HTMLElement>('[tabindex="0"]')!
    close.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(visiblePrice)
  })
  // Approved presentation contract: every newly opened model starts with its
  // tiers collapsed. Exercise clicks through the page instead of assigning
  // details.open or supplying a precomputed component state.
  it('resets service tiers after closing details and when opening another model', async () => {
    const tiers = [{ name: 'priority', pricing: pricing(6e-6) }, { name: 'flex', pricing: pricing(1e-6) }]
    mocks.getCatalog.mockResolvedValue({ groups: [group(1, [
      offer('first', { service_tier_pricing: tiers }),
      offer('second', { service_tier_pricing: tiers })
    ])], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    const first = w.findAll('.catalog-model-card')[0]!.findAll('button')[1]!
    const second = w.findAll('.catalog-model-card')[1]!.findAll('button')[1]!
    await first.trigger('click'); await flushPromises()
    document.querySelector<HTMLElement>('.catalog-quote-disclosure > summary')!.click(); await flushPromises()
    document.querySelector<HTMLElement>('.catalog-quote summary')!.click(); await flushPromises()
    expect(document.querySelector<HTMLDetailsElement>('.catalog-quote details')!.open).toBe(true)
    document.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!.click(); await flushPromises()
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await first.trigger('click'); await flushPromises()
    expect(Array.from(document.querySelectorAll<HTMLDetailsElement>('details')).every(d => !d.open)).toBe(true)
    document.querySelector<HTMLElement>('.catalog-quote-disclosure > summary')!.click(); await flushPromises()
    expect(Array.from(document.querySelectorAll<HTMLDetailsElement>('.catalog-quote details')).every(d => !d.open)).toBe(true)
    document.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!.click(); await flushPromises()
    await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await second.trigger('click'); await flushPromises()
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('second')
    expect(document.querySelectorAll('.catalog-quote-disclosure')).toHaveLength(1)
    expect(document.querySelectorAll('.catalog-quote')).toHaveLength(0)
    expect(Array.from(document.querySelectorAll<HTMLDetailsElement>('details')).every(d => !d.open)).toBe(true)
  })
})
