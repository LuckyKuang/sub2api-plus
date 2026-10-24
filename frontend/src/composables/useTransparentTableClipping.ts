import { onScopeDispose, watch, type Ref } from 'vue'

const viewportSelector = '.record-table-scroll, .workspace-table-viewport'

interface OriginalClip {
  value: string
  priority: string
}

interface TableViewport {
  element: HTMLElement
  table: HTMLTableElement | null
  headers: Set<HTMLTableCellElement>
  clips: Map<HTMLElement, OriginalClip>
}

/** Keep transparent pinned table cells readable without opaque background blockers. */
export function useTransparentTableClipping(root: Ref<HTMLElement | null>, enabled: () => boolean): void {
  let stopTracking: (() => void) | undefined

  const stopWatch = watch([root, enabled], ([element, active]) => {
    stopTracking?.()
    stopTracking = undefined
    if (!element || !active) return

    const viewports = new Map<HTMLElement, TableViewport>()
    const observedTables = new Map<Element, TableViewport>()
    const observedSpacers = new WeakSet<Element>()
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
      const table = [...state.element.querySelectorAll<HTMLTableElement>('table.record-table')]
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
      // Native virtual tables can rebalance only their top/bottom spacer styles
      // while keeping total table size and rendered row identities unchanged.
      for (const spacer of table?.querySelectorAll(':scope > tbody > tr[aria-hidden="true"] > :is(td,th)') ?? []) {
        if (observedSpacers.has(spacer)) continue
        mutations.observe(spacer, { attributes: true, attributeFilter: ['style'] })
        observedSpacers.add(spacer)
      }
    }

    function schedule(state?: TableViewport): void {
      if (disposed) return
      if (state) dirty.add(state)
      else for (const viewport of viewports.values()) dirty.add(viewport)
      if (!frame && dirty.size) frame = requestAnimationFrame(update)
    }

    function register(element: HTMLElement): void {
      if (viewports.has(element)) return
      const state: TableViewport = { element, table: null, headers: new Set(), clips: new Map() }
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
        const viewport = state.element.getBoundingClientRect()
        if (viewport.width < 1 || viewport.height < 1) continue
        const inner = {
          left: viewport.left + state.element.clientLeft,
          top: Math.max(0, viewport.top + state.element.clientTop),
          right: viewport.left + state.element.clientLeft + state.element.clientWidth,
          bottom: Math.min(innerHeight, viewport.top + state.element.clientTop + state.element.clientHeight),
        }
        const headers = [...state.headers]
        const left = Math.max(inner.left, ...headers.filter(cell => cell.classList.contains('sticky-col-left'))
          .map(cell => cell.getBoundingClientRect().right))
        const right = Math.min(inner.right, ...headers.filter(cell => cell.classList.contains('sticky-col-right'))
          .map(cell => cell.getBoundingClientRect().left))
        const head = table.tHead
        const stickyHeaderBottoms = headers.filter(cell => {
          const style = getComputedStyle(cell)
          return style.position === 'sticky' && style.top !== 'auto'
        }).map(cell => cell.getBoundingClientRect().bottom)
        if (head && getComputedStyle(head).position === 'sticky') stickyHeaderBottoms.push(head.getBoundingClientRect().bottom)
        const headerBottom = Math.max(inner.top, Math.min(inner.bottom, Math.max(inner.top, ...stickyHeaderBottoms)))

        for (const cell of state.clips.keys()) {
          if (!cell.isConnected || cell.closest('table') !== table) writes.push(() => restoreCell(state, cell))
        }
        for (const row of table.rows) {
          if (row.getAttribute('aria-hidden') === 'true') continue
          const rowRect = row.getBoundingClientRect()
          if ((rowRect.bottom < inner.top || rowRect.top > inner.bottom) && ![...row.cells].some(cell => cell.rowSpan > 1)) continue
          const isHeader = row.parentElement?.tagName === 'THEAD'
          for (const cell of row.cells) {
            const rect = cell.getBoundingClientRect()
            if (rect.width < 1 || rect.height < 1) continue
            const fixed = cell.classList.contains('sticky-col-left') || cell.classList.contains('sticky-col-right')
            const clip = {
              left: Math.max(rect.left, fixed ? inner.left : left),
              right: Math.min(rect.right, fixed ? inner.right : right),
              top: Math.max(rect.top, isHeader ? inner.top : headerBottom),
              bottom: Math.min(rect.bottom, inner.bottom),
            }
            const offsets = [clip.top - rect.top, rect.right - clip.right, rect.bottom - clip.bottom, clip.left - rect.left]
              .map(value => Math.max(0, Math.round(value * 100) / 100))
            const value = clip.right <= clip.left || clip.bottom <= clip.top ? 'inset(50%)'
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

    // Register SPA and teleported-dialog tables incrementally. Only explicit
    // virtual spacer styles are observed; our clip writes cannot feed back.
    const mutations = new MutationObserver(records => {
      for (const record of records) {
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
      if (!(target instanceof Element)) { schedule(); return }
      const viewport = target.closest<HTMLElement>(viewportSelector)
      const state = viewport && viewports.get(viewport)
      if (state) schedule(state)
      else {
        // A surrounding page/card scroll can move a table relative to the viewport.
        for (const candidate of viewports.values()) if (target.contains(candidate.element)) schedule(candidate)
      }
    }
    const onResize = () => schedule()
    registerTree(document.body)
    mutations.observe(document.body, { childList: true, subtree: true })
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
