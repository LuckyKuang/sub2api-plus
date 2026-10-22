import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { useDialogFocus } from '../useDialogFocus'
import BaseDialog from '@/components/common/BaseDialog.vue'

// Approved glass-console requirements: drawer/dialog focus containment, return
// focus, and a shared scroll lock that survives overlapping surfaces.
enableAutoUnmount(afterEach)
const Drawer = defineComponent({
  props: { show: Boolean, empty: Boolean }, emits: ['close'],
  setup(props, { emit }) {
    const panel = ref<HTMLElement | null>(null)
    useDialogFocus(() => props.show, panel, () => emit('close'))
    return () => props.show ? h('aside', { ref: panel, tabindex: -1 }, props.empty ? [] : [
      h('button', { 'data-first': '' }, 'First'), h('button', { disabled: true }, 'Disabled'),
      h('button', { 'data-last': '' }, 'Last')
    ]) : null
  }
})
const key = (value: string, shiftKey = false) => document.dispatchEvent(new KeyboardEvent('keydown', { key: value, shiftKey, cancelable: true }))

describe('modal navigation and dialog ownership', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{ width: 44, height: 44 }] as unknown as DOMRectList)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })
  it('moves focus into the drawer, wraps both directions, and returns to the opener', async () => {
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const wrapper = mount(Drawer, { attachTo: document.body, props: { show: false } })
    await wrapper.setProps({ show: true })
    await nextTick()
    const first = wrapper.get('[data-first]').element as HTMLElement
    const last = wrapper.get('[data-last]').element as HTMLElement
    expect(document.activeElement).toBe(first)
    key('Tab', true)
    expect(document.activeElement).toBe(last)
    key('Tab')
    expect(document.activeElement).toBe(first)
    await wrapper.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(opener)
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('shares Escape ownership and scroll locking with an actual BaseDialog', async () => {
    const drawer = mount(Drawer, { attachTo: document.body, props: { show: true } })
    await nextTick()
    const dialog = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Details' } })
    await nextTick()
    key('Escape')
    expect(dialog.emitted('close')).toHaveLength(1)
    expect(drawer.emitted('close')).toBeUndefined()
    await dialog.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(true)
    key('Escape')
    expect(drawer.emitted('close')).toHaveLength(1)
    await drawer.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('keeps focus and scrolling with the upper surface when a lower surface closes', async () => {
    const drawer = mount(Drawer, { attachTo: document.body, props: { show: true } })
    await nextTick()
    const upper = mount(Drawer, { attachTo: document.body, props: { show: true } })
    await nextTick()
    const active = document.activeElement
    await drawer.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(active)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    upper.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('contains Tab even when a panel has no interactive content', async () => {
    const wrapper = mount(Drawer, { attachTo: document.body, props: { show: true, empty: true } })
    await nextTick()
    expect(document.activeElement).toBe(wrapper.element)
    key('Tab')
    expect(document.activeElement).toBe(wrapper.element)
  })
  it('does not focus a surface that was closed before its opening tick completed', async () => {
    const wrapper = mount(Drawer, { attachTo: document.body, props: { show: false } })
    const open = wrapper.setProps({ show: true })
    const close = wrapper.setProps({ show: false })
    await Promise.all([open, close])
    await nextTick()
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
})
