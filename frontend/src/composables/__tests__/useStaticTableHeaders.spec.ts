import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { useStaticTableHeaders } from '../useStaticTableHeaders'

// Acceptance: a transparent header occupies a different box from the native
// body scrollport, retains the real interactive Vue nodes, and does no layout
// work when only scrollTop changes. Layout values below are independent fixture
// dimensions; Chrome verifies compositor scrolling against the deployed CSS.
let scope: EffectScope
let nextFrame = 0
const frames = new Map<number, FrameRequestCallback>()
const observers: FakeResizeObserver[] = []

class FakeResizeObserver {
  readonly targets = new Set<Element>()
  constructor(readonly callback: ResizeObserverCallback) { observers.push(this) }
  observe(element: Element) { this.targets.add(element) }
  unobserve(element: Element) { this.targets.delete(element) }
  disconnect() { this.targets.clear() }
  notify(element: Element) { this.callback([{ target: element } as ResizeObserverEntry], this as unknown as ResizeObserver) }
}

function bounds(x: number, y: number, width: number, height: number): DOMRect {
  return { x, y, width, height, top: y, left: x, right: x + width, bottom: y + height, toJSON: () => ({}) } as DOMRect
}

function fixture({ constrained = true, sticky = true, overflowing = true } = {}) {
  const parent = document.createElement('section')
  parent.style.display = 'flex'
  const viewport = document.createElement('div')
  viewport.className = 'record-table-scroll table-wrapper'
  viewport.style.cssText = constrained
    ? 'overflow-y:auto;flex:1 1 0%;max-height:240px;padding-top:8px'
    : 'overflow-y:auto;flex:0 1 auto;max-height:none;padding-top:8px'
  viewport.setAttribute('aria-label', 'Usage records')
  viewport.innerHTML = `<table class="record-table"><thead style="position:${sticky ? 'sticky' : 'static'};top:0"><tr>
    <th data-width="80"><input id="select-all" type="checkbox" aria-label="Select all"></th>
    <th data-width="120" data-field="account" style="left:80px"><button title="Account help">Account</button></th>
    <th data-width="200" data-field="model" aria-sort="none">Model</th>
    <th data-width="200">Cost</th>
    </tr></thead><tbody><tr><td>A</td><td>B</td><td>C</td><td>D</td></tr></tbody></table>`
  Object.defineProperties(viewport, {
    clientLeft: { value: 12 }, clientWidth: { value: 300 },
    clientHeight: { value: constrained ? 240 : 180 }, scrollHeight: { value: constrained && overflowing ? 900 : constrained ? 240 : 180, configurable: true },
  })
  parent.append(viewport)
  document.body.append(parent)
  const table = viewport.querySelector('table')!
  const head = table.tHead!
  return { parent, viewport, table, head, input: head.querySelector('input')!, button: head.querySelector('button')! }
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await nextTick()
    const pending = [...frames.values()]
    frames.clear()
    for (const callback of pending) callback(0)
  }
}

beforeEach(() => {
  scope = effectScope()
  frames.clear()
  observers.length = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    if (this.hidden || this.style.display === 'none') return bounds(0, 0, 0, 0)
    let ancestor: HTMLElement | null = this.parentElement
    while (ancestor) {
      if (ancestor.hidden || ancestor.style.display === 'none') return bounds(0, 0, 0, 0)
      ancestor = ancestor.parentElement
    }
    if (this.matches('.record-table-scroll')) return bounds(100, this.classList.contains('static-table-body-viewport') ? 208 : 160, 324, 240)
    if (this.tagName === 'TH') return bounds(112, this.closest('.static-table-header-rail') ? 160 : 208, Number(this.dataset.width) || 100, 48)
    if (this.tagName === 'THEAD') return bounds(112, 160, 600, 48)
    if (this.tagName === 'TABLE') {
      const head = (this as HTMLTableElement).tHead
      const width = [...(head?.rows[0]?.cells ?? [])].reduce((sum, cell) => sum + (Number(cell.dataset.width) || 100), 0)
      return bounds(112, 160, width, 900)
    }
    if (this.tagName === 'BUTTON') return bounds(192, this.closest('.static-table-header-rail') ? 168 : 208 - (this.closest('.record-table-scroll')?.scrollTop ?? 0), 96, 24)
    return bounds(100, 160, 324, 48)
  })
})

