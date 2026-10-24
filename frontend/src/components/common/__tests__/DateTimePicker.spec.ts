import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import DateTimePicker from '../DateTimePicker.vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
function mountPicker(props: Record<string, unknown> = {}, attrs: Record<string, unknown> = {}) {
  const update = (value: string): void => { void wrapper.setProps({ modelValue: value }) }
  const wrapper = mount(DateTimePicker, { props: { modelValue: '2026-10-24T08:15', ...props, 'onUpdate:modelValue': update }, attrs, attachTo: document.body })
  return wrapper
}
const popup = () => document.querySelector<HTMLElement>('.date-time-panel')!
const button = (label: string) => [...popup().querySelectorAll<HTMLButtonElement>('button')].find(element => element.textContent === label)!
async function open(wrapper: ReturnType<typeof mountPicker>) { await wrapper.get('.date-time-trigger').trigger('click'); await flushPromises() }
function timeInput(unit: string) { return popup().querySelector<HTMLInputElement>(`input[aria-label="dates.${unit}"]`)! }
async function setTime(unit: string, value: string) { const input = timeInput(unit); input.value = value; input.dispatchEvent(new Event('input', { bubbles: true })); await flushPromises() }
async function clickDate(value: string) { popup().querySelector<HTMLButtonElement>(`[data-date="${value}"]`)!.click(); await flushPromises() }

