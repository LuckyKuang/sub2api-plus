import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'

const { getStats, getTrend, getModels, getRecent, getQuotas, refreshUser } = vi.hoisted(() => ({
  getStats: vi.fn(), getTrend: vi.fn(), getModels: vi.fn(), getRecent: vi.fn(), getQuotas: vi.fn(), refreshUser: vi.fn(),
}))
vi.mock('@/api/usage', () => ({ usageAPI: {
  getDashboardStats: getStats, getDashboardTrend: getTrend, getDashboardModels: getModels, getByDateRange: getRecent,
} }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: getQuotas }))
vi.mock('@/composables/useUserView', () => ({ useUserView: () => ({ user: { id: 7, balance: 5 }, isSimpleMode: false, refreshUser }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const stats = { today_requests: 12, timezone: 'Europe/Amsterdam' }
const quotas = [{ platform: 'openai', daily_limit_usd: 10, daily_usage_usd: 2 }]
const summary = { props: ['section', 'stats', 'platformQuotas'], template: '<div :data-section="section">{{ stats.today_requests }}</div>' }
const charts = { props: ['showToolbar'], template: '<div data-testid="chart-content" />' }
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  getStats.mockResolvedValue(stats)
  getTrend.mockResolvedValue({ trend: [] })
  getModels.mockResolvedValue({ models: [] })
  getRecent.mockResolvedValue({ items: [] })
  getQuotas.mockResolvedValue({ platform_quotas: quotas })
  refreshUser.mockResolvedValue({ id: 7, balance: 5 })
})
afterEach(() => { vi.useRealTimers() })

describe('user dashboard workspace', () => {
  it('retains the summary and one toolbar while switching charts, quotas and recent usage', async () => {
    const wrapper = mount(DashboardView, { attachTo: document.body, global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, DateRangePicker: true, Select: true, RefreshControl: true,
      UserDashboardStats: summary, UserDashboardCharts: charts, UserDashboardRecentUsage: { template: '<div />' },
    } } })
    await flushPromises()
    const toolbar = wrapper.get('.dashboard-toolbar').element
    const summaryCard = wrapper.get('[data-section="summary"]').element
    const chart = wrapper.get('[data-testid="chart-content"]').element
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(3)
    expect(wrapper.findComponent(charts).props('showToolbar')).toBe(false)
    expect(wrapper.findAllComponents(summary)[1].props('platformQuotas')).toEqual(quotas)

    for (const [index, panel] of [[1, 'quotas'], [2, 'recent'], [0, 'charts']] as const) {
      await tabs[index].trigger('click')
      expect(wrapper.get(`[data-testid="dashboard-${panel}-panel"]`).isVisible()).toBe(true)
      expect(wrapper.get('[data-section="summary"]').isVisible()).toBe(true)
      expect(wrapper.get('.dashboard-toolbar').element).toBe(toolbar)
      expect(wrapper.get('[data-section="summary"]').element).toBe(summaryCard)
    }
    expect(wrapper.get('[data-testid="chart-content"]').element).toBe(chart)
    expect(wrapper.findAll('.dashboard-toolbar')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('dashboard.quickActions')
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getTrend).toHaveBeenCalledTimes(1)
    expect(getQuotas).toHaveBeenCalledTimes(1)
  })
})
