import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'

import DateRangePicker from '../DateRangePicker.vue'
import DateTimePicker from '../DateTimePicker.vue'

const messages: Record<string, string> = {
  'dates.today': 'Today',
  'dates.yesterday': 'Yesterday',
  'dates.last24Hours': 'Last 24 Hours',
  'dates.last7Days': 'Last 7 Days',
  'dates.last14Days': 'Last 14 Days',
  'dates.last30Days': 'Last 30 Days',
  'dates.thisMonth': 'This Month',
  'dates.lastMonth': 'Last Month',
  'dates.startDate': 'Start Date',
  'dates.endDate': 'End Date',
  'dates.apply': 'Apply',
  'dates.selectDateRange': 'Select date range'
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => messages[key] ?? key,
    locale: ref('en')
  })
}))

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('DateRangePicker', () => {
  // Approved audit: a calendar portal inside the range menu must not dismiss
  // the range or publish a filter while browsing/cancelling the calendar.
  it('keeps the range open while its calendar is used and publishes only on range apply', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 24, 12))
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      attachTo: document.body,
      global: { stubs: { Icon: true, Transition: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    const start = wrapper.findAllComponents(DateTimePicker)[0]
    await start.get('.date-time-trigger').trigger('click')
    await flushPromises()
    const calendar = document.body.querySelector('.date-time-panel')!
    expect(calendar.parentElement).toBe(document.body)
    // Vue ignores bubbling events stamped at/before a listener's attachment.
    // Keep the date fixed while letting this later user click occur after mount.
    vi.setSystemTime(new Date(2026, 9, 24, 12, 0, 0, 1))
    expect(start.props('max')).toBe('2026-10-22')
    const day = calendar.querySelector<HTMLButtonElement>('[data-date="2026-10-05"]')!
    expect(day.disabled).toBe(false)
    day.click()
    await flushPromises()
    expect(calendar.querySelector('[data-date="2026-10-05"]')?.getAttribute('aria-selected')).toBe('true')
    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(true)
    expect(wrapper.emitted('change')).toBeUndefined()
    const apply = [...calendar.querySelectorAll('button')].find(button => button.textContent === 'Apply')!
    apply.click()
    await flushPromises()
    expect(start.emitted('update:modelValue')?.at(-1)).toEqual(['2026-10-05'])
    expect(start.props('modelValue')).toBe('2026-10-05')
    expect(document.body.querySelector('.date-time-panel')).toBeNull()
    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(true)
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    await wrapper.get('.date-picker-apply').trigger('click')
    expect(wrapper.emitted('update:startDate')?.at(-1)).toEqual(['2026-10-05'])
    expect(wrapper.emitted('update:endDate')?.at(-1)).toEqual(['2026-10-22'])
  })

  it('closes only the calendar on the first Escape, then closes the containing range menu', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    // A trusted browser event can run a microtask checkpoint between native
    // listeners. Eager microtasks expose a parent that dismisses too early.
    vi.stubGlobal('queueMicrotask', (callback: VoidFunction) => callback())
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      attachTo: document.body,
      global: { stubs: { Icon: true, Transition: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.findAllComponents(DateTimePicker)[0].get('.date-time-trigger').trigger('click')
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await vi.runOnlyPendingTimersAsync()
    await flushPromises()
    expect(document.body.querySelector('.date-time-panel')).toBeNull()
    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(true)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await vi.runOnlyPendingTimersAsync()
    await flushPromises()
    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(false)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
  })

  // docs/UI_INTERACTION.md: retain the anchor when it fits; otherwise keep the
  // expanded panel 12px from the viewport edge without applying date changes.
  it('keeps the panel within a narrow viewport and repositions on resize', async () => {
    vi.stubGlobal('innerWidth', 1280)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return { left: 96, width: this.classList.contains('date-picker-dropdown') ? 320 : 200 } as DOMRect
    })
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      global: { stubs: { Icon: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    await flushPromises()
    expect(Number.parseFloat(wrapper.get<HTMLElement>('.date-picker-dropdown').element.style.left || '0')).toBe(0)

    vi.stubGlobal('innerWidth', 390)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    // The former fixed alignment ended at x=416. A 320px panel must instead
    // start at x=58 and end at x=378 in this 390px viewport.
    expect(Number.parseFloat(wrapper.get<HTMLElement>('.date-picker-dropdown').element.style.left || '0')).toBe(-38)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
  })

  it('uses last 24 hours as the default recognized preset', () => {
    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    const wrapper = mount(DateRangePicker, {
      props: {
        startDate: formatLocalDate(yesterday),
        endDate: formatLocalDate(now)
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('Last 24 Hours')
  })

  it('emits range updates with last24Hours preset when applied', async () => {
    const now = new Date()
    const today = formatLocalDate(now)

    const wrapper = mount(DateRangePicker, {
      props: {
        startDate: today,
        endDate: today
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    await wrapper.find('.date-picker-trigger').trigger('click')
    const presetButton = wrapper.findAll('.date-picker-preset').find((node) =>
      node.text().includes('Last 24 Hours')
    )
    expect(presetButton).toBeDefined()

    await presetButton!.trigger('click')
    await wrapper.find('.date-picker-apply').trigger('click')

    const nowAfterClick = new Date()
    const yesterdayAfterClick = new Date(nowAfterClick.getTime() - 24 * 60 * 60 * 1000)
    const expectedStart = formatLocalDate(yesterdayAfterClick)
    const expectedEnd = formatLocalDate(nowAfterClick)

    expect(wrapper.emitted('update:startDate')?.[0]).toEqual([expectedStart])
    expect(wrapper.emitted('update:endDate')?.[0]).toEqual([expectedEnd])
    expect(wrapper.emitted('change')?.[0]).toEqual([
      {
        startDate: expectedStart,
        endDate: expectedEnd,
        preset: 'last24Hours'
      }
    ])
  })
})
