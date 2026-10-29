import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserCardScroll from '../UserCardScroll.vue'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('aligned user card scroll area', () => {
  // Approved review: the scrollbar sits outside the card column, so its
  // measured width must be added without shrinking the cards under the header.
  it('tracks classic and overlay scrollbar widths and releases its resize observer', async () => {
    let callback: ResizeObserverCallback | undefined
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      constructor(value: ResizeObserverCallback) { callback = value }
      observe() {}
      disconnect = disconnect
    })
    let clientWidth = 1600
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(1608)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => clientWidth)
    const wrapper = mount(UserCardScroll, { props: { label: 'Subscription cards', resetKey: 1 }, slots: { default: '<article>Real subscription</article>' } })
    await wrapper.vm.$nextTick()
    const viewport = wrapper.get('.user-card-scroll-viewport')
    expect((viewport.element as HTMLElement).style.getPropertyValue('--user-card-scrollbar')).toBe('8px')
    expect(viewport.attributes('aria-label')).toBe('Subscription cards')
    clientWidth = 1608
    callback?.([], {} as ResizeObserver)
    await wrapper.vm.$nextTick()
    expect((viewport.element as HTMLElement).style.getPropertyValue('--user-card-scrollbar')).toBe('0px')
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('returns to the top only when the displayed page changes', async () => {
    const wrapper = mount(UserCardScroll, { props: { resetKey: 1 } })
    const viewport = wrapper.get('.user-card-scroll-viewport').element as HTMLElement
    viewport.scrollTop = 240
    await wrapper.setProps({ resetKey: 1 })
    expect(viewport.scrollTop).toBe(240)
    await wrapper.setProps({ resetKey: 2 })
    expect(viewport.scrollTop).toBe(0)
    wrapper.unmount()
  })
})
