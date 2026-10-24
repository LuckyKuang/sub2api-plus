import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick, type Component } from 'vue'
import GroupDistributionChart from '../GroupDistributionChart.vue'
import TokenUsageTrend from '../TokenUsageTrend.vue'
import DailyRevenueChart from '@/components/admin/payment/DailyRevenueChart.vue'
import OpsLatencyChart from '@/views/admin/ops/components/OpsLatencyChart.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})
vi.mock('vue-chartjs', () => {
  const chart = { name: 'CanvasChartStub', props: ['data', 'options'], template: '<canvas />' }
  return { Doughnut: chart, Line: chart, Bar: chart }
})

enableAutoUnmount(afterEach)
afterEach(() => document.documentElement.classList.remove('dark'))

const cases: { name: string; component: Component; props: Record<string, unknown> }[] = [
  {
    name: 'distribution',
    component: GroupDistributionChart,
    props: { groupStats: [{ group_id: 1, group_name: 'Example', requests: 2, total_tokens: 100, cost: 1, actual_cost: 1 }] },
  },
  {
    name: 'usage trend',
    component: TokenUsageTrend,
    props: { trendData: [{ date: '2026-10-24', requests: 2, input_tokens: 40, output_tokens: 60, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 100, cost: 1, actual_cost: 1 }] },
  },
  {
    name: 'payment revenue',
    component: DailyRevenueChart,
    props: { data: [{ date: '2026-10-24', amount: { USD: 10 }, count: 2 }] },
  },
  {
    name: 'latency histogram',
    component: OpsLatencyChart,
    props: { latencyData: { start_time: '2026-10-24T00:00:00Z', end_time: '2026-10-25T00:00:00Z', platform: 'openai', total_requests: 2, buckets: [{ range: '0-100ms', count: 2 }] }, loading: false },
  },
]

describe('chart hover cards', () => {
  // User requirement: every hover surface is a rounded card without a pointed
  // arrow, and follows the light/dark theme while the page stays mounted.
  it.each(cases)('$name passes card appearance to the canvas renderer in both themes', async ({ component, props }) => {
    document.documentElement.classList.remove('dark')
    const wrapper = mount(component, {
      props,
      global: { stubs: { HelpTooltip: true, EmptyState: true, LoadingSpinner: true } },
    })
    // Read the actual boundary passed to Chart.js, rather than testing only the helper.
    const canvasOptions = () => wrapper.findComponent({ name: 'CanvasChartStub' }).props('options').plugins.tooltip
    expect(canvasOptions()).toMatchObject({ cornerRadius: 16, caretSize: 0, padding: 12, backgroundColor: '#f8faf9', titleColor: '#182e31', bodyColor: '#52676b' })

    document.documentElement.classList.add('dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(canvasOptions()).toMatchObject({ cornerRadius: 16, caretSize: 0, padding: 12, backgroundColor: '#262c2d', titleColor: '#eaf3f2', bodyColor: '#c1cdd1' })

    document.documentElement.classList.remove('dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(canvasOptions()).toMatchObject({ backgroundColor: '#f8faf9', titleColor: '#182e31', bodyColor: '#52676b' })
  })
})
