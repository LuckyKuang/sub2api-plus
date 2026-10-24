import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import TitleTooltip from '../TitleTooltip.vue'

// User requirement: legacy hover labels must render as themed cards, remain
// keyboard accessible, and stay within the viewport without changing actions.
let wrapper: VueWrapper | undefined

function trigger(title = 'Show details') {
  const button = document.createElement('button')
  button.title = title
  button.setAttribute('aria-describedby', 'existing-description')
  button.innerHTML = '<span>Details</span>'
  button.getBoundingClientRect = () => ({ x: 350, y: 2, left: 350, top: 2, right: 380, bottom: 34, width: 30, height: 32, toJSON: () => ({}) })
  button.firstElementChild!.getBoundingClientRect = () => button.getBoundingClientRect()
  document.body.append(button)
  return button
}
function pointer(element: Element, type: string, relatedTarget: EventTarget | null = null) {
  element.dispatchEvent(new MouseEvent(type, { bubbles: true, relatedTarget }))
}
function tooltip() { return document.querySelector('[role="tooltip"]') }
async function start() {
  wrapper = mount(TitleTooltip, { attachTo: document.body })
  await nextTick()
}
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('title card tooltips', () => {
  it('shows the nearest nested title without leaving a native child tooltip behind', async () => {
    await start()
    const button = trigger('Group details')
    const child = button.firstElementChild!
    child.setAttribute('title', 'Peak rate details')
    pointer(button, 'pointerover')
    await nextTick()
    expect(tooltip()?.textContent).toBe('Group details')
    pointer(button, 'pointerout', child)
    pointer(child, 'pointerover')
    await nextTick()
    expect(tooltip()?.textContent).toBe('Peak rate details')
    expect(child.getAttribute('title')).toBe('')
    expect(button.title).toBe('Group details')
  })

  it('hands over to a nested shared help tooltip without displaying duplicate cards', async () => {
    await start()
    const button = trigger('Parent details')
    const help = button.firstElementChild!
    help.className = 'help-tooltip-trigger'
    help.setAttribute('title', '')
    pointer(button, 'pointerover')
    await nextTick()
    expect(tooltip()?.textContent).toBe('Parent details')
    pointer(help, 'pointerover')
    await nextTick()
    expect(tooltip()).toBeNull()
    expect(button.title).toBe('Parent details')
  })

  it('shows a card for dynamically added titles and restores native metadata when leaving', async () => {
    await start()
    const button = trigger()
    pointer(button, 'pointerover')
    await nextTick()
    expect(tooltip()?.textContent).toBe('Show details')
    expect(button.title).toBe('')
    expect(button.getAttribute('aria-describedby')?.split(' ')).toContain(tooltip()?.id)
    pointer(button, 'pointerout')
    await nextTick()
    expect(tooltip()).toBeNull()
    expect(button.title).toBe('Show details')
    expect(button.getAttribute('aria-describedby')).toBe('existing-description')
  })

  it('keeps the tooltip open when moving through children and closes on Escape without consuming the key', async () => {
    await start()
    const button = trigger()
    pointer(button, 'pointerover')
    await nextTick()
    pointer(button, 'pointerout', button.firstElementChild)
    await nextTick()
    expect(tooltip()).not.toBeNull()
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    button.dispatchEvent(escape)
    await nextTick()
    expect(tooltip()).toBeNull()
    expect(escape.defaultPrevented).toBe(false)
    expect(button.title).toBe('')
    window.dispatchEvent(new Event('scroll'))
    expect(button.title).toBe('')
    pointer(button.firstElementChild!, 'pointerover')
    await nextTick()
    expect(tooltip()).toBeNull()
    pointer(button, 'pointerout')
    pointer(button, 'pointerover')
    await nextTick()
    expect(tooltip()).not.toBeNull()
  })

  it('supports focus without hover and preserves the accessible name of icon buttons', async () => {
    await start()
    const button = trigger()
    button.textContent = ''
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    await nextTick()
    expect(tooltip()?.textContent).toBe('Show details')
    expect(button.getAttribute('aria-label')).toBe('Show details')
    button.dispatchEvent(new FocusEvent('focusout', { bubbles: true }))
    await nextTick()
    expect(tooltip()).toBeNull()
    expect(button.hasAttribute('aria-label')).toBe(false)
  })

  it('uses text content for untrusted titles and keeps button clicks working', async () => {
    await start()
    const button = trigger('<img src=x onerror=alert(1)>')
    const clicked = vi.fn()
    button.addEventListener('click', clicked)
    pointer(button, 'pointerover')
    await nextTick()
    expect(tooltip()?.querySelector('img')).toBeNull()
    expect(tooltip()?.textContent).toBe('<img src=x onerror=alert(1)>')
    button.click()
    await nextTick()
    expect(clicked).toHaveBeenCalledOnce()
    expect(tooltip()).toBeNull()
  })

  it('keeps the tooltip inside a phone viewport and uses viewport coordinates after scrolling', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    vi.stubGlobal('scrollY', 1000)
    await start()
    const button = trigger()
    const nativeRect = HTMLElement.prototype.getBoundingClientRect
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if (this.getAttribute('role') === 'tooltip') return { x: 0, y: 0, left: 0, top: 0, right: 200, bottom: 60, width: 200, height: 60, toJSON: () => ({}) }
      return nativeRect.call(this)
    })
    pointer(button, 'pointerover')
    await nextTick()
    await nextTick()
    const panel = tooltip() as HTMLElement
    expect(panel.style.left).toBe('178px')
    expect(panel.style.top).toBe('42px')
  })

  it('restores metadata on route unmount and reflects title updates while open', async () => {
    await start()
    const button = trigger()
    pointer(button, 'pointerover')
    await nextTick()
    button.title = 'Updated details'
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(tooltip()?.textContent).toBe('Updated details')
    expect(button.title).toBe('')
    wrapper?.unmount()
    wrapper = undefined
    expect(button.title).toBe('Updated details')
    expect(button.getAttribute('aria-describedby')).toBe('existing-description')
  })

  it('keeps collapsed navigation named and respects removal of a reactive title', async () => {
    await start()
    const button = trigger('Accounts')
    button.innerHTML = '<span aria-hidden="true">Accounts</span>'
    pointer(button, 'pointerover')
    await nextTick()
    expect(button.getAttribute('aria-label')).toBe('Accounts')
    button.removeAttribute('title')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(tooltip()).toBeNull()
    pointer(button, 'pointerout')
    await nextTick()
    expect(button.hasAttribute('title')).toBe(false)
    expect(button.hasAttribute('aria-label')).toBe(false)
  })

  it('closes when a virtual row or menu trigger is removed without a route change', async () => {
    await start()
    const button = trigger()
    pointer(button, 'pointerover')
    await nextTick()
    button.remove()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(tooltip()).toBeNull()
    expect(button.title).toBe('Show details')
  })
})