afterEach(() => {
  scope.stop()
  document.body.replaceChildren()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('static table headers', () => {
  it('keeps short bounded price tables native and pins their header only after rows overflow', async () => {
    const { viewport, table, head } = fixture({ overflowing: false })
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    expect(table.tHead).toBe(head)
    expect(document.querySelector('.static-table-header-rail')).toBeNull()
    Object.defineProperty(viewport, 'scrollHeight', { value: 900 })
    observers[0]!.notify(table)
    await settle()
    expect(document.querySelector('.static-table-header-rail thead')).toBe(head)
  })

  it('measures all resized tables before applying any header width changes', async () => {
    const first = fixture()
    const second = fixture()
    scope.run(() => useStaticTableHeaders(ref(first.viewport)))
    await settle()
    const operations: string[] = []
    const read = vi.mocked(HTMLElement.prototype.getBoundingClientRect)
    const originalRead = read.getMockImplementation()!
    read.mockImplementation(function (this: HTMLElement) {
      operations.push('read')
      return originalRead.call(this)
    })
    const originalWrite = CSSStyleDeclaration.prototype.setProperty
    vi.spyOn(CSSStyleDeclaration.prototype, 'setProperty').mockImplementation(function (this: CSSStyleDeclaration, name, value, priority) {
      if (name === 'width') operations.push('write')
      return originalWrite.call(this, name, value, priority)
    })
    first.table.tHead!.rows[0]!.cells[0]!.dataset.width = '90'
    second.table.tHead!.rows[0]!.cells[0]!.dataset.width = '90'
    observers[0]!.notify(first.table)
    observers[0]!.notify(second.table)
    const pending = [...frames.values()]
    frames.clear()
    for (const callback of pending) callback(0)
    expect(operations).toContain('write')
    expect(operations.lastIndexOf('read')).toBeLessThan(operations.indexOf('write'))
  })

  it('places the real header before the unchanged native scroll element and preserves native column associations', async () => {
    const { viewport, table, head, input } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const rail = document.querySelector('.static-table-header-rail')!
    expect(rail.nextElementSibling).toBe(viewport)
    expect(rail.querySelector('thead')).toBe(head)
    expect(rail.querySelector('#select-all')).toBe(input)
    expect(viewport.querySelector('table')).toBe(table)
    expect(table.tHead).not.toBe(head)
    expect(table.tHead!.hasAttribute('aria-hidden')).toBe(false)
    expect(table.tHead!.rows[0]!.cells[0]!.getAttribute('aria-label')).toBe('Select all')
    expect(table.tHead!.rows[0]!.cells[1]!.getAttribute('aria-label')).toBe('Account')
    expect(table.tHead!.querySelector('[id]')).toBeNull()
    expect(table.tHead!.querySelector('input')!.disabled).toBe(true)
    expect(table.style.getPropertyValue('--static-table-head-height')).toBe('48px')
    expect(rail.querySelector('table')!.getAttribute('role')).toBe('presentation')
    expect(table.getAttribute('aria-label')).toBe('Usage records')
    expect([...rail.querySelectorAll('col')].map(col => col.style.width)).toEqual(['80px', '120px', '200px', '200px'])
  })

  it('performs no measurement or header mutations during very fast vertical scrolling', async () => {
    const { viewport, table, head, button } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    expect(frames.size).toBe(0)
    const contents = head.innerHTML
    const measurement = table.tHead
    const read = vi.mocked(HTMLElement.prototype.getBoundingClientRect)
    read.mockClear()
    const anchor = button.getBoundingClientRect().top
    read.mockClear()
    for (let i = 0; i < 100; i++) {
      viewport.scrollTop = i % 2 ? 0 : 660
      viewport.dispatchEvent(new Event('scroll'))
    }
    expect(frames.size).toBe(0)
    expect(read).not.toHaveBeenCalled()
    expect(table.tHead).toBe(measurement)
    expect(head.innerHTML).toBe(contents)
    expect(button.getBoundingClientRect().top).toBe(anchor)
    expect(viewport.contains(button)).toBe(false)
  })

  it('keeps native sorting, selection, hover and focus handlers on the same nodes', async () => {
    const { viewport, head, table, input, button } = fixture()
    const sort = vi.fn(() => head.rows[0]!.cells[2]!.setAttribute('aria-sort', 'descending'))
    const selection = vi.fn()
    const hover = vi.fn()
    const focus = vi.fn()
    head.rows[0]!.cells[2]!.addEventListener('click', sort)
    input.addEventListener('change', selection)
    button.addEventListener('mouseenter', hover)
    button.addEventListener('focus', focus)
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    head.rows[0]!.cells[2]!.click()
    input.click()
    button.dispatchEvent(new MouseEvent('mouseenter'))
    button.focus()
    await settle()
    expect(sort).toHaveBeenCalledOnce()
    expect(selection).toHaveBeenCalledOnce()
    expect(input.checked).toBe(true)
    expect(hover).toHaveBeenCalledOnce()
    expect(focus).toHaveBeenCalledOnce()
    expect(document.activeElement).toBe(button)
    expect(table.tHead!.rows[0]!.cells[2]!.getAttribute('aria-sort')).toBe('descending')
  })

  it('synchronizes horizontal scrolling in both directions without scrolling the header vertically', async () => {
    const { viewport } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const rail = document.querySelector<HTMLElement>('.static-table-header-rail')!
    viewport.scrollLeft = 180
    viewport.dispatchEvent(new Event('scroll'))
    expect(rail.scrollLeft).toBe(180)
    rail.scrollLeft = 240
    rail.dispatchEvent(new Event('scroll'))
    expect(viewport.scrollLeft).toBe(240)
    expect(rail.scrollTop).toBe(0)
    expect(frames.size).toBe(0)
  })

  it('updates translated labels, new columns and sticky offsets and notifies the owner after measurement', async () => {
    const { viewport, table, head } = fixture()
    const measured = vi.fn()
    viewport.addEventListener('static-table-header-measured', measured)
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    measured.mockClear()
    head.rows[0]!.cells[1]!.style.left = '96px'
    head.rows[0]!.cells[2]!.textContent = '模型'
    const cell = document.createElement('th')
    cell.dataset.width = '100'
    cell.textContent = 'Endpoint'
    head.rows[0]!.append(cell)
    await settle()
    expect(table.tHead!.rows[0]!.cells.length).toBe(5)
    expect(table.tHead!.rows[0]!.cells[1]!.style.left).toBe('96px')
    expect(table.tHead!.rows[0]!.cells[2]!.textContent).toBe('模型')
    expect(document.querySelectorAll('.static-table-header-table col').length).toBe(5)
    expect(measured).toHaveBeenCalled()
    expect(frames.size).toBe(0)
  })

  it('remeasures after resize and supports source-root and ancestor v-show without a feedback loop', async () => {
    const { viewport, head, table, parent } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector<HTMLElement>('.static-table-frame')!
    viewport.style.display = 'none'
    await settle()
    expect(frame.hidden).toBe(true)
    expect(frames.size).toBe(0)
    viewport.style.display = ''
    await settle()
    expect(frame.hidden).toBe(false)
    parent.style.display = 'none'
    await settle()
    parent.style.display = 'flex'
    head.rows[0]!.cells[0]!.dataset.width = '96'
    observers[0]!.notify(table)
    await settle()
    expect(document.querySelector<HTMLElement>('.static-table-header-table col')!.style.width).toBe('96px')
    expect(frames.size).toBe(0)
  })

  it('separates compact distribution headers even when the original header is not sticky', async () => {
    // Acceptance: every bounded data list keeps its title outside scrolling
    // records; missing sticky markup must not exempt chart distribution rows.
    const { viewport, head } = fixture({ sticky: false })
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const rail = document.querySelector('.static-table-header-rail')!
    expect(rail.querySelector('thead')).toBe(head)
    expect(rail.nextElementSibling).toBe(viewport)
    expect(viewport.contains(head)).toBe(false)
    expect(viewport.classList.contains('static-table-body-viewport')).toBe(true)
    expect(viewport.parentElement!.style.maxHeight).toBe('240px')
  })

  it('keeps short unconstrained explanation tables in their natural document flow', async () => {
    // A short table in a dialog/page must not acquire an unnecessary private
    // scrolling window just because it uses record-table typography.
    const { viewport, head } = fixture({ constrained: false, sticky: false })
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    expect(document.querySelector('.static-table-frame')).toBeNull()
    expect(viewport.querySelector('thead')).toBe(head)
  })

  it('separates a bounded dialog table without moving other dialog content into its scrollport', async () => {
    const { parent, viewport, head } = fixture({ sticky: false })
    parent.className = 'modal-body'
    const introduction = document.createElement('p')
    introduction.textContent = 'Choose a quota account before reviewing these rows.'
    const footer = document.createElement('button')
    footer.textContent = 'Close'
    parent.prepend(introduction)
    parent.append(footer)
    viewport.classList.add('record-table-dialog-scroll')
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector('.static-table-frame')!
    expect(frame.querySelector('.static-table-header-rail thead')).toBe(head)
    expect(frame.contains(viewport)).toBe(true)
    expect(frame.contains(introduction)).toBe(false)
    expect(frame.contains(footer)).toBe(false)
    expect(introduction.nextElementSibling).toBe(frame)
    expect(frame.nextElementSibling).toBe(footer)
  })

  it('preserves an inline viewport-relative height budget when transferring it to the header frame', async () => {
    // A resize must continue to resolve vh through CSS, not keep the pixel
    // height measured at the original browser window size.
    const { viewport } = fixture({ sticky: false })
    viewport.style.maxHeight = 'min(42vh, 420px)'
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector<HTMLElement>('.static-table-frame')!
    expect(frame.style.maxHeight).toBe('min(42vh, 420px)')
    window.dispatchEvent(new Event('resize'))
    await settle()
    expect(frame.style.maxHeight).toBe('min(42vh, 420px)')
    expect(frames.size).toBe(0)
  })

  it('refreshes an authored height budget when a responsive breakpoint changes its padding allowance', async () => {
    const { viewport } = fixture({ sticky: false })
    viewport.style.removeProperty('max-height')
    viewport.style.setProperty('--record-table-max-height', 'max(0px, calc(min(52vh, 520px) - 2rem))')
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector<HTMLElement>('.static-table-frame')!
    expect(frame.style.maxHeight).toBe('max(0px, calc(min(52vh, 520px) - 2rem))')
    viewport.style.setProperty('--record-table-max-height', 'max(0px, calc(min(52vh, 520px) - 2.5rem))')
    window.dispatchEvent(new Event('resize'))
    await settle()
    expect(frame.style.maxHeight).toBe('max(0px, calc(min(52vh, 520px) - 2.5rem))')
    expect(frames.size).toBe(0)
  })

  it('follows dynamic inline height changes and resumes the stylesheet budget when the override is cleared', async () => {
    // A caller may temporarily constrain a table and then restore responsive
    // sizing. The split shell must follow that change without keeping stale px.
    const { viewport } = fixture({ sticky: false })
    viewport.style.setProperty('--record-table-max-height', 'min(42vh, 420px)')
    viewport.style.maxHeight = 'min(30vh, 300px)'
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector<HTMLElement>('.static-table-frame')!
    expect(frame.style.maxHeight).toBe('min(30vh, 300px)')
    viewport.style.maxHeight = 'min(35vh, 350px)'
    await settle()
    expect(frame.style.maxHeight).toBe('min(35vh, 350px)')
    viewport.style.removeProperty('max-height')
    await settle()
    expect(frame.style.maxHeight).toBe('min(42vh, 420px)')
    expect(frames.size).toBe(0)
  })

  it('registers an initially hidden viewport when it becomes visible', async () => {
    const { viewport, head } = fixture()
    viewport.style.display = 'none'
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    expect(document.querySelector('.static-table-frame')).toBeNull()
    viewport.style.display = ''
    await settle()
    expect(document.querySelector('.static-table-header-rail thead')).toBe(head)
    expect(frames.size).toBe(0)
  })

  it('preserves leaf column widths for grouped headers with rowspans', async () => {
    const { viewport, head } = fixture()
    head.innerHTML = '<tr><th rowspan="2" data-width="80">Account</th><th colspan="3" data-width="520">Usage</th></tr><tr><th data-width="120">Input</th><th data-width="200">Output</th><th data-width="200">Cost</th></tr>'
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    expect([...document.querySelectorAll<HTMLElement>('.static-table-header-table col')].map(col => col.style.width)).toEqual(['80px', '120px', '200px', '200px'])
  })

  it('does not reinsert an obsolete real header when Vue replaces the table header', async () => {
    const { viewport, table, head } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const replacement = document.createElement('thead')
    replacement.style.cssText = 'position:sticky;top:0'
    replacement.innerHTML = '<tr><th data-width="600">Replacement</th></tr>'
    table.append(replacement)
    await settle()
    expect(document.querySelector('.static-table-header-rail thead')).toBe(replacement)
    expect(document.body.contains(head)).toBe(false)
    expect(table.querySelectorAll('thead')).toHaveLength(1)
  })

  it('discovers a returning header after Vue removes it without a table resize', async () => {
    const { viewport, table, head } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    head.remove()
    await settle()
    expect(document.querySelector('.static-table-header-rail')).toBeNull()
    expect(table.tHead).toBeNull()
    const replacement = document.createElement('thead')
    replacement.style.cssText = 'position:sticky;top:0'
    replacement.innerHTML = '<tr><th data-width="600">Returned header</th></tr>'
    table.prepend(replacement)
    await settle()
    expect(document.querySelector('.static-table-header-rail thead')).toBe(replacement)
    expect(table.tHead!.classList.contains('static-table-measure-head')).toBe(true)
    expect(frames.size).toBe(0)
  })

  it('restores the original Vue DOM on disposal and removes shells when a conditional viewport disappears', async () => {
    const first = fixture()
    scope.run(() => useStaticTableHeaders(ref(first.viewport)))
    await settle()
    scope.stop()
    expect(first.table.tHead).toBe(first.head)
    expect(first.viewport.parentElement).toBe(first.parent)
    expect(first.table.classList.contains('static-table-source')).toBe(false)
    expect(first.table.style.getPropertyValue('--static-table-head-height')).toBe('')
    expect(document.querySelector('.static-table-frame')).toBeNull()
    expect(observers.every(observer => observer.targets.size === 0)).toBe(true)
    scope = effectScope()
    const second = fixture()
    scope.run(() => useStaticTableHeaders(ref(second.viewport)))
    await settle()
    first.viewport.remove()
    second.viewport.remove()
    await settle()
    expect(document.querySelector('.static-table-frame')).toBeNull()
    expect(second.head.parentElement).toBe(second.table)
  })

  it('preserves the replacement Vue root when desktop tables switch to mobile cards', async () => {
    const { viewport, parent } = fixture()
    scope.run(() => useStaticTableHeaders(ref(viewport)))
    await settle()
    const frame = document.querySelector('.static-table-frame')!
    const mobileCards = document.createElement('div')
    mobileCards.textContent = 'Mobile request cards'
    // Vue uses the previous root's current parent as its insertion container.
    frame.insertBefore(mobileCards, viewport)
    viewport.remove()
    await settle()
    expect(mobileCards.parentElement).toBe(parent)
    expect(mobileCards.isConnected).toBe(true)
    expect(document.querySelector('.static-table-frame')).toBeNull()
    expect(observers[0]!.targets.size).toBe(0)
  })
})
