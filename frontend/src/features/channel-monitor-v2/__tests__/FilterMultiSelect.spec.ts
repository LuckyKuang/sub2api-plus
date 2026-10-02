import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import FilterMultiSelect from '../FilterMultiSelect.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)

function mountFilter() {
  return mount(FilterMultiSelect, { props: {
    label: 'Platforms', allLabel: 'All platforms', modelValue: [],
    options: [{ value: 'openai', label: 'OpenAI', count: 2 }, { value: 'gemini', label: 'Gemini', count: 3 }],
  } })
}

describe('checkbox multi-selection filter', () => {
  it('keeps the dropdown open while adding, removing and clearing choices', async () => {
    const wrapper = mountFilter()
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('.select-trigger').trigger('click')
    const popup = document.querySelector<HTMLElement>('[aria-multiselectable="true"]')!
    const choices = popup.querySelectorAll<HTMLElement>('[role="option"]')
    expect(choices[0].querySelector('.checkbox')).not.toBeNull()
    expect(choices[0].querySelector('small')?.textContent).toBe('2')
    choices[0].click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai']])
    await wrapper.setProps({ modelValue: ['openai'] })
    expect(choices[0].getAttribute('aria-selected')).toBe('true')
    choices[1].click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai', 'gemini']])
    await wrapper.setProps({ modelValue: ['openai', 'gemini'] })
    choices[0].click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['gemini']])
    await wrapper.setProps({ modelValue: ['gemini'] })
    popup.querySelector<HTMLElement>('.select-option-group')!.click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('true')
    expect(document.querySelector('[aria-multiselectable="true"]')).toBe(popup)
  })

  it('closes on Escape and outside clicks without changing selected values', async () => {
    const wrapper = mountFilter()
    await wrapper.setProps({ modelValue: ['openai'] })
    await wrapper.get('.select-trigger').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
    await wrapper.get('.select-trigger').trigger('click')
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
