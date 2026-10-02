import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import type { PublicSettings } from '@/types'
import AppSidebar from '../AppSidebar.vue'

vi.mock('@/stores', () => ({
  useAppStore: () => useAppStore(),
  useAuthStore: () => ({ isAdmin: false, isSimpleMode: false, user: { id: 42 } }),
  useAdminSettingsStore: () => ({ customMenuItems: [], fetch: vi.fn() }),
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/dashboard', params: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() }),
}))
vi.mock('@/composables/useAsyncImageAccess', () => ({
  useAsyncImageAccess: () => ({ canUseAsyncImage: ref(false), refreshAsyncImageAccess: vi.fn() }),
}))
enableAutoUnmount(afterEach)

function settings(mode: 'v1' | 'v2' | 'v3', enabled = true): PublicSettings {
  return { channel_monitor_enabled: enabled, channel_monitor_mode: mode } as PublicSettings
}
function page() {
  return mount(AppSidebar, { global: { stubs: {
    RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
    VersionBadge: true, AdminSupportUserSelector: true, Icon: true,
  } } })
}

describe('ordinary-user monitor navigation', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    useAppStore().cachedPublicSettings = settings('v1')
  })
  it.each(['v1', 'v2', 'v3'] as const)('shows the enabled %s entry', (mode) => {
    useAppStore().cachedPublicSettings = settings(mode)
    const link = page().get('a[href="/monitor"]')
    expect(link.text()).toBe(mode === 'v3' ? 'channelMonitorV3.title' : 'nav.channelStatus')
  })
  it('retains the user entry across mode changes and hides it when disabled', async () => {
    const wrapper = page()
    for (const mode of ['v2', 'v3', 'v1'] as const) {
      useAppStore().cachedPublicSettings = settings(mode)
      await nextTick()
      expect(wrapper.findAll('a[href="/monitor"]')).toHaveLength(1)
    }
    useAppStore().cachedPublicSettings = settings('v2', false)
    await nextTick()
    expect(wrapper.find('a[href="/monitor"]').exists()).toBe(false)
  })
})
