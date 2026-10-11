import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { PaymentOrder } from '@/types/payment'
import UserOrdersView from '../UserOrdersView.vue'
import AdminOrderTable from '@/components/admin/payment/AdminOrderTable.vue'
import AdminOrderDetail from '@/components/admin/payment/AdminOrderDetail.vue'

const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn() }))
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => vi.clearAllMocks())

// User requirements 4/7/8: a 100 CNY redeem order credits 100 USD, displays
// its independent balance/subscription type, and has no refund entry. The
// fixture also gives it an eligible provider so source alone must forbid it.
function order(source: 'payment' | 'redeem', id: number): PaymentOrder {
  return {
    id, user_id: 10, amount: 100, pay_amount: 100, currency: 'CNY',
    fee_rate: 0, bonus_amount: 0, payment_type: source === 'redeem' ? 'redeem' : 'stripe',
    order_source: source, out_trade_no: `order-${id}`, status: 'COMPLETED',
    order_type: 'balance', provider_instance_id: 'refundable-provider',
    created_at: '2026-10-11T12:00:00Z', expires_at: '2026-10-11T12:00:00Z', refund_amount: 0
  }
}

describe('redeem order business presentation', () => {
  it('shows CNY paid and USD credited, and permits only payment refunds for users', async () => {
    api.getMyOrders.mockResolvedValue({ data: { items: [order('redeem', 1), order('payment', 2)], total: 2 } })
    api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['refundable-provider'] } })
    const wrapper = mount(UserOrdersView, { global: {
      directives: { 'support-readonly': {} },
      stubs: { AppLayout: { template: '<main><slot /></main>' }, BaseDialog: true, Icon: true, Pagination: true }
    } })
    await flushPromises()
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('featureExtensions.redeemOrder')
    expect(rows[0].text()).toContain('payment.tabTopUp')
    expect(rows[0].text()).toContain('¥100.00')
    expect(rows[0].text()).toContain('payment.orders.creditedAmount: $100.00')
    expect(rows[0].findAll('button').some(b => b.text().includes('payment.orders.requestRefund'))).toBe(false)
    expect(rows[1].findAll('button').some(b => b.text().includes('payment.orders.requestRefund'))).toBe(true)
  })

  it('hides admin refund actions for redeem orders while keeping payment actions', () => {
    const wrapper = mount(AdminOrderTable, { props: {
      orders: [order('redeem', 1), order('payment', 2)], loading: false, page: 1, pageSize: 20, total: 2
    }, global: { stubs: { Icon: true, Pagination: true } } })
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('featureExtensions.redeemOrder')
    expect(rows[0].text()).toContain('¥100.00')
    expect(rows[0].text()).toContain('$100.00')
    expect(rows[0].findAll('button').some(b => b.text().includes('payment.admin.refund'))).toBe(false)
    expect(rows[1].findAll('button').some(b => b.text().includes('payment.admin.refund'))).toBe(true)
  })

  it('keeps frozen subscription names visible and omits the detail refund button', async () => {
    const wrapper = mount(AdminOrderDetail, { props: {
      show: true, order: { ...order('redeem', 1), order_type: 'subscription', plan_name: 'Frozen monthly plan' }
    }, global: { stubs: { BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /></section>' } } } })
    expect(wrapper.text()).toContain('Frozen monthly plan')
    expect(wrapper.text()).toContain('payment.admin.subscriptionOrder')
    expect(wrapper.findAll('button').some(b => b.text().includes('payment.admin.refund'))).toBe(false)
    await wrapper.setProps({ order: order('payment', 2) })
    expect(wrapper.findAll('button').some(b => b.text().includes('payment.admin.refund'))).toBe(true)
  })
})
