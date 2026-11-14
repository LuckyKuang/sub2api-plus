import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'

const { list, getById, getProgress, extend, resetQuota, showError } = vi.hoisted(() => ({
  list: vi.fn(),
  getById: vi.fn(),
  getProgress: vi.fn(),
  extend: vi.fn(),
  resetQuota: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: { list, getById, getProgress, extend, resetQuota },
    groups: { getAll: vi.fn().mockResolvedValue([]) },
    users: { list: vi.fn().mockResolvedValue({ items: [] }) }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

// The plan configures 5-hour, daily, weekly, and monthly usage in different
// combinations; only the configured windows may appear in the reset dialog.
const planGroup = {
  daily_limit_usd: 15,
  weekly_limit_usd: 75,
  monthly_limit_usd: 250,
  five_hour_limit_usd: null
}
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
  getById.mockResolvedValue({ ...rows[0], group: planGroup })
  getProgress.mockResolvedValue(null)
  extend.mockResolvedValue({})
  resetQuota.mockResolvedValue({})
  wrapper = mountView()
  await flushPromises()
})

afterEach(() => {
  wrapper.unmount()
})

describe('subscription adjust and reset dialogs', () => {
  it('offers only the quota windows the plan configures', async () => {
    await wrapper.get('[data-test="reset-quota"]').trigger('click')
    await flushPromises()
    const form = wrapper.get('[data-test="reset-quota-form"]')
    expect(form.find('input[name="fiveHour"]').exists()).toBe(false)
    for (const name of ['daily', 'weekly', 'monthly']) {
      expect(form.find(`input[name="${name}"]`).exists()).toBe(true)
    }
  })

  it('resets only the selected configured quota windows', async () => {
    await wrapper.get('[data-test="reset-quota"]').trigger('click')
    await flushPromises()
    const form = wrapper.get('[data-test="reset-quota-form"]')
    for (const name of ['daily', 'weekly', 'monthly']) {
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

  it('reports a plan without any configurable reset time', async () => {
    getById.mockResolvedValue({ ...rows[0], group: { daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, five_hour_limit_usd: null } })
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    expect(wrapper.text()).toContain('admin.subscriptions.extendNoQuotaWindows')
  })

  it('reports a plan without any configured usage limit', async () => {
    getById.mockResolvedValue({ ...rows[0], group: { daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, five_hour_limit_usd: null } })
    await wrapper.get('[data-test="reset-quota"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.subscriptions.noQuotaWindows')
    expect(wrapper.get('button[form="reset-quota-form"]').attributes('disabled')).toBeDefined()
  })

  it('offers only the reset times the plan configures', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    expect(wrapper.find('[data-test="extend-fiveHour-reset"]').exists()).toBe(false)
    for (const window of ['daily', 'weekly', 'monthly']) {
      expect(wrapper.find(`[data-test="extend-${window}-reset"]`).exists()).toBe(true)
    }
    // The picker is capped by the subscription expiry.
    expect(wrapper.get('[data-test="extend-weekly-reset"]').attributes('max')).toBeTruthy()
  })

  it('refuses a reset time beyond the subscription expiry', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    const beyondExpiry = new Date(Date.now() + 30 * 24 * 60 * 60 * 1000)
    beyondExpiry.setMilliseconds(0)
    await wrapper.get('[data-test="extend-weekly-reset"]').setValue(toLocal(beyondExpiry))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.subscriptions.resetAfterExpiry')
  })

  it('pre-fills the reset time each configured window already has', async () => {
    const dailyReset = new Date(Date.now() + 6 * 60 * 60 * 1000)
    dailyReset.setMilliseconds(0)
    const monthlyReset = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000)
    monthlyReset.setMilliseconds(0)
    getProgress.mockResolvedValue({
      daily: { used: 0, limit: 15, percentage: 0, reset_in_seconds: 1, resets_at: dailyReset.toISOString() },
      weekly: null,
      monthly: { used: 0, limit: 250, percentage: 0, reset_in_seconds: 1, resets_at: monthlyReset.toISOString() },
      five_hour: null
    })
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    const dailyValue = (wrapper.get('[data-test="extend-daily-reset"]').element as HTMLInputElement).value
    const monthlyValue = (wrapper.get('[data-test="extend-monthly-reset"]').element as HTMLInputElement).value
    const weeklyValue = (wrapper.get('[data-test="extend-weekly-reset"]').element as HTMLInputElement).value
    // The input normalizes its value, so compare the instant instead of text.
    expect(new Date(dailyValue).getTime()).toBe(dailyReset.getTime())
    expect(new Date(monthlyValue).getTime()).toBe(monthlyReset.getTime())
    expect(weeklyValue).toBe('')
  })

  it('does not echo a reset time that has already passed', async () => {
    const stale = new Date(Date.now() - 24 * 60 * 60 * 1000)
    getProgress.mockResolvedValue({
      daily: { used: 0, limit: 15, percentage: 0, reset_in_seconds: 0, resets_at: stale.toISOString() },
      weekly: null,
      monthly: null,
      five_hour: null
    })
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    expect((wrapper.get('[data-test="extend-daily-reset"]').element as HTMLInputElement).value).toBe('')
  })

  it('keeps unfilled reset times untouched', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    const monthlyReset = new Date(Date.now() + 5 * 60 * 60 * 1000)
    monthlyReset.setMilliseconds(0)
    await wrapper.get('[data-test="extend-monthly-reset"]').setValue(toLocal(monthlyReset))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { monthly_reset_at: monthlyReset.toISOString() })
  })

  it('sends only quota reset times in the quota-reset mode', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    expect(wrapper.find('[data-test="extend-days"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="extend-expires-at"]').exists()).toBe(false)
    const weeklyReset = new Date(Date.now() + 3 * 60 * 60 * 1000)
    weeklyReset.setMilliseconds(0)
    await wrapper.get('[data-test="extend-weekly-reset"]').setValue(toLocal(weeklyReset))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { weekly_reset_at: weeklyReset.toISOString() })
  })

  it('keeps relative-day adjustments from sending unused window reset times', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { days: 30 })
  })

  it('requires a day count in the relative-day mode', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('[data-test="extend-days"]').setValue('')
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.subscriptions.daysRequired')
  })

  it('sends an exact expiration timestamp in the browser timezone', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('absolute')
    const expiresAt = new Date(Date.now() + 48 * 60 * 60 * 1000)
    expiresAt.setMilliseconds(0)
    await wrapper.get('[data-test="extend-expires-at"]').setValue(toLocal(expiresAt))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { expires_at: expiresAt.toISOString() })
  })

  it('keeps each adjustment direction from sending hidden fields', async () => {
    await wrapper.get('[data-test="adjust-subscription"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="extend-days"]').setValue('5')
    await wrapper.get('[data-test="extend-adjust-mode"]').setValue('windows')
    const weeklyReset = new Date(Date.now() + 3 * 60 * 60 * 1000)
    weeklyReset.setMilliseconds(0)
    await wrapper.get('[data-test="extend-weekly-reset"]').setValue(toLocal(weeklyReset))
    await wrapper.get('#extend-subscription-form').trigger('submit')
    await flushPromises()
    expect(extend).toHaveBeenCalledWith(1, { weekly_reset_at: weeklyReset.toISOString() })
  })
})
