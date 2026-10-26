import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderQuotaCell from '../CNProviderQuotaCell.vue'
import UsageProgressBar from '../UsageProgressBar.vue'
import type { Account } from '@/types'

const { queryQuota } = vi.hoisted(() => ({
  queryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryQuota }
  }
}))

// 保留 vue-i18n 真实导出：UsageProgressBar 依赖 @/utils/format → @/i18n，
// 其模块级 createI18n 需要真实 createI18n 存在。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const account = {
  id: 7,
  platform: 'zhipu',
  type: 'apikey',
  credentials: { account_mode: 'coding' },
  extra: {
    zhipu_5h_used_percent: 0,
    zhipu_weekly_used_percent: 27,
    zhipu_5h_reset_at: '2026-08-18T12:30:00+08:00',
    zhipu_weekly_reset_at: '2026-08-22T00:00:00+08:00',
    zhipu_usage_updated_at: new Date().toISOString()
  }
} as Account

describe('CNProviderQuotaCell', () => {
  beforeEach(() => {
    queryQuota.mockReset()
  })

  it('renders tier rows through the shared UsageProgressBar inside the account table cell', async () => {
    queryQuota.mockResolvedValue({
      success: true,
      tiers: [
        { window: '5h', used_percent: 0, reset_at: '2026-08-18T12:30:00+08:00' },
        { window: 'weekly', used_percent: 27, reset_at: '2026-08-22T00:00:00+08:00' }
      ]
    })
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })

    const root = wrapper.get('[data-test="cn-provider-quota"]')
    expect(root.classes()).toContain('min-w-[220px]')

    // 新鲜快照：挂载即渲染条形图，不触发探测
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('27%')

    // probe 按钮文案是动词 key（i18n mock 返回 key 本身），点击触发查询
    const probeButton = root.get('[data-test="cn-provider-quota-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')
    await probeButton.trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(account.id)

    // tier 行由 UsageProgressBar 渲染：数量、label/color/utilization/reset 逐行对齐
    expect(root.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(2)
    const bars = root.findAllComponents(UsageProgressBar)
    expect(bars).toHaveLength(2)
    expect(bars[0].props('label')).toBe('admin.accounts.cnProviders.window5h')
    expect(bars[0].props('utilization')).toBe(0)
    expect(bars[0].props('color')).toBe('indigo')
    expect(bars[0].props('resetsAt')).toBe('2026-08-18T12:30:00+08:00')
    expect(bars[1].props('label')).toBe('admin.accounts.cnProviders.windowWeekly')
    expect(bars[1].props('utilization')).toBe(27)
    expect(bars[1].props('color')).toBe('emerald')
    expect(bars[1].props('resetsAt')).toBe('2026-08-22T00:00:00+08:00')
  })

  it('labels the refresh control with an explicit action verb, not a data caption', async () => {
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })
    await flushPromises()

    // The snapshot is fresh (usage_updated_at = now): bars render without probing.
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('27%')

    // The control reads as an action ("query"), unlike the old noun label
    // ("5-hour window/weekly window") which looked like a passive caption.
    // The i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-quota-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(account.id)
  })
  it('renders independent Kimi monthly total and code snapshots and keeps them after an invalid probe', async () => {
    const kimi = {
      ...account,
      id: 1707,
      platform: 'kimi',
      type: 'oauth',
      credentials: { account_mode: 'coding', oauth_provider: 'kimi' },
      extra: {
        kimi_monthly_used_percent: 90,
        kimi_monthly_reset_at: '2026-12-01T00:00:00Z',
        kimi_monthly_code_used_percent: 10,
        kimi_monthly_code_reset_at: '2026-12-02T00:00:00Z',
        kimi_usage_updated_at: new Date().toISOString()
      }
    } as Account
    const wrapper = mount(CNProviderQuotaCell, { props: { account: kimi } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    const bars = wrapper.findAllComponents(UsageProgressBar)
    expect(bars.map(bar => [bar.props('label'), bar.props('utilization'), bar.props('resetsAt')])).toEqual([
      ['admin.accounts.cnProviders.windowMonthlyTotal', 90, '2026-12-01T00:00:00Z'],
      ['admin.accounts.cnProviders.windowMonthlyCode', 10, '2026-12-02T00:00:00Z']
    ])
    queryQuota.mockResolvedValue({ success: false, error: 'Invalid quota response' })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Invalid quota response')
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => bar.props('utilization'))).toEqual([90, 10])
    queryQuota.mockResolvedValue({ success: true, tiers: [
      { window: 'monthly', used_percent: 80, reset_at: '2026-12-03T00:00:00Z' },
      { window: 'monthly_code', used_percent: 20, reset_at: '2026-12-04T00:00:00Z' }
    ] })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => [bar.props('label'), bar.props('utilization'), bar.props('resetsAt')])).toEqual([
      ['admin.accounts.cnProviders.windowMonthlyTotal', 80, '2026-12-03T00:00:00Z'],
      ['admin.accounts.cnProviders.windowMonthlyCode', 20, '2026-12-04T00:00:00Z']
    ])
  })

  it('renders and refreshes native MiniMax OAuth five-hour and weekly quota with their reset times', async () => {
    const minimax = {
      ...account,
      id: 2707,
      platform: 'minimax',
      type: 'oauth',
      credentials: { account_mode: 'coding', oauth_provider: 'minimax', oauth_region: 'cn' },
      extra: {
        minimax_5h_used_percent: 100,
        minimax_5h_reset_at: '2026-12-01T05:00:00Z',
        minimax_weekly_used_percent: 45,
        minimax_weekly_reset_at: '2026-12-07T00:00:00Z',
        minimax_usage_updated_at: new Date().toISOString()
      }
    } as Account
    const wrapper = mount(CNProviderQuotaCell, { props: { account: minimax } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => [bar.props('label'), bar.props('utilization'), bar.props('resetsAt')])).toEqual([
      ['admin.accounts.cnProviders.window5h', 100, '2026-12-01T05:00:00Z'],
      ['admin.accounts.cnProviders.windowWeekly', 45, '2026-12-07T00:00:00Z']
    ])

    queryQuota.mockResolvedValue({ success: true, tiers: [
      { window: '5h', used_percent: 0, reset_at: '2026-12-01T10:00:00Z' },
      { window: 'weekly', used_percent: 50, reset_at: '2026-12-08T00:00:00Z' }
    ] })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(minimax.id)
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => [bar.props('label'), bar.props('utilization'), bar.props('resetsAt')])).toEqual([
      ['admin.accounts.cnProviders.window5h', 0, '2026-12-01T10:00:00Z'],
      ['admin.accounts.cnProviders.windowWeekly', 50, '2026-12-08T00:00:00Z']
    ])
  })

  it.each(['unknown response', 'request failure'])('preserves native MiniMax OAuth quota snapshots after %s', async failure => {
    const minimax = {
      ...account,
      id: failure === 'unknown response' ? 2807 : 2808,
      platform: 'minimax',
      type: 'oauth',
      credentials: { account_mode: 'coding', oauth_provider: 'minimax', oauth_region: 'global' },
      extra: {
        minimax_5h_used_percent: 25,
        minimax_5h_reset_at: '2026-12-01T05:00:00Z',
        minimax_weekly_used_percent: 60,
        minimax_weekly_reset_at: '2026-12-07T00:00:00Z',
        minimax_usage_updated_at: new Date().toISOString()
      }
    } as Account
    const wrapper = mount(CNProviderQuotaCell, { props: { account: minimax } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    if (failure === 'unknown response') {
      queryQuota.mockResolvedValue({ success: false, error: 'MiniMax quota unknown' })
    } else {
      queryQuota.mockRejectedValue(new Error('MiniMax quota unknown'))
    }
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(minimax.id)
    expect(wrapper.text()).toContain('MiniMax quota unknown')
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => [bar.props('utilization'), bar.props('resetsAt')])).toEqual([
      [25, '2026-12-01T05:00:00Z'],
      [60, '2026-12-07T00:00:00Z']
    ])
  })

  it('shows unlimited OAuth windows without fabricating percentage bars, including persisted snapshots', async () => {
    // MiniMax Code matrix-account-client.ts: quota status=3 is unlimited.
    const minimax = {
      ...account, id: 3707, platform: 'minimax', type: 'oauth',
      credentials: { account_mode: 'coding', oauth_provider: 'minimax' },
      extra: { minimax_5h_unlimited: true, minimax_usage_updated_at: new Date().toISOString() }
    } as Account
    const wrapper = mount(CNProviderQuotaCell, { props: { account: minimax } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
    expect(wrapper.get('[data-test="cn-provider-quota-unlimited"]').text()).toContain('admin.accounts.cnProviders.unlimited')
    queryQuota.mockResolvedValue({ success: true, tiers: [
      { window: '5h', used_percent: 30 },
      { window: 'weekly', unlimited: true, used_percent: 0 }
    ] })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar).map(bar => bar.props('utilization'))).toEqual([30])
    expect(wrapper.get('[data-test="cn-provider-quota-unlimited"]').text()).toContain('admin.accounts.cnProviders.windowWeekly')
    expect(wrapper.text()).not.toMatch(/(?<![0-9])0%/)
  })

})
