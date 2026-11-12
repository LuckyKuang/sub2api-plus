import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import CheckboxDropdown from '../CheckboxDropdown.vue'

enableAutoUnmount(cleanup => afterEach(async () => {
  cleanup()
  try {
    await vi.waitFor(() => expect(options()).toHaveLength(0))
  } finally {
    document.body.replaceChildren()
  }
}))

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
  it('keeps typed option metadata in rich options and selected content while searching and changing choices', async () => {
    const wrapper = mount(CheckboxDropdown, {
      attachTo: document.body,
      props: {
        modelValue: [2],
        options: [{ value: 1, label: 'One', platform: 'anthropic' }, { value: 2, label: 'Two', platform: 'openai' }],
        label: 'Groups',
        searchable: true,
      },
      slots: {
        selected: '<template #selected="{ options }"><span v-for="option in options" :key="option.value" data-test="selected-platform" :data-platform="option.platform">{{ option.label }}</span></template>',
        option: '<template #option="{ option, selected }"><span data-test="option-platform" :data-platform="option.platform" :data-selected="selected">{{ option.label }}</span></template>',
      },
    })
    expect(wrapper.get('[data-test="selected-platform"]').attributes('data-platform')).toBe('openai')
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[data-platform="anthropic"][data-test="option-platform"]')).not.toBeNull()
    options()[0]!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 1]])
    await wrapper.setProps({ modelValue: [2, 1] })
    const search = document.querySelector<HTMLInputElement>('input[type="search"]')!
    search.value = 'Two'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(options()).toHaveLength(1)
    expect(wrapper.findAll('[data-test="selected-platform"]').map(element => element.attributes('data-platform'))).toEqual(['anthropic', 'openai'])
    expect(options()[0]!.getAttribute('aria-label')).toBe('Two')
  })

  it('starts at the first search result, preserves IME navigation and prevents search Enter from submitting the form', async () => {
    const wrapper = mountDropdown()
    await wrapper.setProps({ searchable: true })
    await wrapper.get('button').trigger('click')
    await nextTick()
    const search = document.querySelector<HTMLInputElement>('input[type="search"]')!
    const enter = await key(search, 'Enter')
    expect(enter.defaultPrevented).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const composing = new KeyboardEvent('keydown', { key: 'ArrowDown', isComposing: true, bubbles: true, cancelable: true })
    search.dispatchEvent(composing)
    await nextTick()
    expect(composing.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(search)
    await key(search, 'ArrowDown')
    expect(document.activeElement).toBe(options()[0])
  })

  it('keeps search and repeated checkbox selection in one integrated dropdown', async () => {
    const wrapper = mountDropdown()
    await wrapper.setProps({ searchable: true, searchPlaceholder: 'Find groups' })
    await wrapper.get('button').trigger('click')
    await nextTick()
    const search = document.querySelector<HTMLInputElement>('input[type="search"]')!
    expect(document.activeElement).toBe(search)
    search.value = 'Tw'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(options()).toHaveLength(1)
    expect(options()[0]!.value).toBe('2')
    await key(search, 'ArrowDown')
    expect(document.activeElement).toBe(options()[0])
    options()[0]!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    expect(search.isConnected).toBe(true)
    expect(options()).toHaveLength(1)
    await key(options()[0]!, 'Escape')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

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
    await vi.waitFor(() => expect(options()).toHaveLength(0))
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
    await vi.waitFor(() => expect(options()).toHaveLength(0))
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(options()[0]).toBe(document.activeElement)
    const backward = await key(options()[0], 'Tab', true)
    expect(backward.defaultPrevented).toBe(true)
    expect(trigger.element).toBe(document.activeElement)
    await vi.waitFor(() => expect(options()).toHaveLength(0))
  })

  it('does not expose choices when disabled and closes if disabled while open', async () => {
    const wrapper = mountDropdown(true)
    await wrapper.get('button').trigger('keydown', { key: 'ArrowDown' })
    expect(options()).toHaveLength(0)
    await wrapper.setProps({ disabled: false })
    await wrapper.get('button').trigger('click')
    expect(options()).toHaveLength(3)
    await wrapper.setProps({ disabled: true })
    await vi.waitFor(() => expect(options()).toHaveLength(0))
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
