import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminPaymentPlansView from '../AdminPaymentPlansView.vue'
import zh from '@/i18n/locales/zh'

const { getPlans, getConfig, getGroups } = vi.hoisted(() => ({
  getPlans: vi.fn(),
  getConfig: vi.fn(),
  getGroups: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getPlans,
    getConfig,
  },
}))

vi.mock('@/api/admin', () => ({
  default: {
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        // Vitest uses runtime-only i18n. Resolve the actual Chinese catalog;
        // missing keys stay visible so this regression cannot mask the defect.
        const message = key.split('.').reduce<unknown>((node, part) =>
          node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined,
        zh)
        return typeof message === 'string' ? message : key
      },
    }),
  }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-price" :value="row.price" :row="row" />
        <div data-testid="plan-validity"><slot name="cell-validity_days" :value="row.validity_days" :row="row" /></div>
      </div>
    </div>
  `,
}

function mountPlans() {
  return mount(AdminPaymentPlansView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        DataTable: DataTableStub,
        ConfirmDialog: true,
        GroupBadge: true,
        Icon: true,
        PlanEditDialog: true,
      },
    },
  })
}

describe('AdminPaymentPlansView', () => {
  beforeEach(() => {
    getGroups.mockResolvedValue([])
    getConfig.mockResolvedValue({ data: {} })
    getPlans.mockResolvedValue({
      data: [
        {
          id: 1,
          name: 'CNY plan',
          group_id: 1,
          price: 499,
          original_price: 599,
          currency: 'CNY',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          features: [],
        },
        {
          id: 2,
          name: 'Legacy plan',
          group_id: 1,
          price: 10,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          features: [],
        },
      ],
    })
  })

  it('uses the configured currency symbol and keeps legacy prices in USD', async () => {
    const wrapper = mountPlans()

    await flushPromises()

    expect(wrapper.text()).toContain('¥499.00CNY')
    expect(wrapper.text()).toContain('¥599.00')
    expect(wrapper.text()).toContain('$10.00')
    wrapper.unmount()
  })

  // Audit requirement: show the purchased duration in Chinese, including the
  // persisted default `day`; raw translation keys must never reach customers.
  it.each([
    ['day', 30, '30天'],
    ['days', 30, '30天'],
    ['week', 2, '2周'],
    ['weeks', 2, '2周'],
    ['month', 3, '3个月'],
    ['months', 3, '3个月'],
    ['', 30, '30天'],
  ])('shows a readable duration for %s', async (unit, amount, expected) => {
    getPlans.mockResolvedValue({ data: [{
      id: 1, name: 'Duration audit plan', group_id: 1, price: 39.9,
      validity_days: amount, validity_unit: unit, for_sale: true, features: [],
    }] })
    const wrapper = mountPlans()
    await flushPromises()

    expect(wrapper.get('[data-testid="plan-validity"]').text().replace(/\s+/g, '')).toBe(expected)
    wrapper.unmount()
  })
})
