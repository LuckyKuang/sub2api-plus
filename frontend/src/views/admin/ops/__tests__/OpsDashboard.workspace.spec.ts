import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import OpsDashboard from '../OpsDashboard.vue'
import OpsDashboardHeader from '../components/OpsDashboardHeader.vue'
import OpsSystemLogTable from '../components/OpsSystemLogTable.vue'

enableAutoUnmount(afterEach)
const hosts: HTMLElement[] = []
afterEach(() => {
  hosts.splice(0).forEach((host) => host.remove())
})

const { api, getGroups, showError } = vi.hoisted(() => ({
  api: {
    getAdvancedSettings: vi.fn(),
    getMetricThresholds: vi.fn(),
    getDashboardSnapshotV2: vi.fn(),
    getThroughputTrend: vi.fn(),
    getLatencyHistogram: vi.fn(),
    getErrorDistribution: vi.fn(),
    getRealtimeTrafficSummary: vi.fn(),
    listAlertEvents: vi.fn(),
    listSystemLogs: vi.fn(),
    getSystemLogSinkHealth: vi.fn(),
    getRuntimeLogConfig: vi.fn(),
  },
  getGroups: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin/ops', () => ({ opsAPI: api }))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: getGroups } } }))
vi.mock('@/stores', () => ({
  useAdminSettingsStore: () => ({
    opsMonitoringEnabled: true,
    opsRealtimeMonitoringEnabled: true,
    opsQueryModeDefault: 'auto',
    fetch: vi.fn().mockResolvedValue(undefined),
  }),
  useAppStore: () => ({ showError, showSuccess: vi.fn() }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: vi.fn().mockResolvedValue(undefined) }),
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const AppLayoutStub = defineComponent({
  props: { workspace: Boolean },
  template: '<main><slot /></main>',
})

function mountDashboard() {
  // JSDOM invalidates computed-style caches for connected nodes when v-show changes.
  // Keep the page attached so repeated visibility assertions observe the current tab.
  const host = document.createElement('div')
  document.body.appendChild(host)
  hosts.push(host)
  return mount(OpsDashboard, {
    attachTo: host,
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Select: true,
        DateTimePicker: true,
        HelpTooltip: true,
        Icon: true,
        Pagination: true,
        BaseDialog: true,
        OpsConcurrencyCard: true,
        OpsSwitchRateTrendChart: true,
        OpsThroughputTrendChart: true,
        OpsLatencyChart: true,
        OpsErrorDistributionChart: true,
        OpsErrorTrendChart: true,
        OpsOpenAITokenStatsCard: true,
        OpsSettingsDialog: true,
        OpsErrorDetailsModal: true,
        OpsErrorDetailModal: true,
        OpsRequestDetailsModal: true,
        OpsAlertRulesCard: true,
      },
    },
  })
}

describe('Ops dashboard workspace tabs', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getGroups.mockResolvedValue([])
    api.getAdvancedSettings.mockResolvedValue({
      display_alert_events: true,
      display_openai_token_stats: false,
      auto_refresh_enabled: false,
      auto_refresh_interval_seconds: 30,
    })
    api.getMetricThresholds.mockResolvedValue(null)
    api.getDashboardSnapshotV2.mockResolvedValue({
      overview: { tps: { avg: 10 } }, throughput_trend: { points: [] }, error_trend: { points: [] },
    })
    api.getThroughputTrend.mockResolvedValue({ points: [] })
    api.getLatencyHistogram.mockResolvedValue(null)
    api.getErrorDistribution.mockResolvedValue(null)
    api.getRealtimeTrafficSummary.mockResolvedValue({ enabled: true, summary: null })
    api.listAlertEvents.mockResolvedValue([])
    api.listSystemLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.getSystemLogSinkHealth.mockResolvedValue({
      queue_depth: 0, queue_capacity: 5000, dropped_count: 0,
      write_failed_count: 0, written_count: 1, avg_write_delay_ms: 0,
    })
    api.getRuntimeLogConfig.mockResolvedValue({
      level: 'info', persist_access_logs: false, enable_sampling: false,
      sampling_initial: 100, sampling_thereafter: 100, caller: true,
      stacktrace_level: 'error', retention_days: 30,
    })
  })

  it('keeps the toolbar outside each scrolling panel and preserves independent log drafts without fetching on tab changes', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.findComponent(AppLayoutStub).props('workspace')).toBe(true)
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((tab) => tab.text())).toEqual([
      'workspace.opsMonitoring', 'workspace.alerts', 'workspace.systemLogs',
    ])
    const toolbar = wrapper.get('[data-testid="ops-toolbar"]')
    expect(toolbar.element.closest('.workspace-scroll')).toBeNull()
    expect(wrapper.get('[data-testid="ops-panel-overview"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="ops-panel-alerts"]').isVisible()).toBe(false)
    expect(wrapper.get('[data-testid="ops-panel-logs"]').isVisible()).toBe(false)

    await tabs[2].trigger('click')
    const logPanel = wrapper.get('[data-testid="ops-panel-logs"]')
    const logComponent = wrapper.findComponent(OpsSystemLogTable)
    const initialLogInstance = logComponent.vm.$
    const initialLogElement = logComponent.element
    const host = logPanel.findAll('label').find((label) => label.text().includes('admin.ops.systemLogs.host'))!
    const retention = logPanel.findAll('label').find((label) => label.text().includes('admin.ops.systemLogs.retentionDays'))!
    await host.get('input').setValue('api-node-2')
    await retention.get('input').setValue('90')
    api.listSystemLogs.mockClear()
    api.listAlertEvents.mockClear()
    api.getRuntimeLogConfig.mockClear()
    api.getDashboardSnapshotV2.mockClear()

    await tabs[1].trigger('click')
    expect(wrapper.get('[data-testid="ops-panel-alerts"]').isVisible()).toBe(true)
    await tabs[0].trigger('click')
    await tabs[2].trigger('click')
    const revisitedLogComponent = wrapper.findComponent(OpsSystemLogTable)
    expect(revisitedLogComponent.vm.$).toBe(initialLogInstance)
    expect(revisitedLogComponent.element).toBe(initialLogElement)
    expect((host.get('input').element as HTMLInputElement).value).toBe('api-node-2')
    expect((retention.get('input').element as HTMLInputElement).value).toBe('90')
    expect(api.listSystemLogs).not.toHaveBeenCalled()
    expect(api.listAlertEvents).not.toHaveBeenCalled()
    expect(api.getRuntimeLogConfig).not.toHaveBeenCalled()
    expect(api.getDashboardSnapshotV2).not.toHaveBeenCalled()
    expect(wrapper.findComponent(OpsDashboardHeader).props('timeRange')).toBe('1h')

    await logPanel.findAll('button').find((button) => button.text() === 'admin.ops.systemLogs.search')!.trigger('click')
    await flushPromises()
    expect(api.listSystemLogs).toHaveBeenCalledWith(expect.objectContaining({ host: 'api-node-2', time_range: '1h' }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps the three tabs available and shows the disabled state when alert display is off', async () => {
    api.getAdvancedSettings.mockResolvedValue({
      display_alert_events: false, display_openai_token_stats: false,
      auto_refresh_enabled: false, auto_refresh_interval_seconds: 30,
    })
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('[data-testid="ops-panel-alerts"]').text()).toContain('common.disabled')
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(3)
    expect(api.listAlertEvents).not.toHaveBeenCalled()
  })
})
