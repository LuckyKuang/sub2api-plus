import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { supportReadonly } from '../supportReadonly'
import { setAdminSupportContext } from '@/utils/adminSupportContext'

const component = defineComponent({ directives: { supportReadonly }, props: ['mutate', 'copy'], template: '<div><form v-support-readonly @submit.prevent="mutate"><input value="existing configuration"><button type="submit">Save</button></form><button @click="copy">Copy key</button></div>' })
afterEach(() => setAdminSupportContext(null))
describe('assistance preserves reading and disables writes', () => {
  it('retains values and copy actions while blocking both mouse and form submission', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const mutate = vi.fn(), copy = vi.fn()
    const wrapper = mount(component, { props: { mutate, copy } })
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('existing configuration')
    expect(wrapper.get('input').attributes()).toHaveProperty('readonly')
    expect(wrapper.get('button[type=submit]').attributes()).toHaveProperty('disabled')
    await wrapper.get('form').trigger('submit')
    await wrapper.findAll('button')[1].trigger('click')
    expect(mutate).not.toHaveBeenCalled()
    expect(copy).toHaveBeenCalledTimes(1)
    // Vue updates may remove a disabled attribute; the directive must reapply it.
    wrapper.get('button[type=submit]').element.removeAttribute('disabled')
    await nextTick(); await nextTick()
    expect(wrapper.get('button[type=submit]').attributes()).toHaveProperty('disabled')
    wrapper.unmount()
  })
  it('restores editable controls after assistance ends', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const mutate = vi.fn()
    const wrapper = mount(component, { props: { mutate, copy: vi.fn() } })
    setAdminSupportContext(null)
    await nextTick()
    expect(wrapper.get('input').attributes()).not.toHaveProperty('readonly')
    expect(wrapper.get('button[type=submit]').attributes()).not.toHaveProperty('disabled')
    await wrapper.get('form').trigger('submit')
    expect(mutate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('keeps normal-user mutation behavior', async () => {
    const mutate = vi.fn()
    const wrapper = mount(component, { props: { mutate, copy: vi.fn() } })
    expect(wrapper.get('input').attributes()).not.toHaveProperty('readonly')
    await wrapper.get('form').trigger('submit')
    expect(mutate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
