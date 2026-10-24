import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { useTransparentTableClipping } from '../useTransparentTableClipping'

// Audited appearance requirement: transparent sticky headers/columns must never
// show scrolling cell text underneath them. Clipping may only affect paint;
// native row geometry, virtual spacer heights and original styles stay intact.
let scope: EffectScope
let nextFrame = 0
const frames = new Map<number, FrameRequestCallback>()
const resizeObservers: FakeResizeObserver[] = []

class FakeResizeObserver {
  readonly targets = new Set<Element>()
  constructor(readonly callback: ResizeObserverCallback) { resizeObservers.push(this) }
  observe(element: Element) { this.targets.add(element) }
  unobserve(element: Element) { this.targets.delete(element) }
  disconnect() { this.targets.clear() }
  notify(element: Element) { this.callback([{ target: element } as ResizeObserverEntry], this as unknown as ResizeObserver) }
}

function rect(element: Element, x: number, y: number, width: number, height: number) {
  return vi.spyOn(element, 'getBoundingClientRect').mockReturnValue({
    x, y, width, height, top: y, left: x, right: x + width, bottom: y + height,
    toJSON: () => ({}),
  })
}

function fixture() {
  const wrapper = document.createElement('div')
  wrapper.className = 'record-table-scroll'
  wrapper.innerHTML = `<table class="record-table"><thead style="position:sticky;top:0"><tr>
    <th class="sticky-col-left">User</th><th class="sticky-col-left">Account</th><th>Model</th><th class="sticky-col-right">Actions</th>
    </tr></thead><tbody><tr><td class="sticky-col-left">User</td><td class="sticky-col-left">Account</td><td>Scrolling model text</td><td class="sticky-col-right">Actions</td></tr>
    <tr aria-hidden="true"><td colspan="4" style="height:300px"></td></tr></tbody></table>`
  document.body.append(wrapper)
  Object.defineProperties(wrapper, {
    clientLeft: { value: 12 }, clientTop: { value: 0 },
    clientWidth: { value: 360 }, clientHeight: { value: 200 },
  })
  const table = wrapper.querySelector('table')!
  const header = table.tHead!
  const row = table.tBodies[0]!.rows[0]!
  rect(wrapper, 100, 100, 400, 200)
  rect(table, 112, 100, 600, 500)
  rect(header, 112, 100, 600, 40)
  rect(header.rows[0]!, 112, 100, 600, 40)
  rect(row, 112, 125, 600, 50)
  const columns = [[112, 50], [162, 80], [210, 250], [432, 40]] as const
  for (const [index, [x, width]] of columns.entries()) {
    rect(header.rows[0]!.cells[index]!, x, 100, width, 40)
    rect(row.cells[index]!, x, 125, width, 50)
  }
  return { wrapper, table, header, row, middle: row.cells[2]! }
}

function flushFrames() {
  const pending = [...frames.values()]
  frames.clear()
  for (const callback of pending) callback(0)
}

beforeEach(() => {
  scope = effectScope()
  frames.clear()
  resizeObservers.length = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal('ResizeObserver', FakeResizeObserver)
})

