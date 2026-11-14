import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'

const { list, extend, resetQuota, showError } = vi.hoisted(() => ({
  list: vi.fn(),
  extend: vi.fn(),
  resetQuota: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: { list, extend, resetQuota },
    groups: { getAll: vi.fn().mockResolvedValue([]) },
    users: { list: vi.fn().mockResolvedValue({ items: [] }) }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const futureExpiry = new Date(Date.now() + 10 * 24 * 60 * 60 * 1000).toISOString()
const rows = [{
  id: 1,
  user_id: 11,
  group_id: 1,
  status: 'active',
  expires_at: futureExpiry,
  user: { email: 'active@example.com' }
}]

function pad(value: number) {
  return String(value).padStart(2, '0')
}

function toLocal(date: Date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

function mountView() {
  return mount(SubscriptionsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: {
          name: 'DataTable',
          props: ['data'],
          template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
        },
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        DateTimePicker: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', ($event.target).value)" />'
        },
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', ($event.target).value)"><option v-for="option in options" :key="String(option.value)" :value="option.value">{{ option.label }}</option></select>'
        },
        Pagination: true, ConfirmDialog: true, Icon: true,
        GroupBadge: true, GroupOptionItem: true, EmptyState: true, Teleport: true, RouterLink: true
      }
    }
  })
}

let wrapper: ReturnType<typeof mountView>

beforeEach(async () => {
  vi.clearAllMocks()
  list.mockResolvedValue({ items: rows, total: 1, pages: 1 })
  extend.mockResolvedValue({})
  resetQuota.mockResolvedValue({})
  wrapper = mountView()
  await flushPromises()
})

afterEach(() => {
  wrapper.unmount()
})

describe('subscription adjust and reset dialogs', () => {
  it('resets only the selected quota windows', async () => {
    await wrapper.get('[data-test="reset-quota"]').trigger('click')
    const form = wrapper.get('[data-test="reset-quota-form"]')
    for (const name of ['fiveHour', 'daily', 'weekly', 'monthly']) {
      await form.get(`input[name="${name}"]`).setValue(false)
    }
    await form.trigger('submit')
    expect(resetQuota).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.subscriptions.selectResetWindow')

    await form.get('input[name="weekly"]').setValue(true)
    await form.trigger('submit')
    await flushPromises()
    expect(resetQuota).toHaveBeenCalledWith(1, {
      five_hour: false,
      daily: false,
      weekly: true,
      monthly: false
    })
  })

  it('keeps relative-day adjustments from sending unused window reset times', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { days: 30 })
  })

  it('sends an exact expiration timestamp in the browser timezone', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('[data-test="extend-expiry-mode"]').setValue('absolute')
    const expiresAt = new Date(Date.now() + 48 * 60 * 60 * 1000)
    expiresAt.setMilliseconds(0)
    await wrapper.get('[data-test="extend-expires-at"]').setValue(toLocal(expiresAt))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { expires_at: expiresAt.toISOString() })
  })

  it('sends a weekly next-reset time without changing expiration', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('[data-test="extend-days"]').setValue('')
    const weeklyReset = new Date(Date.now() + 3 * 60 * 60 * 1000)
    weeklyReset.setMilliseconds(0)
    await wrapper.get('[data-test="extend-weekly-reset"]').setValue(toLocal(weeklyReset))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { weekly_reset_at: weeklyReset.toISOString() })
  })
})
