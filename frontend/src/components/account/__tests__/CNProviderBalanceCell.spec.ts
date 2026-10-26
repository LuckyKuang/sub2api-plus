import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderBalanceCell from '../CNProviderBalanceCell.vue'
import type { Account } from '@/types'

const { queryBalance } = vi.hoisted(() => ({
  queryBalance: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryBalance }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const account = {
  id: 7,
  platform: 'kimi',
  type: 'apikey',
  credentials: { account_mode: 'payg' },
  extra: {
    kimi_balance: 12.5,
    kimi_balance_currency: 'CNY'
  }
} as Account

describe('CNProviderBalanceCell', () => {
  beforeEach(() => {
    queryBalance.mockReset()
  })

  it('renders the persisted balance as static text with an explicit query action', async () => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })
    await flushPromises()

    // Snapshot value renders without any probe.
    expect(queryBalance).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 12.50')

    // The control reads as an action; the i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-balance-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
  })

  it('shows the low-balance badge from the snapshot marker', () => {
    const lowAccount = {
      ...account,
      extra: { kimi_balance: 0.4, kimi_balance_low: true }
    } as Account

    const wrapper = mount(CNProviderBalanceCell, { props: { account: lowAccount } })

    expect(wrapper.text()).toContain('admin.accounts.cnProviders.balanceLow')
  })

  it('keeps the snapshot balance visible when a query fails', async () => {
    queryBalance.mockResolvedValue({ success: false, error: 'HTTP 401' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })

    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('CNY 12.50')
    expect(wrapper.text()).toContain('HTTP 401')
  })

  const nativeWalletAccount = {
    id: 42,
    platform: 'deepseek',
    type: 'oauth',
    credentials: { account_mode: 'payg', oauth_provider: 'deepseek', oauth_region: 'cn' },
    extra: {
      deepseek_balances: [{ currency: 'CNY', balance: 3 }, { currency: 'USD', balance: 4 }],
      deepseek_bonus_balances: [{ currency: 'CNY', balance: 2 }]
    }
  } as Account

  it('shows persisted native recharge and bonus wallets separately without merging currencies or wallet kinds', () => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account: nativeWalletAccount } })

    const normal = wrapper.get('[data-test="cn-provider-balance-value"]').text()
    const bonus = wrapper.get('[data-test="cn-provider-bonus-balance-value"]').text()
    expect(normal).toContain('admin.accounts.cnProviders.normalBalance')
    expect(normal).toContain('CNY 3.00 · USD 4.00')
    expect(normal).not.toContain('5.00')
    expect(bonus).toContain('admin.accounts.cnProviders.bonusBalance')
    expect(bonus).toContain('CNY 2.00')
    expect(bonus).not.toContain('USD')
    expect(queryBalance).not.toHaveBeenCalled()
  })

  it('refreshes both native wallet kinds independently from the successful probe', async () => {
    queryBalance.mockResolvedValue({
      success: true, balance: 10, currency: 'USD',
      balances: [{ currency: 'USD', balance: 10 }],
      bonus_balances: [{ currency: 'CNY', balance: 6 }, { currency: 'USD', balance: 1 }]
    })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: nativeWalletAccount } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('USD 10.00')
    expect(wrapper.get('[data-test="cn-provider-bonus-balance-value"]').text()).toContain('CNY 6.00 · USD 1.00')
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).not.toContain('11.00')
  })

  it('keeps both valid wallet snapshots when the native probe is unknown', async () => {
    queryBalance.mockResolvedValue({ success: false, error: 'Account wallet unavailable' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: nativeWalletAccount } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 3.00 · USD 4.00')
    expect(wrapper.get('[data-test="cn-provider-bonus-balance-value"]').text()).toContain('CNY 2.00')
    expect(wrapper.text()).toContain('Account wallet unavailable')
  })

  it('clears a previous bonus observation after a successful explicitly empty bonus response', async () => {
    queryBalance.mockResolvedValue({ success: true, balance: 9, currency: 'CNY', balances: [{ currency: 'CNY', balance: 9 }], bonus_balances: [] })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: nativeWalletAccount } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 9.00')
    expect(wrapper.find('[data-test="cn-provider-bonus-balance-value"]').exists()).toBe(false)
  })
})
