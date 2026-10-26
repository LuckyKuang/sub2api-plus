import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { effectScope, nextTick, ref, type Component } from 'vue'
import { useConsoleAppearance } from '@/composables/useConsoleAppearance'
import { defaultConsoleAppearance } from '@/types/appearance'
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
  // docs/UI_INTERACTION.md: canvas hover cards follow global transparency and
  // theme changes live; their text and 1px borders remain visible at 100%.
  it.each(cases)('$name updates mounted canvas options from the published appearance in both themes', async ({ component, props }) => {
    document.documentElement.classList.remove('dark')
    const scope = effectScope()
    const appearance = ref(defaultConsoleAppearance())
    scope.run(() => useConsoleAppearance(() => appearance.value))
    try {
      const wrapper = mount(component, {
        props,
        global: { stubs: { HelpTooltip: true, EmptyState: true, LoadingSpinner: true } },
      })
      // Read the actual options boundary passed to the canvas renderer, not
      // the helper's return value or a separately recreated color formula.
      const canvasOptions = () => wrapper.findComponent({ name: 'CanvasChartStub' }).props('options').plugins.tooltip
      const lightContent = { cornerRadius: 16, caretSize: 0, padding: 12, titleColor: '#182e31', bodyColor: '#52676b', footerColor: '#182e31', borderColor: 'rgba(100, 116, 139, 0.34)', borderWidth: 1 }
      const darkContent = { cornerRadius: 16, caretSize: 0, padding: 12, titleColor: '#eaf3f2', bodyColor: '#c1cdd1', footerColor: '#eaf3f2', borderColor: 'rgba(203, 213, 225, 0.28)', borderWidth: 1 }
      expect(canvasOptions()).toMatchObject({ ...lightContent, backgroundColor: 'rgba(248, 250, 249, 1)' })

      appearance.value.card_transparency = 45
      await nextTick()
      expect(canvasOptions()).toMatchObject({ ...lightContent, backgroundColor: 'rgba(248, 250, 249, 0.55)' })

      appearance.value.card_transparency = 100
      await nextTick()
      expect(canvasOptions()).toMatchObject({ ...lightContent, backgroundColor: 'rgba(248, 250, 249, 0)' })

      document.documentElement.classList.add('dark')
      await new Promise(resolve => setTimeout(resolve, 0))
      await nextTick()
      expect(canvasOptions()).toMatchObject({ ...darkContent, backgroundColor: 'rgba(38, 44, 45, 0)' })

      appearance.value.card_transparency = 0
      await nextTick()
      expect(canvasOptions()).toMatchObject({ ...darkContent, backgroundColor: 'rgba(38, 44, 45, 1)' })

      document.documentElement.classList.remove('dark')
      await new Promise(resolve => setTimeout(resolve, 0))
      await nextTick()
      expect(canvasOptions()).toMatchObject({ ...lightContent, backgroundColor: 'rgba(248, 250, 249, 1)' })
    } finally {
      scope.stop()
    }
  })
})
