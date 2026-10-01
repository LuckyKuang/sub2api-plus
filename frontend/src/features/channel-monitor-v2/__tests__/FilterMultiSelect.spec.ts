import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import FilterMultiSelect from '../FilterMultiSelect.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)

describe('native multi-selection filter', () => {
  it('adds and removes individual choices and clears all without a custom popup', async () => {
    const wrapper = mount(FilterMultiSelect, { props: {
      label: 'Platforms', allLabel: 'All platforms', modelValue: [],
      options: [{ value: 'openai', label: 'OpenAI' }, { value: 'gemini', label: 'Gemini' }],
    } })
    const select = wrapper.get('select')
    await select.setValue('option:openai')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai']])
    await wrapper.setProps({ modelValue: ['openai'] })
    await select.setValue('option:gemini')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai', 'gemini']])
    await wrapper.setProps({ modelValue: ['openai', 'gemini'] })
    await select.setValue('option:openai')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['gemini']])
    await select.setValue('all')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
  })
})
