import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
const { store, route } = vi.hoisted(() => ({ store: { target: null as null | { id: number }, loadTarget: vi.fn() }, route: { params: { user_id: '42' }, meta: { adminSupportResource: 'api-keys' } } }))
vi.mock('@/stores/adminSupportView', () => ({ useAdminSupportViewStore: () => store }))
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/views/user/KeysView.vue', () => ({ __esModule: true, default: { template: '<div data-user-page="keys">original key page</div>' } }))
vi.mock('@/views/user/ProfileView.vue', () => ({ __esModule: true, default: { template: '<div data-user-page="profile">original profile page</div>' } }))
vi.mock('@/views/user/ChannelStatusView.vue', () => ({ __esModule: true, default: { template: '<div data-user-page="monitor">original mode selector</div>' } }))
import AdminSupportView from '../AdminSupportView.vue'

describe('assistance mounts original personal pages', () => {
  beforeEach(() => { store.target = { id: 42 }; store.loadTarget.mockReset() })
  it.each([['api-keys','keys'], ['profile','profile'], ['channel-status','monitor']])('reuses %s', async (resource, page) => {
    route.meta.adminSupportResource = resource
    const wrapper = mount(AdminSupportView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, LoadingSpinner: true } } })
    await flushPromises()
    expect(wrapper.find(`[data-user-page="${page}"]`).exists()).toBe(true)
    wrapper.unmount()
  })
  it('withholds the page if the target profile cannot be loaded', async () => {
    store.target = null
    store.loadTarget.mockRejectedValue(new Error('target not found'))
    const wrapper = mount(AdminSupportView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, LoadingSpinner: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('target not found')
    expect(wrapper.find('[data-user-page]').exists()).toBe(false)
    wrapper.unmount()
  })
})
