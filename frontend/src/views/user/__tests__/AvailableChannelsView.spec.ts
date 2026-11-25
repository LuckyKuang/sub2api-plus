import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import AvailableChannelsView from '../AvailableChannelsView.vue'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { group, offer, pricing, rateQuote } from '@/components/channels/__tests__/catalogFixtures'
import type { ChannelCatalog } from '@/api/channels'
import { update as updateChannel } from '@/api/admin/channels'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import { clearAvailableChannelCatalogCache } from '@/utils/availableChannelCatalogCache'
import { resetAvailableChannelsLayoutSession } from '@/composables/useAvailableChannelsLayout'

const mocks = vi.hoisted(() => ({
  getCatalog: vi.fn(), getCatalogDetail: vi.fn(), copy: vi.fn().mockResolvedValue(true), put: vi.fn(),
  app: { availableChannelsLayout: 'card' },
  auth: { user: { id: 1 }, isAuthenticated: true } as { user: { id: number } | null; isAuthenticated: boolean },
  support: { target: null } as { target: { id: number } | null },
  locale: { value: 'en' }
}))
vi.mock('@/api/channels', () => ({ default: { getCatalog: mocks.getCatalog, getCatalogDetail: mocks.getCatalogDetail } }))
vi.mock('@/api/client', () => ({ apiClient: { put: mocks.put } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
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
  resetAvailableChannelsLayoutSession()
  setAdminSupportContext(null)
  mocks.auth = reactive({ user: { id: 1 }, isAuthenticated: true })
  mocks.support = reactive({ target: null })
  mocks.locale = reactive({ value: 'en' })
  mocks.app = reactive({ availableChannelsLayout: 'card' })
  mocks.getCatalog.mockReset()
  mocks.getCatalog.mockResolvedValue(data())
  mocks.getCatalogDetail.mockReset()
  // The detail dialog asks for one model on its own; answer it with the same
  // fixture the list just returned so quote assertions stay about that model.
  mocks.getCatalogDetail.mockImplementation(async (query: { name: string }) => {
    const result = await (mocks.getCatalog.mock.results.at(-1)?.value ?? Promise.resolve(data()))
    const catalog = (result ?? data()) as ChannelCatalog
    const groups = catalog.groups
      .map(group => ({ ...group, models: group.models.filter(model => model.name.toLowerCase() === query.name.toLowerCase()) }))
      .filter(group => group.models.length)
    return { ...catalog, groups }
  })
})
afterEach(() => {
  wrappers.splice(0).forEach(w => w.unmount())
  setAdminSupportContext(null)
  clearAvailableChannelCatalogCache()
  document.body.innerHTML = ''
})

describe('available channels user page', () => {
  it('uses a list loading placeholder and the existing empty/error states with retry', async () => {
    mocks.app.availableChannelsLayout = 'list'
    let reject!: (error: Error) => void
    mocks.getCatalog.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    const w = page()
    expect(w.get('[aria-busy="true"]').classes()).not.toContain('catalog-grid')
    reject(new Error('Catalog temporarily failed'))
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    mocks.getCatalog.mockResolvedValueOnce({ groups: [], user_rate_status: 'loaded' })
    await w.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('availableChannels.noModels')
    expect(w.find('[data-testid="catalog-model-list"]').exists()).toBe(false)
    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
  })

  it('defaults to cards, changes to the site list without another read and keeps the search term', async () => {
    const w = page(); await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    mocks.app.availableChannelsLayout = 'list'
    await nextTick()
    expect(w.find('.catalog-model-card').exists()).toBe(false)
    expect(w.get('[data-testid="catalog-model-list"]').findAll('h3').map(title => title.text())).toEqual(['claude', 'first', 'second'])
    await w.get('[data-testid="catalog-model-list"] .catalog-list-open').trigger('click')
    await flushPromises()
    expect(w.getComponent(CatalogPriceDetails).props('model').name).toBe('claude')
    expect(mocks.getCatalog).toHaveBeenCalledTimes(1)
    expect(mocks.getCatalogDetail).toHaveBeenCalledTimes(1)
    mocks.app.availableChannelsLayout = 'card'
    await nextTick()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    expect(w.getComponent(CatalogPriceDetails).props('model').name).toBe('claude')
  })

  it('queries one page of nine cards and sends the platform and paging filters to the server', async () => {
    mocks.app.availableChannelsLayout = 'list'
    mocks.getCatalog.mockResolvedValueOnce({
      groups: [group(1, [offer('a-model')])],
      user_rate_status: 'loaded',
      models_total: 12,
      categories: { group: 12 },
      platforms: { openai: 10, anthropic: 2 },
      page: 1,
      page_size: 9,
    })
    const w = page(); await flushPromises()
    expect(mocks.getCatalog.mock.calls[0]![0]).toMatchObject({ page: 1, pageSize: 9, category: 'group' })
    expect(w.get('[data-testid="catalog-model-list"]').findAll('h3').map(title => title.text())).toEqual(['a-model'])

    mocks.getCatalog.mockResolvedValueOnce({
      groups: [group(2, [offer('anthropic-model', { platform: 'anthropic' })])],
      user_rate_status: 'loaded',
      models_total: 2,
      categories: { group: 12 },
      platforms: { openai: 10, anthropic: 2 },
      page: 1,
      page_size: 9,
    })
    const platform = w.findAll('button').find(button => button.text().includes('anthropic'))!
    await platform.trigger('click')
    await flushPromises()
    expect(mocks.getCatalog.mock.calls[1]![0]).toMatchObject({ page: 1, pageSize: 9, platform: 'anthropic' })
    expect(w.get('[data-testid="catalog-model-list"]').findAll('h3').map(title => title.text())).toEqual(['anthropic-model'])
  })

  it('fetches the revised model list after saving channel pricing within the cache lifetime', async () => {
    // Saving channel pricing must prevent a return visit from rendering the
    // removed models out of the still-fresh 180-second cache.
    const first = page(); await flushPromises(); leave(first)
    mocks.put.mockResolvedValueOnce({ data: { id: 7 } })
    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('second')])], user_rate_status: 'loaded' })

    await updateChannel(7, {
      model_pricing: [{ ...pricing(), platform: 'openai', models: ['second'], intervals: [], time_pricing: null }]
    })
    const returning = page(); await flushPromises()

    expect(mocks.getCatalog).toHaveBeenCalledTimes(2)
    expect(returning.findAll('h3').map(title => title.text())).toEqual(['second'])
  })

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

  it('keeps localized catalogs separate while preserving the current model details', async () => {
    const catalogIn = (groupName: string) => ({ groups: [group(1, [offer('first')], { name: groupName })], user_rate_status: 'loaded' })
    mocks.getCatalog.mockResolvedValueOnce(catalogIn('English group'))
    mocks.getCatalogDetail.mockResolvedValueOnce(catalogIn('English group'))
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card')[0]!.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes[0].group.name).toBe('English group')

    // The next list read is served from cache under the same locale+query key;
    // the open dialog still re-reads its own quotes so names follow the locale.
    mocks.getCatalogDetail.mockResolvedValueOnce(catalogIn('中文分组'))
    mocks.locale.value = 'zh'; await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes[0].group.name).toBe('中文分组')

    mocks.getCatalogDetail.mockResolvedValueOnce(catalogIn('English group'))
    mocks.locale.value = 'en'; await flushPromises()
    expect(w.findComponent(CatalogPriceDetails).props('model').quotes[0].group.name).toBe('English group')
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

  it('keeps the consolidated toolbar with live summary outside the independently scrolling channel results', async () => {
    const w = page(); await flushPromises()
    const results = w.get('[data-testid="catalog-results"]')
    const filters = w.get('[data-testid="catalog-filters"]')
    expect(w.getComponent({ name: 'AppLayout' }).props('workspace')).toBe(true)
    expect(w.get('.catalog-page').classes()).toContain('workspace-page')
    expect(results.classes()).toContain('workspace-scroll')
    expect(results.findAll('.catalog-model-card')).toHaveLength(3)
    expect(w.find('[data-testid="catalog-heading"]').exists()).toBe(false)
    expect(w.get('h1.sr-only').text()).toBe('availableChannels.title')
    expect(filters.classes()).toContain('workspace-fixed')
    expect(filters.element.parentElement).toBe(results.element.parentElement)
    expect(results.element.contains(filters.element)).toBe(false)
    expect(filters.find('[aria-live="polite"]').exists()).toBe(true)
    expect(filters.find('button[aria-label="common.refresh"]').exists()).toBe(true)
    expect(filters.find('.segmented-control').exists()).toBe(true)
    expect(filters.find('input[type="search"]').exists()).toBe(true)
  })

  it('toggles between list and card views in the toolbar without extra API reads and preserves filters and open details', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('model-a'), offer('model-z')]),
      group(2, [offer('model-other', { platform: 'anthropic' })])
    ], user_rate_status: 'loaded', models_total: 3, categories: { group: 3 }, platforms: { openai: 2, anthropic: 1 } })
    const w = page(); await flushPromises()
    const filters = w.get('[data-testid="catalog-filters"]')
    const radios = filters.findAll('.segmented-control .segmented-option')
    expect(radios).toHaveLength(2)
    expect(radios[0]!.attributes('aria-pressed')).toBe('false')
    expect(radios[1]!.attributes('aria-pressed')).toBe('true')
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)

    await filters.get('input[type="search"]').setValue('model-')
    await filters.findAll('[role="group"] button[aria-pressed]').find(button => button.text().includes('openai'))!.trigger('click')
    await flushPromises()
    await w.findAll('.catalog-model-card')[0]!.findAll('button')[1]!.trigger('click')
    await flushPromises()
    const callsBeforeToggle = mocks.getCatalog.mock.calls.length
    const openedName = w.getComponent(CatalogPriceDetails).props('model').name

    // Switch to list view in session: zero catalog API calls, filters & dialog intact
    await radios[0]!.trigger('click')
    expect(radios[0]!.attributes('aria-pressed')).toBe('true')
    expect(radios[1]!.attributes('aria-pressed')).toBe('false')
    expect(w.find('[data-testid="catalog-model-list"]').exists()).toBe(true)
    expect(w.find('.catalog-model-card').exists()).toBe(false)
    expect(mocks.getCatalog).toHaveBeenCalledTimes(callsBeforeToggle)
    expect((filters.get('input[type="search"]').element as HTMLInputElement).value).toBe('model-')
    expect(filters.get('.catalog-platform-chip[aria-pressed="true"]').text()).toContain('openai')
    expect(w.getComponent(CatalogPriceDetails).props('model').name).toBe(openedName)

    // Switch back to card view
    await radios[1]!.trigger('click')
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    expect(mocks.getCatalog).toHaveBeenCalledTimes(callsBeforeToggle)
    expect(w.getComponent(CatalogPriceDetails).props('model').name).toBe(openedName)

    // Reactive site default change clears session override
    await radios[0]!.trigger('click')
    expect(w.find('[data-testid="catalog-model-list"]').exists()).toBe(true)
    mocks.app.availableChannelsLayout = 'list'
    await nextTick()
    mocks.app.availableChannelsLayout = 'card'
    await nextTick()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    expect(radios[1]!.attributes('aria-pressed')).toBe('true')
  })

  it('de-emphasizes the reset button when idle and activates it when filters are present', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('model-a')]),
      group(2, [offer('model-b', { platform: 'anthropic' })])
    ], user_rate_status: 'loaded', models_total: 2, categories: { group: 2 }, platforms: { openai: 1, anthropic: 1 } })
    const w = page(); await flushPromises()
    const resetBtn = w.get('.catalog-reset-btn')
    expect(resetBtn.classes()).toContain('is-idle')

    await w.get('input[type="search"]').setValue('model')
    expect(resetBtn.classes()).not.toContain('is-idle')

    await resetBtn.trigger('click'); await flushPromises()
    expect(resetBtn.classes()).toContain('is-idle')

    await w.findAll('[role="group"] button[aria-pressed]').find(button => button.text().includes('anthropic'))!.trigger('click')
    await flushPromises()
    expect(resetBtn.classes()).not.toContain('is-idle')
  })

  it('preserves the platform and search through refresh in the pinned filter card', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('model-a'), offer('model-z')]),
      group(2, [offer('model-other', { platform: 'anthropic' })])
    ], user_rate_status: 'loaded', models_total: 3, categories: { group: 3 }, platforms: { openai: 2, anthropic: 1 } })
    const w = page(); await flushPromises()
    const filters = w.get('[data-testid="catalog-filters"]')
    const results = w.get('[data-testid="catalog-results"]')
    await filters.findAll('[role="group"] button[aria-pressed]').find(button => button.text().startsWith('openai'))!.trigger('click')
    await flushPromises()
    expect(mocks.getCatalog.mock.calls.at(-1)![0]).toMatchObject({ platform: 'openai', category: 'group' })
    await filters.get('input[type="search"]').setValue('model-')
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.get('[data-testid="catalog-results"]').element).toBe(results.element)
    expect((filters.get('input[type="search"]').element as HTMLInputElement).value).toBe('model-')
    expect(filters.get('.catalog-platform-chip[aria-pressed="true"]').text()).toContain('openai')
    expect(mocks.getCatalog.mock.calls.at(-1)![0]).toMatchObject({ search: 'model-', platform: 'openai' })
    expect(results.findAll('h3').map(title => title.text())).toEqual(['model-a', 'model-other', 'model-z'])
  })

  it('loads one page and copies the complete model ID from a card', async () => {
    const w = page(); await flushPromises()
    expect(mocks.getCatalog).toHaveBeenCalledOnce()
    expect(w.findAll('.catalog-model-card')).toHaveLength(3)
    await w.findAll('.catalog-model-card')[0]!.findAll('button')[0]!.trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith('claude')
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
  it('reconciles filters and closes details for an offer a successful refresh removed', async () => {
    const w = page(); await flushPromises()
    await w.findAll('.catalog-model-card')[0]!.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(w.getComponent(CatalogPriceDetails).props('model').name).toBe('claude')

    mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('replacement')])], user_rate_status: 'loaded', models_total: 1, categories: { group: 1 }, platforms: { openai: 1 } })
    await w.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(w.findAll('.catalog-model-card')).toHaveLength(1)
    expect(w.text()).toContain('replacement')
    expect(w.findComponent(CatalogPriceDetails).props('model')).toBeNull()
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

  it('keeps dialog keyboard focus trapped when expanding via comparison row click and restores focus on Escape', async () => {
    mocks.getCatalog.mockResolvedValue({ groups: [
      group(1, [offer('shared', { offer_key: 'q-1', service_tier_pricing: [{ name: 'priority', pricing: pricing(6e-6) }] })]),
      group(2, [offer('shared', { offer_key: 'q-2' })])
    ], user_rate_status: 'loaded' })
    const w = page(); await flushPromises()
    const trigger = w.findAll('.catalog-model-card button')[1]!
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click'); await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')!
    const close = dialog.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!
    const rowToggles = dialog.querySelectorAll<HTMLButtonElement>('.catalog-row-toggle')
    // Closed disclosures are hidden so Shift+Tab from close lands on the last row toggle
    close.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(rowToggles[1])
    // Expanding row 0 reveals its quote & service tier while keeping row 1's disclosure hidden
    rowToggles[0]!.click(); await flushPromises()
    const prioritySummary = Array.from(dialog.querySelectorAll<HTMLElement>('summary')).find(s => s.textContent!.includes('tiers.priority'))!
    close.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(prioritySummary)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await flushPromises()
    expect(document.activeElement).toBe(trigger.element)
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

it('shows only populated categories from the server facets and switches category per query', async () => {
 const facets = { models_total: 1, categories: { group: 1, composite: 1 }, platforms: { openai: 1 } }
 mocks.getCatalog.mockResolvedValueOnce({ groups: [group(1, [offer('ordinary-only')])], user_rate_status: 'loaded', ...facets })
 const wrapper = page(); await flushPromises()
 expect(wrapper.findAll('h3').map(node => node.text())).toEqual(['ordinary-only'])
 const category = wrapper.findAll('button').find(button => button.text() === 'availableChannels.catalog.compositePlatforms')!
 mocks.getCatalog.mockResolvedValueOnce({ groups: [group(2, [offer('aggregate-only')], { platform: 'composite' })], user_rate_status: 'loaded', ...facets })
 await category.trigger('click')
 await flushPromises()
 expect(mocks.getCatalog.mock.calls.at(-1)![0]).toMatchObject({ category: 'composite', page: 1 })
 expect(wrapper.findAll('h3').map(node => node.text())).toEqual(['aggregate-only'])
 const callsBeforeReturn = mocks.getCatalog.mock.calls.length
 await wrapper.findAll('button').find(button => button.text() === 'availableChannels.catalog.groupPlatforms')!.trigger('click')
 await flushPromises()
 // Returning to a page already in the short-lived cache must not refetch it.
 expect(mocks.getCatalog.mock.calls.length).toBe(callsBeforeReturn)
 expect(wrapper.findAll('h3').map(node => node.text())).toEqual(['ordinary-only'])
})
