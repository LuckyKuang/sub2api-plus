import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

enableAutoUnmount(afterEach)

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })

  // User requirement / docs/UI_INTERACTION.md: long titles stay readable
  // without moving the close button, body or footer outside the viewport.
  // Real geometry is covered by test:layout; this checks the keyboard contract.
  it('keeps an ordinary title outside the tab order and retains close-button initial focus', async () => {
    mount(BaseDialog, {
      attachTo: document.body,
      props: { show: true, title: 'Details' },
      global: { stubs: { Icon: true, Transition: true } },
    })
    await flushPromises()
    const title = document.body.querySelector<HTMLElement>('.modal-title')!
    expect(title.hasAttribute('tabindex')).toBe(false)
    expect(document.activeElement).toBe(document.body.querySelector('[aria-label="Close modal"]'))
  })

  it('adds only an overflowing title to the tab order and removes it when the title becomes short', async () => {
    let contentHeight = 240
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('modal-title') ? 100 : 0
    })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('modal-title') ? contentHeight : 0
    })
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: true, title: 'An exceptionally long translated title '.repeat(20) },
      global: { stubs: { Icon: true, Transition: true } },
    })
    await flushPromises()
    const title = document.body.querySelector<HTMLElement>('.modal-title')!
    expect(title.getAttribute('tabindex')).toBe('0')
    title.focus()
    expect(document.activeElement).toBe(title)
    contentHeight = 28
    await wrapper.setProps({ title: 'Details' })
    await flushPromises()
    expect(title.hasAttribute('tabindex')).toBe(false)
    expect(title.textContent?.trim()).toBe('Details')
  })

  it('updates title keyboard access after its available height changes', async () => {
    const resizeCallbacks: ResizeObserverCallback[] = []
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { resizeCallbacks.push(callback) }
      observe() {}
      unobserve() {}
      disconnect() {}
    })
    let availableHeight = 240
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('modal-title') ? availableHeight : 0
    })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('modal-title') ? 180 : 0
    })
    mount(BaseDialog, {
      attachTo: document.body,
      props: { show: true, title: 'A translated title that wraps in a narrow window' },
      global: { stubs: { Icon: true, Transition: true } },
    })
    await flushPromises()
    const title = document.body.querySelector<HTMLElement>('.modal-title')!
    expect(title.hasAttribute('tabindex')).toBe(false)
    expect(resizeCallbacks.length).toBeGreaterThan(0)
    availableHeight = 100
    for (const callback of resizeCallbacks) callback([], {} as ResizeObserver)
    await nextTick()
    expect(title.getAttribute('tabindex')).toBe('0')
    availableHeight = 240
    for (const callback of resizeCallbacks) callback([], {} as ResizeObserver)
    await nextTick()
    expect(title.hasAttribute('tabindex')).toBe(false)
  })
})
