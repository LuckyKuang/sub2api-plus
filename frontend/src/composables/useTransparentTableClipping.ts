import { onScopeDispose, watch, type Ref } from 'vue'

const viewportSelector = '.record-table-scroll, .workspace-table-viewport, .static-table-header-rail'
const tableSelector = 'table.record-table, table.static-table-header-table'

interface OriginalClip {
  value: string
  priority: string
}

interface TableViewport {
  element: HTMLElement
  table: HTMLTableElement | null
  headers: Set<HTMLTableCellElement>
  clips: Map<HTMLElement, OriginalClip>
  scrollLeft: number
}

/** Keep horizontally scrolling cells out of transparent frozen columns.
 * Vertical separation belongs to the static header and native body scrollport;
 * never update vertical cell clips in response to scrolling. */
export function useTransparentTableClipping(root: Ref<HTMLElement | null>, enabled: () => boolean): void {
  let stopTracking: (() => void) | undefined

  const stopWatch = watch([root, enabled], ([element, active]) => {
    stopTracking?.()
    stopTracking = undefined
    if (!element || !active) return

    const viewports = new Map<HTMLElement, TableViewport>()
    const observedTables = new Map<Element, TableViewport>()
    const dirty = new Set<TableViewport>()
    let frame = 0
    let disposed = false

    function restoreCell(state: TableViewport, cell: HTMLElement): void {
      const original = state.clips.get(cell)
      if (!original) return
      if (original.value) cell.style.setProperty('clip-path', original.value, original.priority)
      else cell.style.removeProperty('clip-path')
      state.clips.delete(cell)
    }

    function restore(state: TableViewport): void {
      for (const cell of state.clips.keys()) restoreCell(state, cell)
    }

    function unregister(state: TableViewport): void {
      restore(state)
      resize?.unobserve(state.element)
      if (state.table) {
        resize?.unobserve(state.table)
        observedTables.delete(state.table)
      }
      for (const header of state.headers) {
        resize?.unobserve(header)
        observedTables.delete(header)
      }
      dirty.delete(state)
      viewports.delete(state.element)
    }

    function syncTable(state: TableViewport): void {
      const table = [...state.element.querySelectorAll<HTMLTableElement>(tableSelector)]
        .find(candidate => candidate.closest(viewportSelector) === state.element) ?? null
      if (table !== state.table) {
        restore(state)
        if (state.table) {
          resize?.unobserve(state.table)
          observedTables.delete(state.table)
        }
        state.table = table
        if (table) {
          resize?.observe(table)
          observedTables.set(table, state)
        }
      }
      const headers = new Set(table?.querySelectorAll<HTMLTableCellElement>(':scope > thead > tr > :is(th,td)'))
      for (const header of state.headers) if (!headers.has(header)) {
        resize?.unobserve(header)
        observedTables.delete(header)
      }
      for (const header of headers) if (!state.headers.has(header)) {
        resize?.observe(header)
        observedTables.set(header, state)
      }
      state.headers = headers
    }

    function schedule(state?: TableViewport): void {
      if (disposed) return
      if (state) dirty.add(state)
      else for (const viewport of viewports.values()) dirty.add(viewport)
      if (!frame && dirty.size) frame = requestAnimationFrame(update)
    }

    function register(element: HTMLElement): void {
      if (viewports.has(element)) return
      const state: TableViewport = { element, table: null, headers: new Set(), clips: new Map(), scrollLeft: element.scrollLeft }
      viewports.set(element, state)
      resize?.observe(element)
      syncTable(state)
      schedule(state)
    }

    function registerTree(node: Element): void {
      if (node instanceof HTMLElement && node.matches(viewportSelector)) register(node)
      node.querySelectorAll<HTMLElement>(viewportSelector).forEach(register)
    }

    function update(): void {
      frame = 0
      const writes: Array<() => void> = []
      for (const state of dirty) {
        if (!state.element.isConnected) {
          writes.push(() => unregister(state))
          continue
        }
        const table = state.table
        if (!table) continue
        const headers = [...state.headers]
        // Native overflow already clips ordinary tables. Only frozen columns
        // need per-cell masks; hidden price tables must not pay this layout cost.
        if (!headers.some(cell => cell.matches('.sticky-col-left, .sticky-col-right'))) {
          if (state.clips.size) writes.push(() => restore(state))
          continue
        }
        const viewport = state.element.getBoundingClientRect()
        if (viewport.width < 1 || viewport.height < 1) continue
        const inner = {
          left: viewport.left + state.element.clientLeft,
          right: viewport.left + state.element.clientLeft + state.element.clientWidth,
        }
        const left = Math.max(inner.left, ...headers.filter(cell => cell.classList.contains('sticky-col-left'))
          .map(cell => cell.getBoundingClientRect().right))
        const right = Math.min(inner.right, ...headers.filter(cell => cell.classList.contains('sticky-col-right'))
          .map(cell => cell.getBoundingClientRect().left))
        for (const cell of state.clips.keys()) {
          if (!cell.isConnected || cell.closest('table') !== table) writes.push(() => restoreCell(state, cell))
        }
        for (const row of table.rows) {
          if (row.getAttribute('aria-hidden') === 'true') continue
          for (const cell of row.cells) {
            const rect = cell.getBoundingClientRect()
            if (rect.width < 1 || rect.height < 1) continue
            const fixed = cell.classList.contains('sticky-col-left') || cell.classList.contains('sticky-col-right')
            const clip = {
              left: Math.max(rect.left, fixed ? inner.left : left),
              right: Math.min(rect.right, fixed ? inner.right : right),
            }
            const offsets = [0, rect.right - clip.right, 0, clip.left - rect.left]
              .map(value => Math.max(0, Math.round(value * 100) / 100))
            const value = clip.right <= clip.left ? 'inset(0 0 0 100%)'
              : offsets.every(value => value === 0) ? '' : `inset(${offsets.map(value => `${value}px`).join(' ')})`
            if (!value) {
              if (state.clips.has(cell)) writes.push(() => restoreCell(state, cell))
            } else if (cell.style.clipPath !== value) {
              writes.push(() => {
                if (!state.clips.has(cell)) state.clips.set(cell, {
                  value: cell.style.getPropertyValue('clip-path'),
                  priority: cell.style.getPropertyPriority('clip-path'),
                })
                cell.style.setProperty('clip-path', value)
              })
            }
          }
        }
      }
      dirty.clear()
      // Finish geometry reads for every affected table before changing paint.
      for (const write of writes) write()
    }

    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(entries => {
      for (const entry of entries) {
        const state = viewports.get(entry.target as HTMLElement) ?? observedTables.get(entry.target)
        if (state) schedule(state)
      }
    })

    // Register SPA and teleported-dialog tables incrementally. Clip writes and
    // virtual spacer height changes do not alter horizontal geometry.
    const mutations = new MutationObserver(records => {
      for (const record of records) {
        if (record.type === 'attributes') {
          const target = record.target
          if (!(target instanceof HTMLTableCellElement)) continue
          const previous = (record.oldValue ?? '').split(/\s+/).filter(value => value === 'sticky-col-left' || value === 'sticky-col-right').sort().join(' ')
          const current = [...target.classList].filter(value => value === 'sticky-col-left' || value === 'sticky-col-right').sort().join(' ')
          if (previous === current) continue
        }
        const target = record.target instanceof Element ? record.target.closest<HTMLElement>(viewportSelector) : null
        const state = target && viewports.get(target)
        if (state) {
          syncTable(state)
          schedule(state)
        }
        for (const node of record.addedNodes) if (node instanceof Element) registerTree(node)
      }
      for (const state of viewports.values()) if (!state.element.isConnected) unregister(state)
    })

    const onScroll = (event: Event) => {
      const target = event.target
      if (!(target instanceof Element)) return
      const viewport = target.closest<HTMLElement>(viewportSelector)
      const state = viewport && viewports.get(viewport)
      if (state && state.scrollLeft !== state.element.scrollLeft) {
        state.scrollLeft = state.element.scrollLeft
        schedule(state)
      }
    }
    const onResize = () => schedule()
    registerTree(document.body)
    mutations.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'], attributeOldValue: true })
    document.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', onResize)

    stopTracking = () => {
      disposed = true
      cancelAnimationFrame(frame)
      mutations.disconnect()
      resize?.disconnect()
      document.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', onResize)
      for (const state of viewports.values()) restore(state)
      viewports.clear()
      observedTables.clear()
      dirty.clear()
    }
  }, { immediate: true, flush: 'post' })

  onScopeDispose(() => { stopWatch(); stopTracking?.() })
}
