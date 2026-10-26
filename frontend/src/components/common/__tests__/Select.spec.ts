import { mount, enableAutoUnmount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Select from '../Select.vue'
import { chooseSelect, openSelect } from '@/__tests__/helpers/select'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); document.body.innerHTML = '' })

const options = [
  { value: '', label: 'All' },
  { value: 0, label: 'Zero' },
  { value: 1, label: 'Numeric one' },
  { value: '1', label: 'Text one' },
  { value: false, label: 'False' },
  { value: null, label: 'None' },
]

describe('Select selection controls', () => {
  it('opens the shared themed panel without search and preserves typed values', async () => {
    const wrapper = mount(Select, { props: { modelValue: '', options, searchable: false } })
    expect(wrapper.find('select').exists()).toBe(false)
    for (const [label, value] of [['Zero', 0], ['Numeric one', 1], ['Text one', '1'], ['False', false], ['None', null], ['All', '']] as const) {
      const panel = await openSelect(wrapper)
      expect(panel.classes()).toContain('select-dropdown-portal')
      expect(panel.element.parentElement).toBe(document.body)
      expect(panel.find('input').exists()).toBe(false)
      await chooseSelect(wrapper, label)
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([value])
      expect(wrapper.emitted('change')?.at(-1)?.[1]).toEqual(options.find(option => option.value === value))
    }
  })

  it('selects and reopens when the wall clock moves backwards between interactions', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_760_000_000_000)
    const wrapper = mount(Select, { props: { modelValue: '', options, searchable: false } })
    await openSelect(wrapper)

    now.mockReturnValue(1_759_999_999_995)
    await chooseSelect(wrapper, 'Numeric one')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
    expect(wrapper.emitted('change')).toEqual([[1, options[2]]])
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')

    await chooseSelect(wrapper, 'Text one')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1], ['1']])
    expect(wrapper.emitted('change')).toEqual([[1, options[2]], ['1', options[3]]])
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
  })

  it('supports custom value and label keys', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [{ id: 7, display_name: 'Account seven' }], valueKey: 'id', labelKey: 'display_name' } })
    const panel = await openSelect(wrapper)
    expect(panel.get('[role="option"]').text()).toBe('Account seven')
    await chooseSelect(wrapper, 'Account seven')
    expect(wrapper.emitted('update:modelValue')).toEqual([[7]])
  })

  it('keeps group headers unselectable without disabling their children', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [
      { value: 'header', label: 'Provider', kind: 'group', disabled: true },
      { value: 1, label: 'Available' }, { value: 2, label: 'Unavailable', disabled: true },
    ] } })
    const panel = await openSelect(wrapper)
    await panel.get('.select-option-group').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await chooseSelect(wrapper, 'Available')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
    await chooseSelect(wrapper, 'Unavailable')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
  })

  it('exposes disabled state, errors and labels on the trigger', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options, searchable: false, disabled: true, error: true, id: 'choice', ariaLabel: 'Choose platform', ariaDescribedby: 'hint' } })
    const select = wrapper.get('.select-trigger')
    expect(select.attributes()).toMatchObject({ id: 'choice', disabled: '', 'aria-label': 'Choose platform', 'aria-describedby': 'hint', 'aria-invalid': 'true' })
    await select.trigger('click')
    await select.trigger('keydown', { key: 'ArrowDown' })
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('supports unavailable/loading options on plain dropdowns', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [], loading: true } })
    const panel = await openSelect(wrapper)
    expect(panel.get('.select-empty').text()).toBe('common.loading')
    await wrapper.setProps({ loading: false, emptyText: 'No accounts' })
    expect(panel.get('.select-empty').text()).toBe('No accounts')
  })

  it('keeps clearable selection inside the original trigger', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options, searchable: false, clearable: true } })
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('.select-clear').trigger('click')
    expect(wrapper.emitted('change')).toEqual([[null, null]])
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
  })

  it('renders rich selected content and options inside the original dropdown', async () => {
    const wrapper = mount(Select, {
      props: { modelValue: undefined, options: options.slice(0, 3), searchable: false },
      slots: {
        selected: '<template #selected="{ option }"><span data-test="badge">{{ option?.label || "Choose a group" }}</span></template>',
        option: '<template #option="{ option }"><span data-test="rich-option">{{ option.label }} · rate</span></template>',
      },
    })
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('.select-trigger [data-test="badge"]').text()).toBe('Choose a group')
    await wrapper.setProps({ modelValue: 1 })
    expect(wrapper.get('.select-trigger [data-test="badge"]').text()).toBe('Numeric one')
    await wrapper.get('.select-trigger').trigger('click')
    const popup = document.querySelector('.select-dropdown-portal')!
    expect(popup.querySelector('input')).toBeNull()
    expect(popup.querySelector('[data-test="rich-option"]')?.textContent).toContain('rate')
    popup.querySelectorAll<HTMLElement>('[role="option"]')[1].click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
    await wrapper.setProps({ modelValue: undefined })
    expect(wrapper.get('.select-trigger [data-test="badge"]').text()).toBe('Choose a group')
  })

  it('changes automatic search visibility without changing the menu renderer', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options: options.slice(0, 5) } })
    expect(wrapper.find('select').exists()).toBe(false)
    let panel = await openSelect(wrapper)
    expect(panel.find('input').exists()).toBe(false)
    await wrapper.setProps({ options })
    expect(wrapper.find('select').exists()).toBe(false)
    panel = await openSelect(wrapper)
    const input = document.querySelector<HTMLInputElement>('.select-search-input')!
    input.value = 'numeric'
    input.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()
    const choices = input.closest('.select-dropdown-portal')!.querySelectorAll<HTMLElement>('[role="option"]')
    expect(choices).toHaveLength(1)
    expect(choices[0].textContent).toContain('Numeric one')
    choices[0].click()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([1])
    await wrapper.setProps({ searchable: false })
    expect(wrapper.find('select').exists()).toBe(false)
    panel = await openSelect(wrapper)
    expect(panel.find('input').exists()).toBe(false)
    expect(panel.findAll('[role="option"]')).toHaveLength(6)
  })

  it('keeps search inside the original popup instead of splitting the field', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options, searchable: true } })
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.get('.select-trigger').text()).toContain('Numeric one')
    await wrapper.get('.select-trigger').trigger('click')
    expect(document.querySelector('.select-dropdown-portal .select-search-input')).not.toBeNull()
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('supports keyboard selection, skips disabled/group choices, and closes on Escape and Tab', async () => {
    const wrapper = mount(Select, { attachTo: document.body, props: { modelValue: 0, searchable: false, options: [
      { value: 'group', label: 'Header', kind: 'group' },
      { value: 0, label: 'Zero' },
      { value: 1, label: 'Disabled', disabled: true },
      { value: 2, label: 'Two' },
    ] } })
    const trigger = wrapper.get('.select-trigger')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await trigger.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]])
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('click')
    await trigger.trigger('keydown', { key: 'Home' })
    await trigger.trigger('keydown', { key: ' ' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
    await trigger.trigger('click')
    await trigger.trigger('keydown', { key: 'Escape' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    await trigger.trigger('keydown', { key: 'Tab' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })

  it('keeps layout on the wrapper and forwards control attributes to the trigger', () => {
    const wrapper = mount(Select, { attrs: { class: 'w-24', 'data-testid': 'picker', title: 'Choose' }, props: { modelValue: 1, options, triggerClass: 'py-1 text-xs' } })
    expect(wrapper.classes()).toContain('w-24')
    expect(wrapper.get('[data-testid="picker"]').classes()).toContain('select-trigger')
    expect(wrapper.get('.select-trigger').classes()).toContain('text-xs')
    expect(wrapper.get('.select-trigger').classes()).not.toContain('w-24')
  })
})
