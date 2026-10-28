import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'

import BaseDialog from '../BaseDialog.vue'
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

const rangePanel = () => new DOMWrapper(document.body.querySelector<HTMLElement>('.date-picker-dropdown')!)
const rangeIsOpen = () => !!document.body.querySelector('.date-picker-dropdown')

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
    expect(rangeIsOpen()).toBe(true)
    expect(wrapper.emitted('change')).toBeUndefined()
    const apply = [...calendar.querySelectorAll('button')].find(button => button.textContent === 'Apply')!
    apply.click()
    await flushPromises()
    expect(start.emitted('update:modelValue')?.at(-1)).toEqual(['2026-10-05'])
    expect(start.props('modelValue')).toBe('2026-10-05')
    expect(document.body.querySelector('.date-time-panel')).toBeNull()
    expect(rangeIsOpen()).toBe(true)
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    await rangePanel().get('.date-picker-apply').trigger('click')
    expect(wrapper.emitted('update:startDate')?.at(-1)).toEqual(['2026-10-05'])
    expect(wrapper.emitted('update:endDate')?.at(-1)).toEqual(['2026-10-22'])
  })

  it('closes only the calendar on the first Escape, then closes the containing range menu', async () => {
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      attachTo: document.body,
      global: { stubs: { Icon: true, Transition: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.findAllComponents(DateTimePicker)[0].get('.date-time-trigger').trigger('click')
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.body.querySelector('.date-time-panel')).toBeNull()
    expect(rangeIsOpen()).toBe(true)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(rangeIsOpen()).toBe(false)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
  })

  it('keeps keyboard focus in the range popup and closes calendar, range, and parent dialog one Escape at a time', async () => {
    const registrations = vi.spyOn(document, 'addEventListener')
    const wrapper = mount(defineComponent({
      components: { BaseDialog, DateRangePicker },
      setup: () => ({ show: ref(true) }),
      template: '<BaseDialog :show="show" title="Usage filters" @close="show = false"><DateRangePicker start-date="2026-10-01" end-date="2026-10-22" /><button>Parent action</button></BaseDialog>',
    }), { attachTo: document.body, global: { stubs: { Icon: true, Transition: true } } })
    // Finish the parent dialog's initial focus before simulating the user's
    // mouse focus on the range trigger; VTU click alone does not focus it.
    await flushPromises()
    const parent = wrapper.getComponent(BaseDialog)
    const range = wrapper.getComponent(DateRangePicker)
    const trigger = range.get<HTMLButtonElement>('.date-picker-trigger')
    trigger.element.focus()
    await trigger.trigger('click')
    await flushPromises()
    const panel = rangePanel().element
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-controls')).toBe(panel.id)
    expect(trigger.attributes('aria-haspopup')).toBe('dialog')
    expect(panel.getAttribute('aria-label')).toBe('Select date range')
    expect(panel.getAttribute('role')).toBe('dialog')
    const first = panel.querySelector<HTMLButtonElement>('button')!
    const last = panel.querySelector<HTMLButtonElement>('.date-picker-apply')!
    expect(document.activeElement).toBe(first)
    last.focus()
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.activeElement).toBe(first)
    first.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.activeElement).toBe(last)

    const calendarTrigger = range.findAllComponents(DateTimePicker)[0].get<HTMLButtonElement>('.date-time-trigger')
    calendarTrigger.element.focus()
    await calendarTrigger.trigger('click')
    await flushPromises()
    const pressEscapeWithMicrotasks = async () => {
      const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
      // Browsers can flush Vue's close watcher between native document
      // listeners. Simulate that ordering instead of one synchronous dispatch.
      for (const [type, listener] of registrations.mock.calls) {
        if (type !== 'keydown' || !listener) continue
        if (typeof listener === 'function') listener.call(document, event)
        else listener.handleEvent(event)
        await flushPromises()
      }
      expect(event.defaultPrevented).toBe(true)
    }
    await pressEscapeWithMicrotasks()
    expect(document.body.querySelector('.date-time-panel')).toBeNull()
    expect(rangeIsOpen()).toBe(true)
    expect(document.activeElement).toBe(calendarTrigger.element)
    expect(parent.emitted('close')).toBeUndefined()
    await pressEscapeWithMicrotasks()
    expect(rangeIsOpen()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(parent.emitted('close')).toBeUndefined()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(range.emitted('change')).toBeUndefined()
    expect(range.emitted('update:startDate')).toBeUndefined()
    expect(range.emitted('update:endDate')).toBeUndefined()
    await pressEscapeWithMicrotasks()
    expect(parent.emitted('close')).toHaveLength(1)
    expect(parent.props('show')).toBe(false)
  })

  it('preserves the clicked outside control focus when dismissing the popup', async () => {
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      attachTo: document.body,
      global: { stubs: { Icon: true, Transition: true } },
    })
    const trigger = wrapper.get<HTMLButtonElement>('.date-picker-trigger')
    trigger.element.focus()
    await trigger.trigger('click')
    await flushPromises()
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    outside.click()
    await flushPromises()
    expect(rangeIsOpen()).toBe(false)
    expect(document.activeElement).toBe(outside)
    expect(wrapper.emitted('change')).toBeUndefined()
    outside.remove()
  })

  // docs/UI_INTERACTION.md: retain the anchor when it fits; otherwise keep the
  // expanded panel 12px from the viewport edge without applying date changes.
  it('escapes a clipping container and stays inside the viewport when resized or scrolled near the bottom', async () => {
    vi.stubGlobal('innerWidth', 1280)
    vi.stubGlobal('innerHeight', 800)
    let anchor = new DOMRect(96, 100, 200, 40)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('date-picker-dropdown') ? new DOMRect(0, 0, 320, 260) : anchor
    })
    const clipped = document.createElement('div')
    clipped.className = 'modal-body workspace-scroll'
    clipped.style.overflow = 'hidden'
    clipped.style.height = '100px'
    document.body.append(clipped)
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      attachTo: clipped,
      global: { stubs: { Icon: true, Transition: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    await flushPromises()
    const panel = rangePanel().element
    expect(document.body.contains(panel)).toBe(true)
    expect(clipped.contains(panel)).toBe(false)
    expect(panel.closest('.modal-body, .workspace-scroll')).toBeNull()
    expect(panel.style.left).toBe('96px')
    expect(panel.style.top).toBe('148px')

    vi.stubGlobal('innerWidth', 390)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    // A 320px panel must start at x=58 and end at x=378, preserving 12px.
    expect(panel.style.left).toBe('58px')
    anchor = new DOMRect(300, 700, 200, 40)
    clipped.dispatchEvent(new Event('scroll'))
    await flushPromises()
    // The menu opens above the trigger and ends at y=692, with an 8px gap.
    expect(panel.style.top).toBe('432px')
    expect(panel.style.maxHeight).toBe('680px')
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    expect(wrapper.emitted('update:endDate')).toBeUndefined()
    document.body.click()
    await flushPromises()
    expect(rangeIsOpen()).toBe(false)
    wrapper.unmount()
    clipped.remove()
  })

  it('constrains the scrollable panel to the visible viewport after keyboard resize and viewport panning', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 800)
    const viewport = Object.assign(new EventTarget(), { width: 390, height: 800, offsetLeft: 0, offsetTop: 0 })
    vi.stubGlobal('visualViewport', viewport)
    const removeListener = vi.spyOn(viewport, 'removeEventListener')
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('date-picker-dropdown') ? new DOMRect(0, 0, 320, 260) : new DOMRect(300, 200, 80, 40)
    })
    const wrapper = mount(DateRangePicker, {
      props: { startDate: '2026-10-01', endDate: '2026-10-22' },
      global: { stubs: { Icon: true, Transition: true } },
    })
    await wrapper.get('.date-picker-trigger').trigger('click')
    await flushPromises()

    // A keyboard leaves only y=100..400 visible: the panel must fit either
    // side and retain access to its content through its scroll container.
    Object.assign(viewport, { width: 300, height: 300, offsetLeft: 40, offsetTop: 100 })
    viewport.dispatchEvent(new Event('resize'))
    await flushPromises()
    const panel = rangePanel().element
    expect(panel.style.left).toBe('52px')
    expect(panel.style.maxWidth).toBe('276px')
    expect(panel.style.top).toBe('248px')
    expect(panel.style.maxHeight).toBe('140px')
    expect(Number.parseFloat(panel.style.top) + Number.parseFloat(panel.style.maxHeight)).toBe(388)
    expect(rangePanel().find('.date-picker-apply').exists()).toBe(true)

    Object.assign(viewport, { offsetTop: 180 })
    viewport.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(panel.style.top).toBe('248px')
    expect(panel.style.maxHeight).toBe('220px')
    expect(wrapper.emitted('change')).toBeUndefined()
    wrapper.unmount()
    expect(removeListener).toHaveBeenCalledWith('resize', expect.any(Function))
    expect(removeListener).toHaveBeenCalledWith('scroll', expect.any(Function))
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
    const presetButton = rangePanel().findAll('.date-picker-preset').find((node) =>
      node.text().includes('Last 24 Hours')
    )
    expect(presetButton).toBeDefined()

    await presetButton!.trigger('click')
    await rangePanel().get('.date-picker-apply').trigger('click')

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
