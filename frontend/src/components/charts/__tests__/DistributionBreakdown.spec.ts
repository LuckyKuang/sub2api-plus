import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ModelDistributionChart from '../ModelDistributionChart.vue'
import GroupDistributionChart from '../GroupDistributionChart.vue'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'
import { cssPixels, installAppStyles } from '@/__tests__/appStyles'

enableAutoUnmount(afterEach)
let removeStyles: () => void
beforeAll(async () => { removeStyles = await installAppStyles() })
afterAll(() => removeStyles?.())
const { getUserBreakdown } = vi.hoisted(() => ({ getUserBreakdown: vi.fn() }))
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown }))
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

// The UI requirement is a single column contract: each user's Requests,
// Tokens, Actual, optional Account Cost, and Standard belongs under that
// same parent header. Distinct amounts expose shifted/missing cost columns.
const usage = { requests: 7, total_tokens: 800, actual_cost: 1.25, account_cost: 2.5, cost: 3.75 }
const user = { user_id: 42, email: 'long-user-name@example.com', ...usage }
const cases = [
  { name: 'model with account cost', component: ModelDistributionChart, props: { modelStats: [{ model: 'model-a', input_tokens: 600, output_tokens: 200, cache_creation_tokens: 0, cache_read_tokens: 0, ...usage }] }, accountCost: true },
  { name: 'model without account cost', component: ModelDistributionChart, props: { modelStats: [{ model: 'model-a', input_tokens: 600, output_tokens: 200, cache_creation_tokens: 0, cache_read_tokens: 0, ...usage }], showAccountCost: false }, accountCost: false },
  { name: 'group with account cost', component: GroupDistributionChart, props: { groupStats: [{ group_id: 1, group_name: 'group-a', ...usage }] }, accountCost: true },
  { name: 'group without account cost', component: GroupDistributionChart, props: { groupStats: [{ group_id: 1, group_name: 'group-a', ...usage }], showAccountCost: false }, accountCost: false },
  { name: 'endpoint', component: EndpointDistributionChart, props: { endpointStats: [{ endpoint: '/v1/messages', ...usage }] }, accountCost: false },
] as const

describe.each(cases)('$name expanded user columns', ({ component, props, accountCost }) => {
  beforeEach(() => getUserBreakdown.mockReset())

  function mountChart() {
    return mount(component, { attachTo: document.body, props: { ...props, startDate: '2026-10-01', endDate: '2026-10-08' } })
  }

  it('keeps user amounts in the same table and columns as their headers, then collapses', async () => {
    getUserBreakdown.mockResolvedValue({ users: [user] })
    const wrapper = mountChart()
    const table = wrapper.get('table')
    const headers = table.findAll('thead th').map(cell => cell.text())
    expect(headers.slice(1)).toEqual([
      'admin.dashboard.requests', 'admin.dashboard.tokens', 'admin.dashboard.actual',
      ...(accountCost ? ['admin.dashboard.accountCost'] : []), 'admin.dashboard.standard',
    ])
    await table.get('tbody tr').trigger('click')
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledTimes(1)
    // Sharing a table grid (not an independently sized nested table) makes
    // header/body alignment hold regardless of user-name or amount length.
    expect(wrapper.findAll('table')).toHaveLength(1)
    const rows = table.findAll('tbody > tr')
    expect(rows).toHaveLength(2)
    const cells = rows[1].findAll('td')
    expect(cells.map(cell => cell.text())).toEqual([
      user.email, '7', '800 (100.0%)', '$1.25', ...(accountCost ? ['$2.50'] : []), '$3.75',
    ])
    expect(cells).toHaveLength(headers.length)
    expect(cells.every(cell => cell.element.closest('table') === table.element)).toBe(true)
    cells.slice(1).forEach((cell, index) => {
      const valueStyle = getComputedStyle(cell.element)
      const headerStyle = getComputedStyle(table.findAll('thead th')[index + 1].element)
      expect(valueStyle.textAlign).toBe('right')
      expect(headerStyle.textAlign).toBe('right')
      expect(cssPixels(valueStyle.paddingRight)).toBe(cssPixels(headerStyle.paddingRight))
    })

    await rows[0].trigger('click')
    expect(table.findAll('tbody > tr')).toHaveLength(1)
    expect(table.text()).not.toContain(user.email)
  })

  it('spans the actual parent columns while loading and when no users are returned', async () => {
    let resolve!: (value: { users: typeof user[] }) => void
    getUserBreakdown.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountChart()
    const table = wrapper.get('table')
    await table.get('tbody tr').trigger('click')
    let rows = table.findAll('tbody > tr')
    expect(rows).toHaveLength(2)
    expect(rows[1].findAll('td')).toHaveLength(1)
    expect(rows[1].get('td').attributes('colspan')).toBe(accountCost ? '6' : '5')

    resolve({ users: [] })
    await flushPromises()
    rows = table.findAll('tbody > tr')
    expect(rows[1].get('td').attributes('colspan')).toBe(accountCost ? '6' : '5')
    expect(rows[1].text()).toContain('admin.dashboard.noDataAvailable')
    expect(wrapper.findAll('table')).toHaveLength(1)
  })
})
