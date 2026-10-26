import { onScopeDispose, watch, type Ref } from 'vue'

const viewportSelector = '.record-table-scroll, .workspace-table-viewport'
const tableSelector = 'table.record-table'
const measureClass = 'static-table-measure-head'

interface HeaderLayout {
  originalMaxHeight: string
  originalFlex: string
  originalMargin: string
  originalPaddingTop: string
}

interface HeaderState extends HeaderLayout {
  table: HTMLTableElement
  viewport: HTMLElement
  head: HTMLTableSectionElement
  measure: HTMLTableSectionElement
  frame: HTMLDivElement
  rail: HTMLDivElement
  headerTable: HTMLTableElement
  observer: MutationObserver
  originalLabel: string | null
  needsMirror: boolean
  geometry: string
}

/** Column widths belong to the body table, including grouped/rowspan headers. */
function columnWidths(head: HTMLTableSectionElement): number[] {
  const occupied: boolean[][] = []
  const widths: number[] = []
  const spans: Array<{ start: number; count: number; width: number }> = []
  for (const [rowIndex, row] of [...head.rows].entries()) {
    const slots = occupied[rowIndex] ?? (occupied[rowIndex] = [])
    let column = 0
    for (const cell of row.cells) {
      while (slots[column]) column++
      const count = cell.colSpan
      const width = cell.getBoundingClientRect().width
      if (count === 1) widths[column] = width
      else spans.push({ start: column, count, width })
      for (let r = rowIndex; r < rowIndex + cell.rowSpan; r++) {
        const covered = occupied[r] ?? (occupied[r] = [])
        for (let c = column; c < column + count; c++) covered[c] = true
      }
      column += count
    }
  }
  for (const span of spans) {
    const missing = Array.from({ length: span.count }, (_, i) => span.start + i).filter(i => widths[i] === undefined)
    const assigned = Array.from({ length: span.count }, (_, i) => widths[span.start + i] ?? 0).reduce((a, b) => a + b, 0)
    for (const index of missing) widths[index] = Math.max(0, span.width - assigned) / missing.length
  }
  return widths
}

function measurementHead(head: HTMLTableSectionElement): HTMLTableSectionElement {
  const clone = head.cloneNode(true) as HTMLTableSectionElement
  clone.classList.add(measureClass)
  for (const element of [clone, ...clone.querySelectorAll<HTMLElement>('*')]) {
    element.removeAttribute('id')
    element.removeAttribute('data-testid')
    element.removeAttribute('data-test')
    element.style.removeProperty('clip-path')
    if (element.matches('th,td')) {
      const label = element.getAttribute('aria-label') ?? (element.textContent?.trim() || element.querySelector('[aria-label]')?.getAttribute('aria-label') || '')
      element.setAttribute('aria-label', label)
    }
    else if (element !== clone && !element.matches('tr')) element.setAttribute('aria-hidden', 'true')
    if (element.matches('a,button,input,select,textarea,[tabindex]')) element.setAttribute('tabindex', '-1')
    if (element instanceof HTMLInputElement || element instanceof HTMLSelectElement || element instanceof HTMLTextAreaElement || element instanceof HTMLButtonElement) element.disabled = true
  }
  return clone
}

function meaningfulStyle(record: MutationRecord): boolean {
  if (record.attributeName !== 'style' || !(record.target instanceof HTMLElement)) return true
  const before = document.createElement('span').style
  const after = document.createElement('span').style
  before.cssText = record.oldValue ?? ''
  after.cssText = record.target.style.cssText
  before.removeProperty('clip-path')
  after.removeProperty('clip-path')
  before.removeProperty('--static-table-head-height')
  after.removeProperty('--static-table-head-height')
  return before.cssText !== after.cssText
}

/** Keep authored responsive expressions instead of freezing computed vh in px. */
function authoredMaxHeight(viewport: HTMLElement, styles: CSSStyleDeclaration): string {
  return viewport.style.maxHeight || styles.getPropertyValue('--record-table-max-height').trim()
}

/**
 * Put the real, Vue-owned header above its native body scrollport. A hidden
 * measurement copy keeps the body's native column layout and existing table
 * refs intact. No vertical scroll listener, animated mask, or late paint clip
 * is involved in keeping body content out of the transparent header.
 */
