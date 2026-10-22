import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import AppHeader from '../AppHeader.vue'

const width = ref(1200)
vi.mock('@vueuse/core', () => ({ useElementSize: () => ({ width }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }), useRoute: () => ({ path: '/dashboard', meta: {}, params: {} }) }))
vi.mock('@/router/title', () => ({ resolveRouteMetaKeys: () => ({ titleKey: 'dashboard.title' }) }))
vi.mock('@/utils/featureFlags', async importOriginal => ({
  ...await importOriginal<typeof import('@/utils/featureFlags')>(),
  isFeatureFlagEnabled: () => true
}))
vi.mock('@/composables/useUserView', () => ({ useUserView: () => ({ user: { role: 'user', username: 'Reviewer', email: 'review@example.test', balance: 2, frozen_balance: 1 } }) }))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ publicSettingsLoaded: true, siteLogo: '', docUrl: 'https://example.test/docs', cachedPublicSettings: {}, toggleMobileSidebar: vi.fn() }),
  useAuthStore: () => ({ logout: vi.fn() }), useOnboardingStore: () => ({ replay: vi.fn() })
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))

enableAutoUnmount(afterEach)
function render() {
  return mount(AppHeader, { attachTo: document.body, global: { stubs: {
    Icon: true, AnnouncementBell: true, RouterLink: { template: '<a><slot /></a>' },
    LocaleSwitcher: { template: '<select data-feature="locale"><option>EN</option></select>' },
    SubscriptionProgressMini: { props: { inline: Boolean }, template: '<div data-feature="subscription" :data-inline="inline" />' }
  } } })
}
// Approved contract: compact based on available header width, preserving every
// tool through the user menu with one mounted subscription component at a time.
describe('header tools in the fluid workspace', () => {
  beforeEach(() => { width.value = 1200 })
  it('moves tools into the menu at narrow widths and restores them when space returns', async () => {
    const wrapper = render()
    expect(wrapper.findAll('[data-feature="locale"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-feature="subscription"]')).toHaveLength(1)
    width.value = 600
    await nextTick()
    expect(wrapper.find('[data-feature="locale"]').exists()).toBe(false)
    expect(wrapper.get('h1').text()).toBe('dashboard.title')
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    const menu = wrapper.get('.header-user-dropdown')
    expect(menu.findAll('[data-feature="locale"]')).toHaveLength(1)
    expect(menu.get('[data-feature="subscription"]').attributes('data-inline')).toBe('true')
    expect(menu.find('a[href="https://example.test/docs"]').exists()).toBe(true)
    expect(menu.text()).toContain('nav.modelPlaza')
    const locale = menu.get('[data-feature="locale"]').element as HTMLSelectElement
    locale.focus()
    await wrapper.get('.header-user-dropdown').trigger('keydown', { key: 'Escape' })
    expect(wrapper.get('button[aria-label="common.userMenu"]').attributes('aria-expanded')).toBe('false')
    await vi.waitFor(() => expect(wrapper.find('.header-user-dropdown').exists()).toBe(false))
    expect(document.activeElement).toBe(wrapper.get('button[aria-label="common.userMenu"]').element)
    width.value = 1200
    await nextTick()
    expect(wrapper.findAll('[data-feature="locale"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-feature="subscription"]')).toHaveLength(1)
  })
})
