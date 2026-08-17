import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import IPAccessControlView from '../IPAccessControlView.vue'

const {
  getSettings,
  getTrustedProxyStatus,
  updateSettings,
  listFailureStates,
  listRules,
  createRule,
  releaseRuleAndReset,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getTrustedProxyStatus: vi.fn(),
  updateSettings: vi.fn(),
  listFailureStates: vi.fn(),
  listRules: vi.fn(),
  createRule: vi.fn(),
  releaseRuleAndReset: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    ipAccessControl: {
      getSettings,
      getTrustedProxyStatus,
      updateSettings,
      listFailureStates,
      listRules,
      createRule,
      releaseRuleAndReset,
      resetFailureState: vi.fn(),
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: <T>(action: () => Promise<T>) => action() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: '<div><slot name="empty" /></div>',
})
const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const defaultSettings = {
  enforcement_enabled: true,
  login_failure_auto_block_enabled: true,
  login_failure_threshold: 8,
  login_failure_window_minutes: 15,
  login_failure_block_minutes: 60,
}

const defaultProxyStatus = {
  configuration_state: 'configured' as const,
  trusted_proxies: ['10.0.0.0/8'],
  client_ip: '203.0.113.8',
  direct_peer_ip: '10.1.2.3',
  direct_peer_trusted: true,
  trusted_proxy_applied: true,
  forwarded_headers: ['X-Forwarded-For'],
  identity_source: 'trusted_forwarded' as const,
  safe_for_enforcement: true,
  legacy_forwarded_mode: false,
  emergency_allowlist_configured: false,
  emergency_allowlist_count: 0,
  automatic_blocking_ready: true,
  manual_blocking_ready: true,
}

const emptyPage = () => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })

function mountView() {
  return mount(IPAccessControlView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        Toggle: true,
        Select: true,
        Icon: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        TotpStepUpDialog: true,
        RouterLink: true,
      },
    },
  })
}

describe('IPAccessControlView', () => {
  beforeEach(() => {
    for (const fn of [
      getSettings,
      getTrustedProxyStatus,
      updateSettings,
      listFailureStates,
      listRules,
      createRule,
      releaseRuleAndReset,
      showError,
      showSuccess,
    ]) {
      fn.mockReset()
    }

    getSettings.mockResolvedValue({ ...defaultSettings })
    getTrustedProxyStatus.mockResolvedValue({ ...defaultProxyStatus })
    updateSettings.mockImplementation(async (payload) => ({ ...payload }))
    listFailureStates.mockResolvedValue(emptyPage())
    listRules.mockResolvedValue(emptyPage())
    createRule.mockResolvedValue({ id: 1 })
    releaseRuleAndReset.mockResolvedValue({ id: 1 })
  })

  it('refreshes failure status after saving enforcement settings', async () => {
    const wrapper = mountView()
    await flushPromises()
    listFailureStates.mockClear()

    await (wrapper.vm as any).saveSettings()
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledTimes(1)
    expect(listFailureStates).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('refreshes rules and failure states after rule creation and release', async () => {
    const wrapper = mountView()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.createForm.ip_or_cidr = '203.0.113.8'
    listRules.mockClear()
    listFailureStates.mockClear()
    await vm.createRule()

    expect(createRule).toHaveBeenCalledTimes(1)
    expect(listRules).toHaveBeenCalledTimes(1)
    expect(listFailureStates).toHaveBeenCalledTimes(1)

    vm.releaseTarget = {
      id: 1,
      ip_or_cidr: '203.0.113.8',
      rule_kind: 'auto_block',
      status: 'active',
    }
    listRules.mockClear()
    listFailureStates.mockClear()
    await vm.releaseAndReset()

    expect(releaseRuleAndReset).toHaveBeenCalledWith(1)
    expect(listRules).toHaveBeenCalledTimes(1)
    expect(listFailureStates).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('returns to the last valid page when failure-state data shrinks', async () => {
    const wrapper = mountView()
    await flushPromises()

    listFailureStates.mockReset()
    listFailureStates
      .mockResolvedValueOnce({ items: [], total: 1, page: 2, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({
        items: [{
          normalized_ip: '203.0.113.8',
          failure_count: 1,
          window_started_at: '2026-07-27T00:00:00Z',
          last_failed_at: '2026-07-27T00:00:00Z',
          window_expires_at: '2026-07-27T00:15:00Z',
          currently_blocked: false,
        }],
        total: 1,
        page: 1,
        page_size: 20,
        pages: 1,
      })

    const vm = wrapper.vm as any
    vm.changeFailurePage(2)
    await flushPromises()

    expect(listFailureStates).toHaveBeenNthCalledWith(1, expect.objectContaining({ page: 2 }))
    expect(listFailureStates).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 1 }))
    expect(vm.failurePage).toBe(1)
    expect(vm.failureStates).toHaveLength(1)
    wrapper.unmount()
  })

  it('shows Cloudflare Real IP steps in the deploy guide', async () => {
    const host = document.createElement('div')
    document.body.appendChild(host)
    const wrapper = mount(IPAccessControlView, {
      attachTo: host,
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          DataTable: DataTableStub,
          Pagination: true,
          Toggle: true,
          Select: true,
          Icon: true,
          BaseDialog: BaseDialogStub,
          ConfirmDialog: true,
          TotpStepUpDialog: true,
          RouterLink: true,
        },
      },
    })
    await flushPromises()

    const guideLink = wrapper.findAll('button').find((button) => button.text() === 'admin.ipAccessControl.trustedProxy.guideLink')
    expect(guideLink).toBeTruthy()
    await guideLink!.trigger('click')
    await flushPromises()

    const bodyText = document.body.textContent ?? ''
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyCloudflareHint')
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyCloudflareHttp')
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyHeaderHint')
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyNginx')
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyVerify')
    expect(bodyText).toContain('admin.ipAccessControl.deployGuide.proxyTunnelHint')
    wrapper.unmount()
    host.remove()
  })

  it('renders safely when an older server returns null collection fields', async () => {
    getTrustedProxyStatus.mockResolvedValue({
      ...defaultProxyStatus,
      trusted_proxies: null,
      forwarded_headers: null,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ipAccessControl.trustedProxy.noneConfigured')
    wrapper.unmount()
  })

  it('returns to the last valid page when rule data shrinks', async () => {
    const wrapper = mountView()
    await flushPromises()

    listRules.mockReset()
    listRules
      .mockResolvedValueOnce({ items: [], total: 1, page: 2, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({
        items: [{
          id: 1,
          ip_or_cidr: '203.0.113.8',
          rule_kind: 'manual_block',
          status: 'active',
          reason: '',
          failure_count: 0,
          hit_count: 0,
          created_at: '2026-07-27T00:00:00Z',
          updated_at: '2026-07-27T00:00:00Z',
        }],
        total: 1,
        page: 1,
        page_size: 20,
        pages: 1,
      })

    const vm = wrapper.vm as any
    vm.changePage(2)
    await flushPromises()

    expect(listRules).toHaveBeenNthCalledWith(1, expect.objectContaining({ page: 2 }))
    expect(listRules).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 1 }))
    expect(vm.page).toBe(1)
    expect(vm.rules).toHaveLength(1)
    wrapper.unmount()
  })
})
