import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Toggle from '../Toggle.vue'
import ToggleSwitch from '@/components/payment/ToggleSwitch.vue'
import QuotaNotifyToggle from '@/components/account/QuotaNotifyToggle.vue'

describe('Toggle', () => {
  it('emits one update and waits for the owner to accept the new state', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false }, attrs: { 'aria-label': 'Enable' } })
    const button = wrapper.get('button')
    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    expect(button.attributes('aria-checked')).toBe('false')
    await wrapper.setProps({ modelValue: true })
    expect(button.attributes('aria-checked')).toBe('true')
    expect(button.attributes('aria-label')).toBe('Enable')
    expect(button.attributes('type')).toBe('button')
    wrapper.unmount()
  })

  it('does not update while disabled', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: true, disabled: true } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('button').attributes('aria-disabled')).toBe('true')
    wrapper.unmount()
  })

  it('preserves the payment wrapper toggle event without duplicating it', async () => {
    const wrapper = mount(ToggleSwitch, { props: { label: 'For sale', checked: false } })
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('treats an unset quota notification as off and emits the requested enabled state', async () => {
    const wrapper = mount(QuotaNotifyToggle, {
      props: { enabled: null, threshold: null, thresholdType: null },
    })
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    wrapper.unmount()
  })
})
