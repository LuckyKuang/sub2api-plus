import { mount, enableAutoUnmount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Select from '../Select.vue'

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
  it('renders a native control with no custom popup and preserves typed values', async () => {
    const wrapper = mount(Select, { props: { modelValue: '', options, searchable: false } })
    const select = wrapper.get('select')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    for (const [key, value] of [['number:0', 0], ['number:1', 1], ['string:1', '1'], ['boolean:false', false], ['object:null', null], ['string:', '']] as const) {
      await select.setValue(key)
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([value])
      expect(wrapper.emitted('change')?.at(-1)?.[1]).toEqual(options.find(option => option.value === value))
    }
  })

  it('supports custom value and label keys', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [{ id: 7, display_name: 'Account seven' }], valueKey: 'id', labelKey: 'display_name' } })
    await wrapper.get('select').setValue('number:7')
    expect(wrapper.emitted('update:modelValue')).toEqual([[7]])
    expect(wrapper.get('option[value="number:7"]').text()).toBe('Account seven')
  })

  it('keeps disabled headers as native optgroups without disabling their children', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [
      { value: 'header', label: 'Provider', kind: 'group', disabled: true },
      { value: 1, label: 'Available' }, { value: 2, label: 'Unavailable', disabled: true },
    ] } })
    expect(wrapper.get('optgroup').attributes('label')).toBe('Provider')
    expect(wrapper.get('optgroup').attributes('disabled')).toBeUndefined()
    await wrapper.get('select').setValue('number:1')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
    await wrapper.get('select').setValue('number:2')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
  })

  it('exposes disabled state, errors and labels on the native form control', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options, searchable: false, disabled: true, error: true, id: 'choice', ariaLabel: 'Choose platform', ariaDescribedby: 'hint' } })
    const select = wrapper.get('select')
    expect(select.attributes()).toMatchObject({ id: 'choice', disabled: '', 'aria-label': 'Choose platform', 'aria-describedby': 'hint', 'aria-invalid': 'true' })
    await select.setValue('number:0')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('supports unavailable/loading options on plain dropdowns', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [], loading: true } })
    expect(wrapper.get('option').text()).toBe('common.loading')
    await wrapper.setProps({ loading: false, emptyText: 'No accounts' })
    expect(wrapper.get('option').text()).toBe('No accounts')
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

  it('restores automatic search above five options while keeping smaller plain controls native', async () => {
    const wrapper = mount(Select, { props: { modelValue: 1, options: options.slice(0, 5) } })
    expect(wrapper.find('select').exists()).toBe(true)
    await wrapper.setProps({ options })
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('.select-trigger').trigger('click')
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
    expect(wrapper.find('select').exists()).toBe(true)
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
})
