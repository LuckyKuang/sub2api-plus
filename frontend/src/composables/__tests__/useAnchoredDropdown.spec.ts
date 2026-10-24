import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { useAnchoredDropdown } from '../useAnchoredDropdown'

const viewport = { width: window.innerWidth, height: window.innerHeight }
let rect = new DOMRect(100, 700, 150, 34)
const harness = defineComponent({
  props: { active: Boolean },
  setup(props) {
    const anchor = ref<HTMLElement | null>(null)
    const style = useAnchoredDropdown(anchor, () => props.active)
    return { anchor, style }
  },
  template: '<div><button ref="anchor">Trigger</button><div data-testid="menu" :style="style" /></div>',
})
enableAutoUnmount(afterEach)
beforeEach(() => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1024 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, writable: true, value: 768 })
  rect = new DOMRect(100, 700, 150, 34)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => rect)
})
afterEach(() => {
  vi.restoreAllMocks()
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: viewport.width })
  Object.defineProperty(window, 'innerHeight', { configurable: true, writable: true, value: viewport.height })
})

describe('dropdowns in scrolling workspaces', () => {
  it('opens upward near the viewport bottom, follows scrolling, and stays within the right edge', async () => {
    const wrapper = mount(harness, { props: { active: true } })
    await flushPromises()
    const menu = wrapper.get('[data-testid="menu"]').element as HTMLElement
    expect(menu.style.position).toBe('fixed')
    expect(menu.style.bottom).toBe('74px')
    expect(menu.style.maxHeight).toBe('240px')
    rect = new DOMRect(980, 300, 150, 34)
    window.dispatchEvent(new Event('scroll'))
    await wrapper.vm.$nextTick()
    expect(menu.style.left).toBe('772px')
    expect(menu.style.width).toBe('240px')
    expect(menu.style.top).toBe('340px')
    expect(menu.style.bottom).toBe('')
  })

  it('measures only active menus and removes both global listeners on unmount', async () => {
    const measure = vi.mocked(HTMLElement.prototype.getBoundingClientRect)
    const remove = vi.spyOn(window, 'removeEventListener')
    const wrapper = mount(harness, { props: { active: false } })
    window.dispatchEvent(new Event('scroll'))
    expect(measure).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(measure).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('resize', expect.any(Function))
    expect(remove).toHaveBeenCalledWith('scroll', expect.any(Function), true)
    window.dispatchEvent(new Event('scroll'))
    expect(measure).toHaveBeenCalledTimes(1)
  })
})
