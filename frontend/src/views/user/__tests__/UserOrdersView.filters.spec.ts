import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import UserOrdersView from '../UserOrdersView.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import OrderTable from '@/components/payment/OrderTable.vue'
import { chooseSelect } from '@/__tests__/helpers/select'

const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn(), cancelOrder: vi.fn(), requestRefund: vi.fn() }))
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
const AppLayoutStub = defineComponent({ props: { workspace: Boolean }, template: '<main><slot /></main>' })

beforeEach(() => {
  vi.clearAllMocks()
  api.getMyOrders.mockResolvedValue({ data: { items: [], total: 100 } })
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
  api.cancelOrder.mockResolvedValue({})
  api.requestRefund.mockResolvedValue({})
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, {
    global: { stubs: {
      AppLayout: AppLayoutStub,
      OrderTable: true, BaseDialog: true, Icon: true, Pagination: true, teleport: true
    } }
  })
  await flushPromises()
  wrapper.getComponent(Pagination).vm.$emit('update:page', 4)
  await flushPromises()
  return wrapper
}

describe('order status filtering', () => {
  it('keeps the filters and pagination outside the orders viewport in the fixed workspace', async () => {
    const wrapper = await openOrders()
    expect(wrapper.getComponent(AppLayoutStub).props('workspace')).toBe(true)
    const table = wrapper.get('[data-test="user-orders-table"]')
    const filters = wrapper.get('[data-test="user-orders-filters"]')
    const pagination = wrapper.get('[data-test="user-orders-pagination"]')
    expect(filters.element.parentElement).toBe(table.element.parentElement)
    expect(pagination.element.parentElement).toBe(table.element.parentElement)
    expect(filters.classes()).toContain('workspace-fixed')
    expect(pagination.classes()).toContain('workspace-fixed')
    expect(table.classes()).toContain('workspace-table-card')
    expect(table.getComponent(OrderTable).classes()).toContain('workspace-table-viewport')
    expect(table.find('[data-test="user-orders-filters"]').exists()).toBe(false)
    expect(table.find('[data-test="user-orders-pagination"]').exists()).toBe(false)
  })

  it('loads the first page with the selected status', async () => {
    const wrapper = await openOrders()
    const select = wrapper.findAllComponents(Select).find(c => c.props('options').some(o => o.value === 'PENDING'))!
    await chooseSelect(select, 'payment.status.pending')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: 'PENDING', order_source: undefined, order_type: undefined })
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })

  it('combines source and type filters and resets pagination', async () => {
    const wrapper = await openOrders()
    const selects = wrapper.findAllComponents(Select)
    const source = selects.find(c => c.props('options').some(o => o.value === 'redeem'))!
    const type = selects.find(c => c.props('options').some(o => o.value === 'subscription'))!
    await chooseSelect(source, source.props('options').find(o => o.value === 'redeem')!.label)
    await chooseSelect(type, type.props('options').find(o => o.value === 'subscription')!.label)
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: undefined, order_source: 'redeem', order_type: 'subscription' })
  })

  it('keeps the current page on manual refresh', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 4, page_size: 20, status: undefined, order_source: undefined, order_type: undefined })
  })

  it('clears a selected filter and resets the page, then preserves the page-size change', async () => {
    const wrapper = await openOrders()
    const status = wrapper.findAllComponents(Select).find(c => c.props('options').some(o => o.value === 'PENDING'))!
    await chooseSelect(status, 'payment.status.pending')
    await chooseSelect(status, 'common.all')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: undefined, order_source: undefined, order_type: undefined })

    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 50, status: undefined, order_source: undefined, order_type: undefined })
  })

  it('keeps cancellation and refund confirmation dialogs outside the scrolling orders card', async () => {
    api.getMyOrders.mockResolvedValue({ data: { items: [
      { id: 1, status: 'PENDING', order_source: 'payment', amount: 10 },
      { id: 2, status: 'COMPLETED', order_source: 'payment', amount: 15, provider_instance_id: 'refundable' },
    ], total: 2 } })
    api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['refundable'] } })
    const wrapper = mount(UserOrdersView, {
      global: { stubs: {
        AppLayout: AppLayoutStub,
        OrderTable: defineComponent({
          props: ['orders'],
          template: '<div><div v-for="row in orders" :key="row.id"><slot name="actions" :row="row" /></div></div>',
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<section v-if="show" role="dialog"><slot /><slot name="footer" /></section>',
        }),
        Icon: true, Select: true, Pagination: true,
      } },
    })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'payment.orders.cancel')!.trigger('click')
    expect(api.cancelOrder).not.toHaveBeenCalled()
    const cancelDialog = wrapper.get('[role="dialog"]')
    expect(cancelDialog.element.closest('[data-test="user-orders-table"]')).toBeNull()
    await cancelDialog.findAll('button').find(button => button.text() === 'payment.orders.cancel')!.trigger('click')
    await flushPromises()
    expect(api.cancelOrder).toHaveBeenCalledWith(1)

    await wrapper.findAll('button').find(button => button.text() === 'payment.orders.requestRefund')!.trigger('click')
    expect(api.requestRefund).not.toHaveBeenCalled()
    const refundDialog = wrapper.get('[role="dialog"]')
    expect(refundDialog.element.closest('[data-test="user-orders-table"]')).toBeNull()
    await refundDialog.get('textarea').setValue(' Duplicate payment ')
    await refundDialog.findAll('button').find(button => button.text() === 'payment.orders.requestRefund')!.trigger('click')
    await flushPromises()
    expect(api.requestRefund).toHaveBeenCalledWith(2, { reason: 'Duplicate payment' })
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })
})