describe('shared local date and time cards', () => {
  // User-approved date/time card: selection stays a draft until Apply, local
  // YYYY-MM-DDTHH:mm is emitted without timezone conversion, and Cancel is inert.
  it('applies local date/time and Event.target.value only after confirmation', async () => {
    const wrapper = mountPicker()
    await open(wrapper)
    expect(popup().querySelectorAll('[role=row]')).toHaveLength(6)
    expect(popup().querySelectorAll('[role=gridcell]')).toHaveLength(42)
    await clickDate('2026-10-25')
    await setTime('hours', '09')
    await setTime('minutes', '37')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['2026-10-25T09:37']])
    const change = wrapper.emitted('change')![0]![0] as Event
    expect(change).toBeInstanceOf(Event)
    expect((change.target as HTMLInputElement).value).toBe('2026-10-25T09:37')
    expect(wrapper.emitted('input')).toHaveLength(1)

    await open(wrapper)
    await clickDate('2026-10-26')
    button('common.cancel').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })

  it('navigates months and dates by keyboard without committing a value', async () => {
    const wrapper = mountPicker({ type: 'date', modelValue: '2026-10-31' })
    await open(wrapper)
    const selected = popup().querySelector<HTMLButtonElement>('[data-date="2026-10-31"]')!
    selected.focus()
    selected.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await flushPromises()
    expect(document.activeElement?.getAttribute('data-date')).toBe('2026-11-01')
    expect(wrapper.emitted('change')).toBeUndefined()
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
    await flushPromises()
    expect(document.activeElement?.getAttribute('data-date')).toBe('2026-10-26')
  })

  it('clamps month navigation to the last available day of the target month', async () => {
    // Calendar navigation moves one month even when the starting day is 31.
    const wrapper = mountPicker({ type: 'date', modelValue: '2026-10-31' })
    await open(wrapper)
    const selected = popup().querySelector<HTMLButtonElement>('[data-date="2026-10-31"]')!
    selected.focus()
    selected.dispatchEvent(new KeyboardEvent('keydown', { key: 'PageDown', bubbles: true }))
    await flushPromises()
    expect(document.activeElement?.getAttribute('data-date')).toBe('2026-11-30')
    expect(wrapper.emitted('change')).toBeUndefined()
    button('common.cancel').click()
    await flushPromises()
    await wrapper.setProps({ modelValue: '2026-03-31' })
    await open(wrapper)
    popup().querySelector<HTMLButtonElement>('[data-date="2026-03-31"]')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'PageUp', bubbles: true }))
    await flushPromises()
    expect(document.activeElement?.getAttribute('data-date')).toBe('2026-02-28')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('retains date-only form values and enforces inclusive min/max boundaries', async () => {
    const wrapper = mountPicker({ type: 'date', modelValue: '2026-10-24', min: '2026-10-23', max: '2026-10-25', name: 'expires', id: 'expiration', required: true }, { 'aria-label': 'Expiration', 'data-testid': 'expiration' })
    await open(wrapper)
    expect(popup().querySelector<HTMLButtonElement>('[data-date="2026-10-22"]')!.disabled).toBe(true)
    expect(popup().querySelector<HTMLButtonElement>('[data-date="2026-10-23"]')!.disabled).toBe(false)
    expect(popup().querySelector<HTMLButtonElement>('[data-date="2026-10-26"]')!.disabled).toBe(true)
    expect(popup().querySelector('[aria-label="dates.previousMonth"]')?.hasAttribute('disabled')).toBe(true)
    expect(wrapper.get('#expiration').attributes('aria-label')).toBe('Expiration')
    expect(wrapper.findAll('[data-testid="expiration"]')).toHaveLength(1)
    await clickDate('2026-10-25')
    button('dates.apply').click()
    await flushPromises()
    const native = wrapper.get<HTMLInputElement>('.date-time-value-proxy').element
    expect(native.name).toBe('expires')
    expect(native.type).toBe('date')
    expect(native.required).toBe(true)
    expect(native.value).toBe('2026-10-25')
    expect(wrapper.emitted('update:modelValue')).toEqual([['2026-10-25']])
  })

  it('retains seconds and validates the original time range and step', async () => {
    const wrapper = mountPicker({ type: 'time', modelValue: '09:15:30', min: '09:15:00', max: '09:16:00', step: 15 })
    await open(wrapper)
    expect(popup().querySelector('[role=grid]')).toBeNull()
    expect(timeInput('seconds').value).toBe('30')
    await setTime('seconds', '45')
    expect(button('dates.apply').disabled).toBe(false)
    await setTime('seconds', '46')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('seconds', '00')
    await setTime('minutes', '16')
    expect(button('dates.apply').disabled).toBe(false)
    await setTime('seconds', '15')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('minutes', '15')
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['09:15:15']])
  })

  it('keeps fractional seconds on unchanged drafts and supports second increment controls', async () => {
    const wrapper = mountPicker({ type: 'time', modelValue: '09:15:30.125', step: 'any' })
    await open(wrapper)
    expect(timeInput('seconds').value).toBe('30.125')
    await setTime('minutes', '16')
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['09:16:30.125']])
    await wrapper.setProps({ modelValue: '09:15:30', step: 15 })
    await open(wrapper)
    const secondsPart = timeInput('seconds').closest('.date-time-clock-part')!
    secondsPart.querySelector<HTMLButtonElement>('button:last-child')!.click()
    await flushPromises()
    expect(timeInput('seconds').value).toBe('45')
  })

  it('accepts fractional seconds throughout the last second and preserves the original step', async () => {
    // A time field with step="any" accepts all valid seconds below 60.
    const wrapper = mountPicker({ type: 'time', modelValue: '09:15:59.5', step: 'any' })
    await open(wrapper)
    expect(timeInput('seconds').value).toBe('59.5')
    expect(timeInput('seconds').step).toBe('any')
    expect(button('dates.apply').disabled).toBe(false)
    await setTime('seconds', '60')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('seconds', '0.5')
    expect(button('dates.apply').disabled).toBe(false)
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['09:15:00.5']])
  })

  it('wraps fractional second steps without changing their precision', async () => {
    const wrapper = mountPicker({ type: 'time', modelValue: '09:15:59.9', step: 0.1 })
    await open(wrapper)
    const secondsPart = timeInput('seconds').closest('.date-time-clock-part')!
    secondsPart.querySelector<HTMLButtonElement>('button:last-child')!.click()
    await flushPromises()
    expect(timeInput('seconds').value).toBe('00')
    secondsPart.querySelector<HTMLButtonElement>('button:first-child')!.click()
    await flushPromises()
    expect(timeInput('seconds').value).toBe('59.9')
    expect(button('dates.apply').disabled).toBe(false)
    expect(wrapper.emitted('change')).toBeUndefined()
  })

  it('keeps fractional min/max inclusive and enforces fractional step alignment', async () => {
    // HTML time fractions represent tenths/hundredths of a second, not integer
    // milliseconds: https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#times
    const wrapper = mountPicker({ type: 'time', modelValue: '09:15:59.5', min: '09:15:59.1', max: '09:15:59.9', step: 0.1 })
    await open(wrapper)
    expect(button('dates.apply').disabled).toBe(false)
    await setTime('seconds', '59.15')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('seconds', '59.95')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('seconds', '59.0')
    expect(button('dates.apply').disabled).toBe(true)
    await setTime('seconds', '59.1')
    expect(button('dates.apply').disabled).toBe(false)
    await setTime('seconds', '59.9')
    expect(button('dates.apply').disabled).toBe(false)
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['09:15:59.9']])
  })

  it('clears to an empty string and rejects disabled or readonly interactions', async () => {
    const wrapper = mountPicker()
    await open(wrapper)
    button('common.clear').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['']])
    await wrapper.setProps({ disabled: true })
    await wrapper.get('.date-time-trigger').trigger('click')
    expect(document.querySelector('.date-time-panel')).toBeNull()
    expect(wrapper.get<HTMLInputElement>('.date-time-value-proxy').element.disabled).toBe(true)
    await wrapper.setProps({ disabled: false, readonly: true })
    await wrapper.get('.date-time-trigger').trigger('click')
    expect(document.querySelector('.date-time-panel')).toBeNull()
    expect(wrapper.get<HTMLInputElement>('.date-time-value-proxy').element.readOnly).toBe(true)
  })

  it('opens the custom picker for required form invalidity and preserves native form submission', async () => {
    const Host = defineComponent({ components: { DateTimePicker }, setup: () => ({ value: ref('') }), template: '<form><DateTimePicker v-model="value" type="date" name="expires" required /></form>' })
    const wrapper = mount(Host, { attachTo: document.body })
    const native = wrapper.get<HTMLInputElement>('.date-time-value-proxy').element
    const event = new Event('invalid', { cancelable: true })
    native.dispatchEvent(event)
    await flushPromises()
    expect(event.defaultPrevented).toBe(true)
    expect(document.querySelector('.date-time-panel')).not.toBeNull()
    const form = wrapper.get('form').element as HTMLFormElement
    expect(native.validity.valueMissing).toBe(true)
    await clickDate(popup().querySelector<HTMLButtonElement>('[aria-selected="true"]')!.dataset.date!)
    button('dates.apply').click()
    await flushPromises()
    expect(native.validity.valid).toBe(true)
    expect(new FormData(form).get('expires')).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  })

  it('respects DOM readonly guards applied by the existing support-view directive', async () => {
    const wrapper = mountPicker()
    await open(wrapper)
    await setTime('minutes', '42')
    wrapper.get<HTMLInputElement>('.date-time-value-proxy').element.readOnly = true
    wrapper.get<HTMLButtonElement>('.date-time-trigger').element.disabled = true
    button('dates.apply').click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(document.querySelector('.date-time-panel')).toBeNull()
    wrapper.get<HTMLButtonElement>('.date-time-trigger').element.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('.date-time-panel')).toBeNull()
  })

  it('routes wrapping-label activation to the custom card and prevents the native popup', async () => {
    const Host = defineComponent({ components: { DateTimePicker }, template: '<label>Expiration<DateTimePicker model-value="2026-10-24" type="date" /></label>' })
    const wrapper = mount(Host, { attachTo: document.body })
    const native = wrapper.get<HTMLInputElement>('.date-time-value-proxy').element
    const prevented: boolean[] = []
    native.addEventListener('click', event => prevented.push(event.defaultPrevented))
    wrapper.get<HTMLLabelElement>('label').element.click()
    await flushPromises()
    expect(prevented).toEqual([true])
    expect(document.querySelector('.date-time-panel')).not.toBeNull()
    expect(document.activeElement).not.toBe(native)
    expect(wrapper.getComponent(DateTimePicker).emitted('change')).toBeUndefined()
  })

  it('keeps the focus on an outside field when the card is dismissed by that click', async () => {
    const wrapper = mountPicker()
    await open(wrapper)
    const outside = document.createElement('input')
    document.body.appendChild(outside)
    outside.focus()
    outside.click()
    await flushPromises()
    expect(document.querySelector('.date-time-panel')).toBeNull()
    expect(document.activeElement).toBe(outside)
    expect(wrapper.emitted('change')).toBeUndefined()
    outside.remove()
  })

  it('closes the picker first on Escape inside a modal and retains the parent scroll lock', async () => {
    const Host = defineComponent({ components: { BaseDialog, DateTimePicker }, template: '<BaseDialog :show="true" title="Parent"><DateTimePicker model-value="2026-10-24" type="date" /></BaseDialog>' })
    const wrapper = mount(Host, { attachTo: document.body })
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.date-time-trigger')!.click()
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.querySelector('.date-time-panel')).toBeNull()
    expect(wrapper.getComponent(BaseDialog).emitted('close')).toBeUndefined()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.getComponent(BaseDialog).emitted('close')).toHaveLength(1)
  })

  it('keeps a body-portal card inside a narrow viewport and repositions on scroll', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    let anchorLeft = 280
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('date-time-panel')
        ? { left: 0, top: 0, width: 352, height: 440 } as DOMRect
        : { left: anchorLeft, top: 600, bottom: 642, width: 98, height: 42 } as DOMRect
    })
    const wrapper = mountPicker()
    await open(wrapper)
    expect(wrapper.find('.date-time-panel').exists()).toBe(false)
    expect(popup().style.left).toBe('26px')
    expect(popup().style.top).toBe('152px')
    anchorLeft = 12
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(popup().style.left).toBe('12px')
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
})
