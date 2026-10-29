import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'
import type { UserSubscription } from '@/types'
import { notifyLocaleChanged } from '@/utils/localeRefresh'

const { getSubscriptions, push, showError } = vi.hoisted(() => ({ getSubscriptions: vi.fn(), push: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/subscriptions', () => ({ default: { getMySubscriptions: getSubscriptions } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null, showError }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => key === 'pagination.goToPage' ? `Page ${params?.page}` : key,
}) }))
enableAutoUnmount(afterEach)

const subscription = (id: number): UserSubscription => ({
  id, user_id: 7, group_id: id, status: 'active', starts_at: '2026-10-01T00:00:00Z', expires_at: null,
  daily_usage_usd: 0, weekly_usage_usd: 0, monthly_usage_usd: 0, five_hour_usage_usd: 0,
  daily_window_start: null, weekly_window_start: null, monthly_window_start: null, five_hour_window_start: null,
  created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
})
const subscriptions = (count: number) => Array.from({ length: count }, (_, index) => subscription(index + 1))
const render = () => mount(SubscriptionsView, { global: { stubs: {
  AppLayout: { name: 'AppLayout', props: { workspace: Boolean }, template: '<div><slot /></div>' },
  Icon: true,
} } })
beforeEach(() => { vi.clearAllMocks(); getSubscriptions.mockResolvedValue(subscriptions(28)) })

describe('user subscription card pagination', () => {
  // Approved user review: exactly nine real subscriptions per page, in the
  // existing order; the final partial page must remain reachable.
  it('paginates 28 subscriptions as 9, 9, 9 and 1 with fixed header and footer', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.getComponent({ name: 'AppLayout' }).props('workspace')).toBe(true)
    const activity = wrapper.get('[data-testid="subscriptions-activity"]')
    const heading = wrapper.get('[data-testid="subscriptions-heading"]')
    const pager = wrapper.get('.subscriptions-pagination')
    expect(activity.element.contains(heading.element)).toBe(false)
    expect(activity.element.contains(pager.element)).toBe(false)
    expect(wrapper.findAll('[data-testid="subscription-card"]')).toHaveLength(9)
    expect(wrapper.findAll('[data-testid="subscription-card"] h3').map(title => title.text())).toEqual([
      'Group #1', 'Group #2', 'Group #3', 'Group #4', 'Group #5', 'Group #6', 'Group #7', 'Group #8', 'Group #9',
    ])
    for (const number of [2, 3]) {
      await wrapper.get(`button[aria-label="Page ${number}"]`).trigger('click')
      expect(wrapper.findAll('[data-testid="subscription-card"]')).toHaveLength(9)
    }
    await wrapper.get('button[aria-label="Page 4"]').trigger('click')
    expect(wrapper.findAll('[data-testid="subscription-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="subscription-card"] h3').text()).toBe('Group #28')
    expect(wrapper.get('button[aria-label="pagination.next"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
    expect(getSubscriptions).toHaveBeenCalledTimes(1)
    expect(push).not.toHaveBeenCalled()
  })

  it('retains working renewal actions on later pages and clamps pagination after a reload', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('button[aria-label="Page 4"]').trigger('click')
    await wrapper.get('[data-testid="subscription-card"] button').trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/purchase', query: { tab: 'subscription', group: '28' } })
    getSubscriptions.mockResolvedValueOnce(subscriptions(10))
    notifyLocaleChanged(); await flushPromises()
    expect(wrapper.findAll('[data-testid="subscription-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="subscription-card"] h3').text()).toBe('Group #10')
    expect(wrapper.get('button[aria-current="page"]').text()).toBe('2')
  })

  it('keeps the empty list inside the card scroll area with a fixed zero-count footer', async () => {
    getSubscriptions.mockResolvedValueOnce([])
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('[data-testid="subscription-card"]')).toHaveLength(0)
    expect(wrapper.get('[data-testid="subscriptions-activity"]').text()).toContain('userSubscriptions.noActiveSubscriptions')
    expect(wrapper.get('.subscriptions-pagination').text()).toContain('userSubscriptions.count')
    expect(wrapper.find('button[aria-label="pagination.next"]').exists()).toBe(false)
  })
})
