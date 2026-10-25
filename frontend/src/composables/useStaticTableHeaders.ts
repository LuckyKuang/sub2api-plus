import { onScopeDispose, watch, type Ref } from 'vue'

const viewportSelector = '.record-table-scroll, .workspace-table-viewport'
const tableSelector = 'table.record-table'
const measureClass = 'static-table-measure-head'

interface HeaderState {
  table: HTMLTableElement
  viewport: HTMLElement
  head: HTMLTableSectionElement
  measure: HTMLTableSectionElement
  frame: HTMLDivElement
  rail: HTMLDivElement
  headerTable: HTMLTableElement
  observer: MutationObserver
  originalMaxHeight: string
  originalFlex: string
  originalMargin: string
  originalPaddingTop: string
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
    const observed = new Map<Element, Set<HTMLTableElement>>()
    const dirty = new Set<HTMLTableElement>()
    let frame = 0
    let disposed = false

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
        states.delete(table)
      }
      for (const [target, tables] of observed) if (tables.delete(table) && !tables.size) {
        resize?.unobserve(target)
        observed.delete(target)
      }
      candidates.delete(table)
      dirty.delete(table)
    }

    function register(table: HTMLTableElement): void {
      if (candidates.has(table)) return
      const viewport = table.closest<HTMLElement>(viewportSelector)
      if (!viewport || !table.tHead) return
      candidates.add(table)
      observe(table, table)
      observe(viewport, table)
      schedule(table)
    }

    function registerTree(node: Element): void {
      if (node.matches(tableSelector)) register(node as HTMLTableElement)
      node.querySelectorAll<HTMLTableElement>(tableSelector).forEach(register)
    }

    function split(table: HTMLTableElement, viewport: HTMLElement): HeaderState | undefined {
      const head = table.tHead
      if (!head || head.classList.contains(measureClass)) return
      const styles = getComputedStyle(viewport)
      const parentStyles = viewport.parentElement && getComputedStyle(viewport.parentElement)
      const constrained = viewport.scrollHeight > viewport.clientHeight
        || (styles.maxHeight !== 'none' && styles.maxHeight !== '')
        || (Number(styles.flexGrow) > 0 && parentStyles?.display.includes('flex'))
      // Distribution lists and dialog tables also need a static header, even
      // when their original markup never opted into position: sticky.
      if (!constrained || !['auto', 'scroll'].includes(styles.overflowY)) return

      // Computed styles are live in browsers; snapshot before adding body roles.
      const originalLayout = {
        originalMaxHeight: authoredMaxHeight(viewport, styles) || styles.maxHeight,
        originalFlex: styles.flex,
        originalMargin: `${styles.marginTop} ${styles.marginRight} ${styles.marginBottom} ${styles.marginLeft}`,
        originalPaddingTop: styles.paddingTop,
      }

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
      observe(head, table)
      for (const cell of measure.querySelectorAll('th,td')) observe(cell, table)
      return state
    }

    function update(): void {
      frame = 0
      for (const table of [...dirty]) {
        dirty.delete(table)
        if (!table.isConnected) { unregister(table); continue }
        let state = states.get(table)
        const viewport = state?.viewport ?? table.closest<HTMLElement>(viewportSelector)
        if (!viewport) { unregister(table); continue }
        if (state) {
          const replacement = [...table.children].find(child => child.tagName === 'THEAD' && child !== state!.measure)
          if (replacement || !state.head.isConnected || state.head.parentElement !== state.headerTable) {
            unregister(table)
            register(table)
            continue
          }
          // Transfer a v-show applied to the original component root to its shell.
          const hidden = getComputedStyle(viewport).display === 'none'
          if (state.frame.hidden !== hidden) state.frame.hidden = hidden
          if (!viewport.classList.contains('static-table-body-viewport')) viewport.classList.add('static-table-body-viewport')
          if (!table.classList.contains('static-table-source')) table.classList.add('static-table-source')
        }
        const bounds = viewport.getBoundingClientRect()
        if (bounds.width < 1 || bounds.height < 1) continue
        state ??= split(table, viewport)
        if (!state) continue
        const mirrored = state.needsMirror
        if (mirrored) {
          const next = measurementHead(state.head)
          for (const cell of state.measure.querySelectorAll('th,td')) {
            resize?.unobserve(cell)
            observed.delete(cell)
          }
          state.measure.replaceWith(next)
          state.measure = next
          state.needsMirror = false
          for (const cell of next.querySelectorAll('th,td')) observe(cell, table)
        }
        const height = state.measure.getBoundingClientRect().height
        const width = table.getBoundingClientRect().width
        if (!height || !width) continue
        const heightValue = `${height}px`
        if (table.style.getPropertyValue('--static-table-head-height') !== heightValue) table.style.setProperty('--static-table-head-height', heightValue)
        const css = getComputedStyle(viewport)
        const maxHeight = authoredMaxHeight(viewport, css)
        if (maxHeight && state.frame.style.maxHeight !== maxHeight) state.frame.style.maxHeight = maxHeight
        if (state.originalLabel === null) {
          const label = viewport.getAttribute('aria-label')
          if (label && table.getAttribute('aria-label') !== label) table.setAttribute('aria-label', label)
        }
        state.rail.style.marginLeft = `${viewport.clientLeft + (parseFloat(css.paddingLeft) || 0)}px`
        state.rail.style.width = `${viewport.clientWidth - (parseFloat(css.paddingLeft) || 0) - (parseFloat(css.paddingRight) || 0)}px`
        state.headerTable.style.setProperty('width', `${width}px`, 'important')
        const widths = columnWidths(state.measure)
        let columns = state.headerTable.querySelector('colgroup')
        if (!columns || columns.children.length !== widths.length) {
          const next = document.createElement('colgroup')
          for (let index = 0; index < widths.length; index++) next.append(document.createElement('col'))
          if (columns) columns.replaceWith(next)
          else state.headerTable.prepend(next)
          columns = next
        }
        widths.forEach((columnWidth, index) => { (columns!.children[index] as HTMLElement).style.width = `${columnWidth}px` })
        state.rail.scrollLeft = viewport.scrollLeft
        const geometry = JSON.stringify([height, width, ...widths])
        if (mirrored || geometry !== state.geometry) {
          state.geometry = geometry
          // DataTable owns sticky offsets and observes its measurement cells.
          // A replaced copy must refresh those refs after this layout is ready.
          viewport.dispatchEvent(new Event('static-table-header-measured'))
        }
      }
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
          if (record.type === 'childList') for (const state of states.values()) {
            if (state.rail === rail && state.head.parentElement !== state.headerTable) schedule(state.table)
          }
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
        if (record.type === 'attributes' && target) {
          for (const table of candidates) {
            const viewport = states.get(table)?.viewport ?? table.closest(viewportSelector)
            if (viewport && (target === table || target === viewport || target.contains(viewport)) && meaningfulStyle(record)) schedule(table)
          }
        }
      }
      for (const table of candidates) if (!table.isConnected) unregister(table)
    })
    const onScroll = (event: Event) => {
      for (const state of states.values()) {
        if (event.target === state.viewport && state.rail.scrollLeft !== state.viewport.scrollLeft) state.rail.scrollLeft = state.viewport.scrollLeft
        else if (event.target === state.rail && state.viewport.scrollLeft !== state.rail.scrollLeft) state.viewport.scrollLeft = state.rail.scrollLeft
      }
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
      dirty.clear()
    }
  }, { immediate: true, flush: 'post' })
  onScopeDispose(() => { stopWatch(); stopTracking?.() })
}
