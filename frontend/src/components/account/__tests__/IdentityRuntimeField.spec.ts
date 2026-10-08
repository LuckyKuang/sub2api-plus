import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import IdentityRuntimeField from '../IdentityRuntimeField.vue'
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))

describe('identity language and timezone selection', () => {
  it.each([
    ['X-Client-Timezone', 'Europe/Amsterdam'],
    ['X-Client-Language', 'zh-CN'],
    ['timezone', 'Asia/Shanghai']
  ])('selects %s inside the searchable dropdown', async (name, value) => {
    const wrapper = mount(IdentityRuntimeField, { props: { name, modelValue: '', fallback: 'UTC' }, global: { stubs: { teleport: true, transition: false } } })
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await wrapper.get('input').setValue(value)
    await flushPromises()
    const option = wrapper.findAll('[role="option"]').find(item => item.text().includes(value))!
    await option.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([value])
    wrapper.unmount()
  })

  it('preserves a saved language outside the common choices and supports inheritance', async () => {
    const wrapper = mount(IdentityRuntimeField, { props: { name: 'X-Client-Language', modelValue: 'fr-CA', fallback: 'en-US' }, global: { stubs: { teleport: true, transition: false } } })
    expect(wrapper.get('button').text()).toContain('fr-CA')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    await wrapper.findAll('[role="option"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])
    wrapper.unmount()
  })
})
