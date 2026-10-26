import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AdminOrdersView from '../AdminOrdersView.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import OrderTable from '@/components/payment/OrderTable.vue'
import AdminRefundDialog from '@/components/admin/payment/AdminRefundDialog.vue'
import { chooseSelect } from '@/__tests__/helpers/select'
import type { PaymentOrder } from '@/types/payment'

const { api, showError, showSuccess } = vi.hoisted(() => ({
  api: {
    getOrders: vi.fn(), getOrder: vi.fn(), cancelOrder: vi.fn(), retryRecharge: vi.fn(),
    refundOrder: vi.fn(), queryRefund: vi.fn(),
  },
  showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: api, default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })

const AppLayoutStub = defineComponent({ props: { workspace: Boolean }, template: '<main><slot /></main>' })
const DataTableStub = defineComponent({
  props: ['data'],
  template: '<div><div v-for="row in data" :key="row.id" :data-order-id="row.id"><slot name="cell-actions" :row="row" /></div></div>',
})
const BaseDialogStub = defineComponent({
  props: ['show'],
  template: '<section v-if="show" role="dialog"><slot /><slot name="footer" /></section>',
})

function order(id: number, status: PaymentOrder['status']): PaymentOrder {
  return {
    id, user_id: 10, amount: 100, pay_amount: 100, currency: 'USD', fee_rate: 0,
    payment_type: 'stripe', out_trade_no: `order-${id}`, status, order_source: 'payment',
    order_type: 'balance', created_at: '2026-10-24T12:00:00Z', expires_at: '2026-10-24T13:00:00Z', refund_amount: 0,
  }
}

function mountOrders() {
  return mount(AdminOrdersView, { global: { stubs: {
    AppLayout: AppLayoutStub, DataTable: DataTableStub, BaseDialog: BaseDialogStub,
    AdminRefundDialog: true, Pagination: true, Icon: true, OrderStatusBadge: true, teleport: true,
  } } })
}

beforeEach(() => {
  vi.resetAllMocks()
  api.getOrders.mockResolvedValue({ data: { items: [], total: 100 } })
  api.cancelOrder.mockResolvedValue({})
  api.retryRecharge.mockResolvedValue({})
  api.refundOrder.mockResolvedValue({ data: { success: true } })
  api.queryRefund.mockResolvedValue({ data: { success: true } })
})

describe('admin orders workspace', () => {
  it('keeps queries and pagination outside the rounded order-data viewport', async () => {
    const wrapper = mountOrders()
    await flushPromises()
    expect(wrapper.getComponent(AppLayoutStub).props('workspace')).toBe(true)
    const table = wrapper.get('[data-test="admin-orders-table"]')
    const filters = wrapper.get('[data-test="admin-orders-filters"]')
    const pagination = wrapper.get('[data-test="admin-orders-pagination"]')
    expect(filters.element.parentElement).toBe(table.element.parentElement)
    expect(pagination.element.parentElement).toBe(table.element.parentElement)
    expect(filters.classes()).toContain('workspace-fixed')
    expect(pagination.classes()).toContain('workspace-fixed')
    expect(table.classes()).toContain('workspace-table-card')
    expect(wrapper.getComponent(OrderTable).props('showUser')).toBe(true)
    expect(wrapper.getComponent(DataTableStub).classes()).toContain('workspace-table-viewport')
    expect(table.find('[data-test="admin-orders-filters"]').exists()).toBe(false)
    expect(table.find('[data-test="admin-orders-pagination"]').exists()).toBe(false)
  })

  it('preserves combined filters, debounced and cleared keywords, refresh, and page-size changes', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = mountOrders()
    await flushPromises()
    wrapper.getComponent(Pagination).vm.$emit('update:page', 3)
    await flushPromises()
    const selects = wrapper.findAllComponents(Select)
    await chooseSelect(selects[1], 'payment.status.failed')
    await chooseSelect(selects[2], 'payment.methods.stripe')
    await chooseSelect(selects[3], 'payment.admin.subscriptionOrder')
    await chooseSelect(selects[0], 'featureExtensions.redeemOrder')
    const filters = { status: 'FAILED', payment_type: 'stripe', order_type: 'subscription', order_source: 'redeem' }
    expect(api.getOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, keyword: undefined, ...filters })
    api.getOrders.mockClear()
    await wrapper.get('[placeholder="payment.admin.searchOrders"]').setValue('order-42')
    await vi.advanceTimersByTimeAsync(299)
    expect(api.getOrders).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(api.getOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, keyword: 'order-42', ...filters })

    await wrapper.get('[placeholder="payment.admin.searchOrders"]').setValue('')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(api.getOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 50, keyword: undefined, ...filters })
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getOrders).toHaveBeenLastCalledWith({ page: 2, page_size: 50, keyword: undefined, ...filters })
  })

  it('preserves cancellation, retries, details and refund dialogs outside the data scroller', async () => {
    api.getOrders.mockResolvedValue({ data: { items: [order(1, 'PENDING'), order(2, 'FAILED'), order(3, 'COMPLETED')], total: 3 } })
    api.getOrder.mockResolvedValue({ data: { order: order(3, 'COMPLETED'), audit_logs: [] } })
    const wrapper = mountOrders()
    await flushPromises()
    await wrapper.get('[data-order-id="1"]').findAll('button').find(button => button.text() === 'payment.orders.cancel')!.trigger('click')
    await flushPromises()
    expect(api.cancelOrder).toHaveBeenCalledWith(1)
    await wrapper.get('[data-order-id="2"]').findAll('button').find(button => button.text() === 'payment.admin.retry')!.trigger('click')
    await flushPromises()
    expect(api.retryRecharge).toHaveBeenCalledWith(2)
    await wrapper.get('[data-order-id="3"]').findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    expect(api.getOrder).toHaveBeenCalledWith(3)
    expect(wrapper.get('[role="dialog"]').element.closest('[data-test="admin-orders-table"]')).toBeNull()

    await wrapper.get('[data-order-id="3"]').findAll('button').find(button => button.text() === 'payment.admin.refund')!.trigger('click')
    const refund = wrapper.getComponent(AdminRefundDialog)
    expect(refund.props('show')).toBe(true)
    expect(refund.element.closest('[data-test="admin-orders-table"]')).toBeNull()
    refund.vm.$emit('confirm', { amount: 15, reason: 'Duplicate payment', deduct_balance: true, force: false })
    await flushPromises()
    expect(api.refundOrder).toHaveBeenCalledWith(3, { amount: 15, reason: 'Duplicate payment', deduct_balance: true, force: false })
    expect(api.getOrders).toHaveBeenCalledTimes(4)
    expect(showError).not.toHaveBeenCalled()
  })
})
