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
  blockFailureState,
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
  blockFailureState: vi.fn(),
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
      blockFailureState,
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
  template: '<div><div v-for="(row, index) in data" :key="row.normalized_ip || row.id || index"><slot name="cell-actions" :row="row" /></div><slot v-if="!data.length" name="empty" /></div>',
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
      blockFailureState,
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
    blockFailureState.mockResolvedValue({
      rule: { id: 42, ip_or_cidr: '203.0.113.8', rule_kind: 'manual_block', status: 'active' },
      already_blocked: false,
      effectively_blocked: true,
      suppressed_by_allow_rule: false,
      as_of: '2026-08-17T00:00:00Z',
    })
  })

  it('uses the one-year maximum for both failure window and block duration', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('input[type="number"][max="525600"]')).toHaveLength(2)
    wrapper.unmount()
  })

  it('refreshes failure status after saving enforcement settings', async () => {
    const wrapper = mountView()
    await flushPromises()
    listFailureStates.mockClear()

    const vm = wrapper.vm as any
    vm.settingsUnavailable = true
    await vm.saveSettings()
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledTimes(1)
    expect(listFailureStates).toHaveBeenCalledTimes(1)
    expect(vm.settingsUnavailable).toBe(false)
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
          failure_threshold: 2,
          window_started_at: '2026-07-27T00:00:00Z',
          last_failed_at: '2026-07-27T00:00:00Z',
          window_expires_at: '2026-07-27T00:15:00Z',
          active_block_rule: false,
          runtime_enforcement_enabled: true,
          suppressed_by_allow_rule: false,
          emergency_allowlisted: false,
          effectively_blocked: false,
          as_of: '2026-07-27T00:00:00Z',
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

  it('does not treat a missing last-seen timestamp as a permanent block', async () => {
    const wrapper = mountView()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.formatExpiryTime()).toBe('admin.ipAccessControl.rules.never')
    expect(vm.formatLastSeenTime()).toBe('admin.ipAccessControl.rules.lastSeenNever')
    expect(vm.formatLastSeenTime()).not.toBe('admin.ipAccessControl.rules.never')
    expect(vm.formatTimestamp()).toBe('—')
    expect(vm.formatTimestamp()).not.toBe('admin.ipAccessControl.rules.never')
    expect(vm.formatExpiryTime('2026-08-17T12:00:00Z')).not.toBe('admin.ipAccessControl.rules.never')
    expect(vm.formatLastSeenTime('2026-08-17T12:00:00Z')).not.toBe('admin.ipAccessControl.rules.lastSeenNever')
    const autoBlock = { expires_at: '2026-08-18T12:00:00Z', last_seen_at: null as string | null }
    expect(vm.formatExpiryTime(autoBlock.expires_at)).not.toBe('admin.ipAccessControl.rules.never')
    expect(vm.formatLastSeenTime(autoBlock.last_seen_at ?? undefined)).toBe('admin.ipAccessControl.rules.lastSeenNever')
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

  it('maps layered failure states to distinct operator labels', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any
    const base = {
      failure_count: 2,
      failure_threshold: 2,
      active_block_rule: true,
      runtime_enforcement_enabled: true,
      suppressed_by_allow_rule: false,
      emergency_allowlisted: false,
      effectively_blocked: true,
    }

    expect(vm.failureBlockStatusLabel(base)).toBe('admin.ipAccessControl.failureStates.blocked')
    expect(vm.failureBlockStatusLabel({ ...base, effectively_blocked: false, suppressed_by_allow_rule: true }))
      .toBe('admin.ipAccessControl.failureStates.suppressedByAllow')
    expect(vm.failureBlockStatusLabel({ ...base, effectively_blocked: false, emergency_allowlisted: true }))
      .toBe('admin.ipAccessControl.failureStates.emergencyAllowlisted')
    expect(vm.failureBlockStatusLabel({ ...base, effectively_blocked: false, runtime_enforcement_enabled: false }))
      .toBe('admin.ipAccessControl.failureStates.ruleNotEnforced')
    expect(vm.failureBlockStatusLabel({ ...base, active_block_rule: false, effectively_blocked: false }))
      .toBe('admin.ipAccessControl.failureStates.observing')
    wrapper.unmount()
  })

  it('submits the dedicated manual-block action once and refreshes both lists', async () => {
    const state = {
      normalized_ip: '203.0.113.8',
      failure_count: 1,
      failure_threshold: 2,
      window_started_at: '2026-08-17T00:00:00Z',
      last_failed_at: '2026-08-17T00:00:00Z',
      window_expires_at: '2026-08-17T00:15:00Z',
      active_block_rule: false,
      runtime_enforcement_enabled: true,
      suppressed_by_allow_rule: false,
      emergency_allowlisted: false,
      effectively_blocked: false,
      as_of: '2026-08-17T00:00:00Z',
    }
    listFailureStates.mockResolvedValue({ ...emptyPage(), items: [state], total: 1 })

    let resolveBlock!: (value: unknown) => void
    blockFailureState.mockReturnValueOnce(new Promise((resolve) => { resolveBlock = resolve }))
    const wrapper = mountView()
    await flushPromises()

    const manualButton = wrapper.findAll('button').find((button) => button.text() === 'admin.ipAccessControl.failureStates.manualBlock')
    expect(manualButton).toBeTruthy()
    expect(manualButton!.attributes('disabled')).toBeUndefined()

    const vm = wrapper.vm as any
    getSettings.mockClear()
    await vm.confirmManualBlock(state)
    expect(getSettings).not.toHaveBeenCalled()
    listFailureStates.mockClear()
    listRules.mockClear()
    const firstSubmit = vm.manualBlockFailureState()
    await vm.manualBlockFailureState()

    expect(blockFailureState).toHaveBeenCalledTimes(1)
    expect(blockFailureState).toHaveBeenCalledWith({ ip: '203.0.113.8' })
    expect(vm.manualBlockSubmitting).toBe(true)

    resolveBlock({
      rule: { id: 42, ip_or_cidr: '203.0.113.8', rule_kind: 'manual_block', status: 'active' },
      already_blocked: false,
      effectively_blocked: true,
      suppressed_by_allow_rule: false,
      as_of: '2026-08-17T00:00:00Z',
    })
    await firstSubmit
    await flushPromises()

    expect(vm.manualBlockSubmitting).toBe(false)
    expect(listFailureStates).toHaveBeenCalledTimes(1)
    expect(listRules).toHaveBeenCalledTimes(1)
    expect(showSuccess).toHaveBeenCalledWith('admin.ipAccessControl.failureStates.manualBlockSuccess')
    wrapper.unmount()
  })

  it('uses safe alternate actions and disables manual blocking for stale or unenforced state', async () => {
    const base = {
      normalized_ip: '203.0.113.8',
      failure_count: 2,
      failure_threshold: 2,
      window_started_at: '2026-08-17T00:00:00Z',
      last_failed_at: '2026-08-17T00:00:00Z',
      window_expires_at: '2026-08-17T00:15:00Z',
      active_block_rule: false,
      runtime_enforcement_enabled: true,
      suppressed_by_allow_rule: false,
      emergency_allowlisted: false,
      effectively_blocked: false,
      as_of: '2026-08-17T00:00:00Z',
    }
    listFailureStates.mockResolvedValue({
      ...emptyPage(),
      total: 4,
      items: [
        { ...base, active_block_rule: true, block_rule_ip_or_cidr: '203.0.113.0/24', effectively_blocked: true },
        { ...base, normalized_ip: '203.0.113.9', suppressed_by_allow_rule: true },
        { ...base, normalized_ip: '203.0.113.10', runtime_enforcement_enabled: false },
        { ...base, normalized_ip: '203.0.113.11', emergency_allowlisted: true },
      ],
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ipAccessControl.failureStates.viewRule')
    expect(wrapper.text()).toContain('admin.ipAccessControl.failureStates.handleAllow')
    expect(wrapper.text()).toContain('admin.ipAccessControl.failureStates.emergencyAllowActive')
    const manualButton = wrapper.findAll('button').find((button) => button.text() === 'admin.ipAccessControl.failureStates.manualBlock')
    expect(manualButton?.attributes('disabled')).toBeDefined()

    const vm = wrapper.vm as any
    expect(vm.manualBlockDisabledReason({ ...base, runtime_enforcement_enabled: false }))
      .toBe('admin.ipAccessControl.failureStates.manualBlockDisabledEnforcement')
    vm.failureStatesUnavailable = true
    expect(vm.manualBlockDisabledReason({ ...base, runtime_enforcement_enabled: true }))
      .toBe('admin.ipAccessControl.failureStates.manualBlockDisabledStale')

    vm.failureStatesUnavailable = false
    vm.settingsUnavailable = true
    expect(vm.manualBlockDisabledReason({ ...base, runtime_enforcement_enabled: true })).toBe('')
    vm.settingsUnavailable = false
    expect(vm.manualBlockDisabledReason({ ...base, suppressed_by_allow_rule: true }))
      .toBe('admin.ipAccessControl.failureStates.manualBlockDisabledAllow')
    expect(vm.manualBlockDisabledReason({ ...base, emergency_allowlisted: true }))
      .toBe('admin.ipAccessControl.failureStates.manualBlockDisabledEmergencyAllow')
    expect(vm.manualBlockDisabledReason({ ...base, active_block_rule: true }))
      .toBe('admin.ipAccessControl.failureStates.manualBlockDisabledAlreadyBlocked')
    listRules.mockClear()
    await vm.focusRulesForFailureState({ ...base, block_rule_ip_or_cidr: '203.0.113.0/24' })
    expect(vm.filters.query).toBe('203.0.113.0/24')
    expect(listRules).toHaveBeenCalledWith(expect.objectContaining({ query: '203.0.113.0/24', status: 'active' }))
    wrapper.unmount()
  })

  it('does not submit a stale manual-block target after the row disappears', async () => {
    const state = {
      normalized_ip: '203.0.113.8',
      failure_count: 1,
      failure_threshold: 2,
      window_started_at: '2026-08-17T00:00:00Z',
      last_failed_at: '2026-08-17T00:00:00Z',
      window_expires_at: '2026-08-17T00:15:00Z',
      active_block_rule: false,
      runtime_enforcement_enabled: true,
      suppressed_by_allow_rule: false,
      emergency_allowlisted: false,
      effectively_blocked: false,
      as_of: '2026-08-17T00:00:00Z',
    }
    listFailureStates.mockResolvedValue({ ...emptyPage(), items: [state], total: 1 })
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any

    await vm.confirmManualBlock(state)
    expect(vm.manualBlockTarget?.normalized_ip).toBe(state.normalized_ip)
    vm.failureStates = []
    blockFailureState.mockClear()
    showError.mockClear()
    await vm.manualBlockFailureState()

    expect(blockFailureState).not.toHaveBeenCalled()
    expect(vm.manualBlockTarget).toBeNull()
    expect(showError).toHaveBeenCalledWith('admin.ipAccessControl.failureStates.manualBlockDisabledStale')
    wrapper.unmount()
  })

  it('refreshes failure states only after an explicit action', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountView()
      await flushPromises()
      listFailureStates.mockClear()

      vi.advanceTimersByTime(60_000)
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()

      expect(listFailureStates).not.toHaveBeenCalled()
      const vm = wrapper.vm as any
      vm.refreshFailureStates()
      await flushPromises()
      expect(listFailureStates).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('marks displayed failure data stale when refresh fails', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.failureStatesUpdatedAt).toBeInstanceOf(Date)

    listFailureStates.mockRejectedValueOnce(new Error('unavailable'))
    await vm.loadFailureStates()

    expect(vm.failureStatesUnavailable).toBe(true)
    expect(vm.failureStatesUpdatedAt).toBeInstanceOf(Date)
    wrapper.unmount()
  })

  it('keeps the newest failure-state response when refreshes overlap', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any

    let resolveOlder!: (value: unknown) => void
    listFailureStates.mockReset()
    listFailureStates
      .mockReturnValueOnce(new Promise((resolve) => { resolveOlder = resolve }))
      .mockResolvedValueOnce({
        ...emptyPage(),
        total: 1,
        items: [{
          normalized_ip: '203.0.113.20',
          failure_count: 2,
          failure_threshold: 2,
          window_started_at: '2026-08-17T00:00:00Z',
          last_failed_at: '2026-08-17T00:01:00Z',
          window_expires_at: '2026-08-17T00:15:00Z',
          active_block_rule: true,
          runtime_enforcement_enabled: true,
          suppressed_by_allow_rule: false,
          emergency_allowlisted: false,
          effectively_blocked: true,
          as_of: '2026-08-17T00:01:00Z',
        }],
      })

    const olderRequest = vm.loadFailureStates()
    const newerRequest = vm.loadFailureStates()
    await newerRequest
    expect(vm.failureStates[0].normalized_ip).toBe('203.0.113.20')

    resolveOlder({
      ...emptyPage(),
      total: 1,
      items: [{
        normalized_ip: '203.0.113.19',
        failure_count: 1,
        failure_threshold: 2,
        window_started_at: '2026-08-17T00:00:00Z',
        last_failed_at: '2026-08-17T00:00:30Z',
        window_expires_at: '2026-08-17T00:15:00Z',
        active_block_rule: false,
        runtime_enforcement_enabled: true,
        suppressed_by_allow_rule: false,
        emergency_allowlisted: false,
        effectively_blocked: false,
        as_of: '2026-08-17T00:00:30Z',
      }],
    })
    await olderRequest

    expect(vm.failureStates[0].normalized_ip).toBe('203.0.113.20')
    expect(vm.failureStatesLoading).toBe(false)
    wrapper.unmount()
  })

  it('keeps the manual-block duration editable when automatic blocking is off', async () => {
    getSettings.mockResolvedValue({
      ...defaultSettings,
      login_failure_auto_block_enabled: false,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ipAccessControl.protection.duration')
    expect(wrapper.text()).not.toContain('admin.ipAccessControl.protection.threshold')
    expect(wrapper.text()).not.toContain('admin.ipAccessControl.protection.window')
    wrapper.unmount()
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
