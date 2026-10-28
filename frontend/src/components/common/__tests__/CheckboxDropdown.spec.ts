import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import CheckboxDropdown from '../CheckboxDropdown.vue'

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

function mountDropdown(disabled = false) {
  return mount(CheckboxDropdown, {
    attachTo: document.body,
    props: {
      modelValue: [2],
      options: [{ value: 1, label: 'One' }, { value: 2, label: 'Two' }, { value: 3, label: 'Three' }],
      label: 'Groups',
      disabled,
    },
  })
}

function options() { return Array.from(document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')) }
async function key(element: Element, name: string, shiftKey = false) {
  const event = new KeyboardEvent('keydown', { key: name, shiftKey, bubbles: true, cancelable: true })
  element.dispatchEvent(event)
  await nextTick()
  return event
}

describe('CheckboxDropdown keyboard interaction', () => {
  it('enters the teleported options and keeps repeated selections inside the menu', async () => {
    const wrapper = mountDropdown()
    await wrapper.get('button').trigger('click')
    expect(options()[0]).toBe(document.activeElement)
    await key(options()[0], 'Enter')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 1]])
    await wrapper.setProps({ modelValue: [2, 1] })
    await key(options()[0], 'ArrowDown')
    expect(options()[1]).toBe(document.activeElement)
    await key(options()[1], 'Enter')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1]])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    await key(options()[1], 'End')
    expect(options()[2]).toBe(document.activeElement)
    await key(options()[2], 'Escape')
    expect(options()).toHaveLength(0)
    expect(wrapper.get('button').element).toBe(document.activeElement)
  })

  it('opens from either arrow and restores form tab order at panel boundaries', async () => {
    const wrapper = mountDropdown()
    const trigger = wrapper.get('button')
    await trigger.trigger('keydown', { key: 'ArrowUp' })
    expect(options()[2]).toBe(document.activeElement)
    const forward = await key(options()[2], 'Tab')
    expect(forward.defaultPrevented).toBe(false)
    expect(trigger.element).toBe(document.activeElement)
    expect(options()).toHaveLength(0)
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(options()[0]).toBe(document.activeElement)
    const backward = await key(options()[0], 'Tab', true)
    expect(backward.defaultPrevented).toBe(true)
    expect(trigger.element).toBe(document.activeElement)
    expect(options()).toHaveLength(0)
  })

  it('does not expose choices when disabled and closes if disabled while open', async () => {
    const wrapper = mountDropdown(true)
    await wrapper.get('button').trigger('keydown', { key: 'ArrowDown' })
    expect(options()).toHaveLength(0)
    await wrapper.setProps({ disabled: false })
    await wrapper.get('button').trigger('click')
    expect(options()).toHaveLength(3)
    await wrapper.setProps({ disabled: true })
    expect(options()).toHaveLength(0)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