export function useStaticTableHeaders(root: Ref<HTMLElement | null>): void {
  let stopTracking: (() => void) | undefined
  const stopWatch = watch(root, element => {
    stopTracking?.()
    stopTracking = undefined
    if (!element) return

    const candidates = new Set<HTMLTableElement>()
    const states = new Map<HTMLTableElement, HeaderState>()
    const owners = new Map<Element, Set<HTMLTableElement>>()
    const scrollOwners = new Map<EventTarget, HeaderState>()
    const observed = new Map<Element, Set<HTMLTableElement>>()
    const dirty = new Set<HTMLTableElement>()
    let frame = 0
    let disposed = false

    function index(target: Element, table: HTMLTableElement): void {
      let tables = owners.get(target)
      if (!tables) { tables = new Set(); owners.set(target, tables) }
      tables.add(table)
    }

    function observe(target: Element, table: HTMLTableElement): void {
      let tables = observed.get(target)
      if (!tables) {
        tables = new Set()
        observed.set(target, tables)
        resize?.observe(target)
      }
      tables.add(table)
    }

    function schedule(table?: HTMLTableElement): void {
      if (disposed) return
      if (table) dirty.add(table)
      else for (const candidate of candidates) dirty.add(candidate)
      if (!frame && dirty.size) frame = requestAnimationFrame(update)
    }

    function unregister(table: HTMLTableElement): void {
      const state = states.get(table)
      if (state) {
        state.observer.disconnect()
        // Vue can already have removed the viewport while its parent remains.
        // Restore only this table's real head, never a replacement table's head.
        const replacement = [...table.children].some(child => child.tagName === 'THEAD' && child !== state.measure)
        if (!replacement && state.head.parentElement === state.headerTable) {
          if (state.measure.parentElement === table) state.measure.replaceWith(state.head)
          else if (!table.tHead) table.insertBefore(state.head, table.tBodies[0] ?? table.tFoot)
        }
        state.measure.remove()
        state.rail.remove()
        table.classList.remove('static-table-source')
        table.style.removeProperty('--static-table-head-height')
        if (state.originalLabel === null) table.removeAttribute('aria-label')
        state.viewport.classList.remove('static-table-body-viewport')
        // A Vue root v-if (desktop table -> mobile cards) may insert its new
        // root into our frame before removing the original viewport. Preserve
        // every remaining Vue node, rather than removing that replacement.
        for (const child of [...state.frame.childNodes]) state.frame.before(child)
        state.frame.remove()
        scrollOwners.delete(state.viewport)
        scrollOwners.delete(state.rail)
        states.delete(table)
      }
      for (const [target, tables] of observed) if (tables.delete(table) && !tables.size) {
        resize?.unobserve(target)
        observed.delete(target)
      }
      candidates.delete(table)
      dirty.delete(table)
      for (const [target, tables] of owners) if (tables.delete(table) && !tables.size) owners.delete(target)
    }

    function register(table: HTMLTableElement): void {
      if (candidates.has(table)) return
      const viewport = table.closest<HTMLElement>(viewportSelector)
      if (!viewport || !table.tHead) return
      candidates.add(table)
      for (let target: Element | null = table; target; target = target.parentElement) index(target, table)
      observe(table, table)
      observe(viewport, table)
      schedule(table)
    }

    function registerTree(node: Element): void {
      if (node.matches(tableSelector)) register(node as HTMLTableElement)
      node.querySelectorAll<HTMLTableElement>(tableSelector).forEach(register)
    }

    function split(table: HTMLTableElement, viewport: HTMLElement, originalLayout: HeaderLayout): void {
      const head = table.tHead
      if (!head || head.classList.contains(measureClass)) return

      const frameElement = document.createElement('div')
      frameElement.className = 'static-table-frame'
      const rail = document.createElement('div')
      rail.className = 'static-table-header-rail'
      const headerTable = document.createElement('table')
      // Preserve table-scoped typography without registering a second record table.
      for (const attribute of table.attributes) if (attribute.name === 'class' || attribute.name.startsWith('data-v-')) headerTable.setAttribute(attribute.name, attribute.value)
      headerTable.classList.remove('record-table')
      headerTable.classList.add('static-table-header-table')
      headerTable.setAttribute('role', 'presentation')
      rail.append(headerTable)
      const measure = measurementHead(head)
      head.before(measure)
      headerTable.append(head)
      viewport.before(frameElement)
      frameElement.append(rail, viewport)
      viewport.classList.add('static-table-body-viewport')
      table.classList.add('static-table-source')

      const state: HeaderState = {
        table, viewport, head, measure, frame: frameElement, rail, headerTable,
        ...originalLayout,
        originalLabel: table.getAttribute('aria-label'),
        needsMirror: false,
        geometry: '',
        observer: new MutationObserver(records => {
          if (records.some(meaningfulStyle)) {
            state.needsMirror = true
            schedule(table)
          }
        }),
      }
      state.observer.observe(head, { subtree: true, childList: true, characterData: true, attributes: true, attributeOldValue: true })
      frameElement.style.flex = state.originalFlex
      frameElement.style.maxHeight = state.originalMaxHeight
      frameElement.style.margin = state.originalMargin
      frameElement.style.paddingTop = state.originalPaddingTop
      states.set(table, state)
      index(frameElement, table)
      scrollOwners.set(viewport, state)
      scrollOwners.set(rail, state)
      observe(head, table)
      for (const cell of measure.querySelectorAll('th,td')) observe(cell, table)
      schedule(table)
    }

    function update(): void {
      frame = 0
      const writes: Array<() => void> = []
      const notifications: Array<() => void> = []
      for (const table of [...dirty]) {
        dirty.delete(table)
        if (!table.isConnected) { writes.push(() => unregister(table)); continue }
        const state = states.get(table)
        const viewport = state?.viewport ?? table.closest<HTMLElement>(viewportSelector)
        if (!viewport) { writes.push(() => unregister(table)); continue }
        const css = getComputedStyle(viewport)
        if (state) {
          const replacement = [...table.children].find(child => child.tagName === 'THEAD' && child !== state!.measure)
          if (replacement || !state.head.isConnected || state.head.parentElement !== state.headerTable) {
            writes.push(() => { unregister(table); register(table) })
            continue
          }
          // Transfer a v-show applied to the original component root to its shell.
          const hidden = css.display === 'none'
          if (state.frame.hidden !== hidden) {
            writes.push(() => { state.frame.hidden = hidden; if (!hidden) schedule(table) })
          }
          if (!viewport.classList.contains('static-table-body-viewport') || !table.classList.contains('static-table-source')) {
            writes.push(() => { viewport.classList.add('static-table-body-viewport'); table.classList.add('static-table-source'); schedule(table) })
            continue
          }
        }
        const bounds = viewport.getBoundingClientRect()
        if (bounds.width < 1 || bounds.height < 1) continue
        if (!state) {
          // A max-height alone does not make a one-row price table scroll. Keep
          // its native header until real vertical overflow needs separation.
          if (viewport.scrollHeight <= viewport.clientHeight || !['auto', 'scroll'].includes(css.overflowY)) continue
          const layout: HeaderLayout = {
            originalMaxHeight: authoredMaxHeight(viewport, css) || css.maxHeight,
            originalFlex: css.flex,
            originalMargin: `${css.marginTop} ${css.marginRight} ${css.marginBottom} ${css.marginLeft}`,
            originalPaddingTop: css.paddingTop,
          }
          writes.push(() => split(table, viewport, layout))
          continue
        }
        const mirrored = state.needsMirror
        if (mirrored) {
          writes.push(() => {
            const next = measurementHead(state.head)
            for (const cell of state.measure.querySelectorAll('th,td')) {
              resize?.unobserve(cell)
              observed.delete(cell)
            }
            state.measure.replaceWith(next)
            state.measure = next
            state.needsMirror = false
            // A replacement must notify the owner after the next measured frame.
            state.geometry = ''
            for (const cell of next.querySelectorAll('th,td')) observe(cell, table)
            schedule(table)
          })
          continue
        }
        const height = state.measure.getBoundingClientRect().height
        const width = table.getBoundingClientRect().width
        if (!height || !width) continue
        const heightValue = `${height}px`
        const maxHeight = authoredMaxHeight(viewport, css)
        const label = viewport.getAttribute('aria-label')
        const marginLeft = `${viewport.clientLeft + (parseFloat(css.paddingLeft) || 0)}px`
        const railWidth = `${viewport.clientWidth - (parseFloat(css.paddingLeft) || 0) - (parseFloat(css.paddingRight) || 0)}px`
        const scrollLeft = viewport.scrollLeft
        const widths = columnWidths(state.measure)
        const geometry = JSON.stringify([height, width, ...widths])
        writes.push(() => {
          if (table.style.getPropertyValue('--static-table-head-height') !== heightValue) table.style.setProperty('--static-table-head-height', heightValue)
          if (maxHeight && state.frame.style.maxHeight !== maxHeight) state.frame.style.maxHeight = maxHeight
          if (state.originalLabel === null && label && table.getAttribute('aria-label') !== label) table.setAttribute('aria-label', label)
          if (state.rail.style.marginLeft !== marginLeft) state.rail.style.marginLeft = marginLeft
          if (state.rail.style.width !== railWidth) state.rail.style.width = railWidth
          if (state.headerTable.style.width !== `${width}px`) state.headerTable.style.setProperty('width', `${width}px`, 'important')
          let columns = state.headerTable.querySelector('colgroup')
          if (!columns || columns.children.length !== widths.length) {
            const next = document.createElement('colgroup')
            for (let index = 0; index < widths.length; index++) next.append(document.createElement('col'))
            if (columns) columns.replaceWith(next)
            else state.headerTable.prepend(next)
            columns = next
          }
          widths.forEach((columnWidth, index) => {
            const column = columns!.children[index] as HTMLElement
            if (column.style.width !== `${columnWidth}px`) column.style.width = `${columnWidth}px`
          })
          if (state.rail.scrollLeft !== scrollLeft) state.rail.scrollLeft = scrollLeft
          if (geometry !== state.geometry) {
            state.geometry = geometry
            notifications.push(() => viewport.dispatchEvent(new Event('static-table-header-measured')))
          }
        })
      }
      // All affected tables share a read phase. Structural changes are measured
      // on the next frame, never by forcing layout after each individual write.
      for (const write of writes) write()
      for (const notify of notifications) notify()
    }

    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(entries => {
      for (const entry of entries) for (const table of observed.get(entry.target) ?? []) schedule(table)
    })
    const mutations = new MutationObserver(records => {
      for (const record of records) {
        const target = record.target instanceof Element ? record.target : record.target.parentElement
        const rail = target?.closest('.static-table-header-rail')
        if (rail) {
          // Removing a moved Vue-owned head changes its parent, not the head's
          // own subtree, so its dedicated observer cannot see this lifecycle.
          const state = scrollOwners.get(rail)
          if (record.type === 'childList' && state && state.head.parentElement !== state.headerTable) schedule(state.table)
          continue
        }
        if (record.type === 'childList' && target instanceof HTMLTableElement && target.matches(tableSelector)
          && [...record.addedNodes, ...record.removedNodes].some(node => node instanceof HTMLTableSectionElement && node.tagName === 'THEAD' && !node.classList.contains(measureClass))) {
          // A same-size replacement, or a header returning after v-if, need
          // not resize anything. Do not rely on ResizeObserver to discover it.
          register(target)
          schedule(target)
        }
        for (const node of record.addedNodes) if (node instanceof Element) registerTree(node)
        if (record.type === 'attributes' && target && meaningfulStyle(record)) for (const table of owners.get(target) ?? []) schedule(table)
      }
      if (records.some(record => [...record.removedNodes].some(node => !node.isConnected))) {
        for (const table of candidates) if (!table.isConnected) unregister(table)
      }
    })
    const onScroll = (event: Event) => {
      const state = event.target && scrollOwners.get(event.target)
      if (!state) return
      if (event.target === state.viewport && state.rail.scrollLeft !== state.viewport.scrollLeft) state.rail.scrollLeft = state.viewport.scrollLeft
      else if (event.target === state.rail && state.viewport.scrollLeft !== state.rail.scrollLeft) state.viewport.scrollLeft = state.rail.scrollLeft
    }
    const onResize = () => schedule()
    registerTree(document.body)
    mutations.observe(document.body, { subtree: true, childList: true, attributes: true, attributeOldValue: true, attributeFilter: ['class', 'style', 'hidden', 'aria-label'] })
    document.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', onResize)
    stopTracking = () => {
      disposed = true
      cancelAnimationFrame(frame)
      mutations.disconnect()
      resize?.disconnect()
      document.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', onResize)
      for (const table of [...candidates]) unregister(table)
      observed.clear()
      owners.clear()
      scrollOwners.clear()
      dirty.clear()
    }
  }, { immediate: true, flush: 'post' })
  onScopeDispose(() => { stopWatch(); stopTracking?.() })
}