afterEach(() => {
  scope.stop()
  document.body.replaceChildren()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('transparent table clipping', () => {
  it('clips scrolled cells at both pinned columns and the sticky header without changing row geometry', async () => {
    const { wrapper, row, middle, header, table } = fixture()
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
    expect(row.cells[0]!.style.clipPath).toBe('inset(15px 0px 0px 0px)')
    expect(row.cells[1]!.style.clipPath).toBe('inset(15px 0px 0px 0px)')
    expect(row.cells[3]!.style.clipPath).toBe('inset(15px 0px 0px 0px)')
    expect(header.rows[0]!.cells[2]!.style.clipPath).toBe('inset(0px 28px 0px 32px)')
    expect(row.getBoundingClientRect().height).toBe(50)
    expect(table.querySelector<HTMLElement>('[aria-hidden] td')!.style.height).toBe('300px')
    expect(table.querySelector<HTMLElement>('[aria-hidden] td')!.style.clipPath).toBe('')
  })

  it('restores pre-existing clip styles and detaches tracking at zero transparency or disposal', async () => {
    const { wrapper, middle } = fixture()
    middle.style.setProperty('clip-path', 'inset(1px)', 'important')
    const transparent = ref(true)
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => transparent.value))
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
    transparent.value = false
    await nextTick()
    expect(middle.style.clipPath).toBe('inset(1px)')
    expect(middle.style.getPropertyPriority('clip-path')).toBe('important')
    expect(resizeObservers.every(observer => observer.targets.size === 0)).toBe(true)
    wrapper.dispatchEvent(new Event('scroll'))
    expect(frames.size).toBe(0)
    transparent.value = true
    await nextTick()
    flushFrames()
    scope.stop()
    expect(middle.style.clipPath).toBe('inset(1px)')
    expect(middle.style.getPropertyPriority('clip-path')).toBe('important')
  })

  it('updates only the scrolled viewport and registers SPA/teleported tables incrementally', async () => {
    const first = fixture()
    scope.run(() => useTransparentTableClipping(ref(first.wrapper), () => true))
    await nextTick()
    flushFrames()
    const second = fixture()
    await nextTick()
    flushFrames()
    expect(second.middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
    const otherReads = vi.mocked(second.wrapper.getBoundingClientRect)
    otherReads.mockClear()
    rect(first.middle, 250, 150, 150, 50)
    first.wrapper.dispatchEvent(new Event('scroll'))
    first.wrapper.dispatchEvent(new Event('scroll'))
    expect(frames.size).toBe(1)
    flushFrames()
    expect(first.middle.style.clipPath).toBe('')
    expect(otherReads).not.toHaveBeenCalled()
    second.wrapper.remove()
    await nextTick()
    expect(second.middle.style.clipPath).toBe('')
  })

  it('rechecks header geometry after resize and keeps fully covered cells invisible', async () => {
    const { wrapper, middle, header } = fixture()
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    rect(middle, 112, 150, 80, 50)
    resizeObservers[0]!.notify(wrapper)
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(50%)')
    rect(header.rows[0]!.cells[1]!, 162, 100, 20, 40)
    rect(middle, 170, 150, 80, 50)
    // Individual header widths can change while total table size stays fixed.
    expect(resizeObservers[0]!.targets.has(header.rows[0]!.cells[1]!)).toBe(true)
    resizeObservers[0]!.notify(header.rows[0]!.cells[1]!)
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(0px 0px 0px 12px)')
  })

  it('uses painted sticky cells when the thead itself scrolls away', async () => {
    const { wrapper, middle, header } = fixture()
    header.style.position = 'static'
    rect(header, 112, 20, 600, 40)
    for (const cell of header.rows[0]!.cells) {
      cell.style.position = 'sticky'
      cell.style.top = '0px'
    }
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
  })

  it('keeps a visible rowspan clipped when its originating row has scrolled out', async () => {
    const { wrapper, row } = fixture()
    row.cells[0]!.rowSpan = 2
    rect(row, 112, 50, 600, 20)
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    expect(row.cells[0]!.style.clipPath).toBe('inset(15px 0px 0px 0px)')
  })

  it('reclips on virtual spacer-only changes without observing ordinary cell styles', async () => {
    const { wrapper, middle, table } = fixture()
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    rect(middle, 250, 150, 150, 50)
    table.querySelector<HTMLElement>('[aria-hidden] td')!.style.height = '280px'
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('')
    await nextTick()
    expect(frames.size).toBe(0)
  })

  it('owns each table only once inside nested viewports and restores its original style', async () => {
    const { wrapper, middle } = fixture()
    const outer = document.createElement('div')
    outer.className = 'workspace-table-viewport'
    wrapper.replaceWith(outer)
    outer.append(wrapper)
    middle.style.clipPath = 'inset(2px)'
    scope.run(() => useTransparentTableClipping(ref(outer), () => true))
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
    scope.stop()
    expect(middle.style.clipPath).toBe('inset(2px)')
  })

  it('rechecks tables when v-show makes their viewport visible again', async () => {
    const { wrapper, middle } = fixture()
    const wrapperRect = rect(wrapper, 0, 0, 0, 0)
    scope.run(() => useTransparentTableClipping(ref(wrapper), () => true))
    await nextTick()
    flushFrames()
    expect(middle.style.clipPath).toBe('')
    wrapperRect.mockReturnValue({ x: 100, y: 100, left: 100, top: 100, right: 500, bottom: 300, width: 400, height: 200, toJSON: () => ({}) })
    resizeObservers[0]!.notify(wrapper)
    flushFrames()
    expect(middle.style.clipPath).toBe('inset(15px 28px 0px 32px)')
  })
})
