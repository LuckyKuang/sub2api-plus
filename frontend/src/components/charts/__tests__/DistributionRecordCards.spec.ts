import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupDistributionChart from '../GroupDistributionChart.vue'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'
import ModelDistributionChart from '../ModelDistributionChart.vue'
import { getUserBreakdown, type UserBreakdownResponse } from '@/api/admin/dashboard'
import type { UserBreakdownItem } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' } }))
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown: vi.fn() }))

const stats = { requests: 10, total_tokens: 1000, actual_cost: 0.2, account_cost: 0.1, cost: 1 }
const cases = [
  {
    name: 'group', component: GroupDistributionChart, columns: 6,
    props: { groupStats: [{ ...stats, group_id: 1, group_name: 'Group A' }, { ...stats, group_id: 2, group_name: 'Group B' }] },
  },
  {
    name: 'endpoint', component: EndpointDistributionChart, columns: 5,
    props: { endpointStats: [{ ...stats, endpoint: '/v1/responses' }, { ...stats, endpoint: '/v1/messages' }] },
  },
  {
    name: 'model', component: ModelDistributionChart, columns: 6,
    props: {
      modelStats: ['model-a', 'model-b'].map(model => ({
        ...stats, model, input_tokens: 700, output_tokens: 300, cache_creation_tokens: 0, cache_read_tokens: 0,
      })),
    },
  },
]

const users: UserBreakdownItem[] = [1, 2].map(user_id => ({
  ...stats, user_id, email: `user${user_id}@example.com`, input_tokens: 400, output_tokens: 100,
  cache_tokens: 0, total_tokens: 500,
}))

describe.each(cases)('$name distribution record cards', (config) => {
  function mountChart() {
    return mount(defineComponent({ setup: () => () => h(config.component, config.props) }))
  }

  it('keeps the summary, loading state, and user details inside the same rounded record', async () => {
    let resolveResponse!: (response: UserBreakdownResponse) => void
    vi.mocked(getUserBreakdown).mockReturnValueOnce(new Promise(resolve => { resolveResponse = resolve }))
    const wrapper = mountChart()
    const groups = wrapper.findAll('tbody.record-group')
    expect(groups).toHaveLength(2)
    const record = groups[0]
    const summary = record.get('tr')
    expect(summary.findAll('td')).toHaveLength(config.columns)
    expect(summary.findAll('td')[0].attributes('data-record-edges')).toBe('t b l tl bl')
    expect(summary.findAll('td').at(-1)!.attributes('data-record-edges')).toBe('t b r tr br')

    await summary.trigger('click')
    expect(record.findAll('tr')).toHaveLength(2)
    const loading = record.get('td[colspan]')
    expect(loading.attributes('colspan')).toBe(String(config.columns))
    expect(loading.attributes('data-record-edges')).toBe('b l r bl br')
    expect(summary.findAll('td')[0].attributes('data-record-edges')).toBe('t l tl')
    expect(summary.findAll('td').at(-1)!.attributes('data-record-edges')).toBe('t r tr')

    resolveResponse({ users, start_date: '2026-10-01', end_date: '2026-10-24' })
    await flushPromises()
    const rows = record.findAll('tr')
    expect(rows).toHaveLength(3)
    expect(rows[1].text()).toContain('user1@example.com')
    expect(rows[2].text()).toContain('user2@example.com')
    expect(rows[1].findAll('td')).toHaveLength(config.columns)
    expect(rows[1].findAll('td').map(cell => cell.attributes('data-record-edges')))
      .toEqual(['l', ...Array(config.columns - 2).fill(''), 'r'])
    expect(rows[2].findAll('td').map(cell => cell.attributes('data-record-edges')))
      .toEqual(['b l bl', ...Array(config.columns - 2).fill('b'), 'b r br'])
    expect(groups[1].findAll('tr')).toHaveLength(1)

    await summary.trigger('click')
    expect(record.findAll('tr')).toHaveLength(1)
    expect(summary.findAll('td')[0].attributes('data-record-edges')).toBe('t b l tl bl')
    expect(summary.findAll('td').at(-1)!.attributes('data-record-edges')).toBe('t b r tr br')
    wrapper.unmount()
  })

  it('closes the record around an empty details response', async () => {
    vi.mocked(getUserBreakdown).mockResolvedValueOnce({ users: [], start_date: '', end_date: '' })
    const wrapper = mountChart()
    const record = wrapper.get('tbody.record-group')
    await record.get('tr').trigger('click')
    await flushPromises()
    const empty = record.get('td[colspan]')
    expect(empty.text()).toBe('admin.dashboard.noDataAvailable')
    expect(empty.attributes('colspan')).toBe(String(config.columns))
    expect(empty.attributes('data-record-edges')).toBe('b l r bl br')
    wrapper.unmount()
  })
})
