import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper } from '@vue/test-utils'

import RedeemView from '../RedeemView.vue'

const { listRedeemCodes, batchUpdateRedeemCodes, generateCodes, getPlans, getAllGroups, showSuccess, showError, showInfo } =
  vi.hoisted(() => ({
    listRedeemCodes: vi.fn(),
    batchUpdateRedeemCodes: vi.fn(),
    generateCodes: vi.fn(),
    getPlans: vi.fn(),
    getAllGroups: vi.fn(),
    showSuccess: vi.fn(),
    showError: vi.fn(),
    showInfo: vi.fn()
  }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    redeem: {
      list: listRedeemCodes,
      generate: generateCodes,
      delete: vi.fn(),
      batchDelete: vi.fn(),
      batchUpdate: batchUpdateRedeemCodes,
      exportCodes: vi.fn()
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getPlans } }))

enableAutoUnmount(afterEach)

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError,
    showInfo
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <table>
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key">
            <slot :name="'header-' + column.key" :column="column">{{ column.label }}</slot>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data" :key="row.id">
          <td v-for="column in columns" :key="column.key">
            <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]">
              {{ row[column.key] }}
            </slot>
          </td>
        </tr>
      </tbody>
    </table>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  setup(props: { options: Array<{ value: unknown; label: string }> }, { emit }: { emit: (event: string, ...args: unknown[]) => void }) {
    const onChange = (event: Event) => {
      const raw = (event.target as HTMLSelectElement).value
      const option = props.options.find((item) => String(item.value ?? '') === raw)
      const value = option ? option.value : raw
      emit('update:modelValue', value)
      emit('change', value, option ?? null)
    }
    return { onChange }
  },
  template: `
    <select v-bind="$attrs" :value="modelValue ?? ''" @change="onChange">
      <option v-for="option in options" :key="String(option.value ?? '')" :value="option.value ?? ''">
        {{ option.label }}
      </option>
    </select>
  `
}

function renderView() {
  return mount(RedeemView, { attachTo: document.body, global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
    DataTable: DataTableStub, Pagination: true, ConfirmDialog: true,
    GroupBadge: true, GroupOptionItem: true, Icon: true, Teleport: true,
  } } })
}

async function choose(select: DOMWrapper<Element>, label: string) {
  const option = select.findAll('option').find(o => o.text().includes(label))!
  await select.setValue((option.element as HTMLOptionElement).value)
  await flushPromises()
}

describe('admin RedeemView', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''

    listRedeemCodes.mockReset()
    batchUpdateRedeemCodes.mockReset()
    getAllGroups.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    showInfo.mockReset()
    generateCodes.mockReset()
    getPlans.mockReset()
    generateCodes.mockResolvedValue([])
    getPlans.mockResolvedValue({ data: [{ id: 7, name: 'Frozen plan', price: 25, currency: 'USD', for_sale: false }] })

    listRedeemCodes.mockResolvedValue({
      items: [
        {
          id: 1,
          code: 'CODE-1',
          type: 'balance',
          value: 10,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        },
        {
          id: 2,
          code: 'CODE-2',
          type: 'balance',
          value: 20,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        }
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    batchUpdateRedeemCodes.mockResolvedValue({ updated: 1, message: 'ok' })
    getAllGroups.mockResolvedValue([])
  })

  it('defaults to no order and CNY, with an explicit balance currency choice', async () => {
    const wrapper = renderView()
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'admin.redeem.generateCodes')!.trigger('click')
    await flushPromises()
    const form = () => wrapper.get('form')
    const orderSelect = () => form().findAll('select').find(s => s.text().includes('featureExtensions.noOrder'))!
    expect(orderSelect().get('option:checked').text()).toBe('featureExtensions.noOrder')
    await choose(orderSelect(), 'featureExtensions.withOrder')
    const currency = form().findAll('select').find(s => s.text().includes('CNY'))!
    expect(currency.get('option:checked').text()).toBe('CNY')
    await choose(currency, 'USD')
    await form().trigger('submit')
    await flushPromises()
    expect(generateCodes).toHaveBeenCalledWith(1, 'balance', 10, undefined, undefined, undefined,
      { generate_order: true, order_currency: 'USD', order_plan_id: null })
  })

  it('generates a subscription from the selected plan without stale entitlement fields', async () => {
    const wrapper = renderView()
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'admin.redeem.generateCodes')!.trigger('click')
    await flushPromises()
    const form = () => wrapper.get('form')
    await choose(form().findAll('select')[0], 'admin.redeem.subscription')
    await choose(form().findAll('select').find(s => s.text().includes('featureExtensions.noOrder'))!, 'featureExtensions.withOrder')
    await choose(form().findAll('select').find(s => s.text().includes('Frozen plan'))!, 'Frozen plan')
    expect(form().text()).toContain('featureExtensions.planSnapshot')
    expect(form().text()).not.toContain('admin.redeem.validityDays')
    await form().trigger('submit')
    await flushPromises()
    expect(generateCodes).toHaveBeenCalledWith(1, 'subscription', 10, undefined, undefined, undefined,
      { generate_order: true, order_currency: 'CNY', order_plan_id: 7 })
  })

  it('clears the order option when changing to an unsupported code type', async () => {
    const wrapper = renderView()
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'admin.redeem.generateCodes')!.trigger('click')
    await flushPromises()
    const form = () => wrapper.get('form')
    await choose(form().findAll('select').find(s => s.text().includes('featureExtensions.noOrder'))!, 'featureExtensions.withOrder')
    await choose(form().findAll('select')[0], 'admin.redeem.concurrency')
    expect(form().text()).not.toContain('featureExtensions.generateOrder')
    await form().trigger('submit')
    await flushPromises()
    expect(generateCodes).toHaveBeenCalledWith(1, 'concurrency', 10, undefined, undefined, undefined,
      { generate_order: false, order_currency: 'CNY', order_plan_id: null })
  })

  it('submits only checked fields for selected redeem codes', async () => {
    const wrapper = mount(RedeemView, {
      attachTo: document.body,
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          Select: SelectStub,
          GroupBadge: true,
          GroupOptionItem: true,
          Icon: true,
          Teleport: true
        }
      }
    })

    await flushPromises()
    await wrapper.findAll('[data-test="select-code"]')[0].setValue(true)
    await wrapper.get('[data-test="batch-update-open"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="batch-field-status"]').setValue(true)
    await wrapper.get('[data-test="batch-status-select"]').setValue('disabled')
    await wrapper.get('[data-test="batch-field-notes"]').setValue(true)
    await wrapper.get('[data-test="batch-notes-input"]').setValue('maintenance')
    await wrapper.get('[data-test="batch-update-form"]').trigger('submit')
    await flushPromises()

    expect(batchUpdateRedeemCodes).toHaveBeenCalledWith([1], {
      status: 'disabled',
      notes: 'maintenance'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.redeem.batchUpdateSuccess')
  })
})
