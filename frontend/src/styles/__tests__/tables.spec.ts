import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it } from 'vitest'
import postcss from 'postcss'

const sourceRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const stylesheet = postcss.parse(readFileSync(resolve(sourceRoot, 'styles/tables.css'), 'utf8'))
const headerStylesheet = postcss.parse(readFileSync(resolve(sourceRoot, 'styles/tableHeaders.css'), 'utf8'))

// Evaluate matching rules against real table structures, including logical
// group boundaries. State attributes stand in for browser hover in jsdom.
function paint(element: Element, pseudo = '', rules = stylesheet) {
  const result = new Map<string, string>()
  rules.walkRules(rule => {
    const matches = rule.selectors.some(original => {
      let selector = original.replace(/:hover/g, '[data-hover]')
      if (pseudo) {
        if (!selector.endsWith(pseudo)) return false
        selector = selector.slice(0, -pseudo.length)
      } else if (selector.includes('::')) return false
      return element.matches(selector)
    })
    if (matches) rule.walkDecls(declaration => { result.set(declaration.prop, declaration.value) })
  })
  return result
}

function table(markup = '<thead><tr><th>User</th><th>Account</th></tr></thead><tbody><tr><td>User</td><td>Account</td></tr><tr><td>User 2</td><td>Account 2</td></tr></tbody>') {
  const container = document.createElement('div')
  container.className = 'record-table-scroll'
  container.innerHTML = `<table class="record-table">${markup}</table>`
  document.body.append(container)
  return container
}

afterEach(() => {
  document.body.replaceChildren()
  document.documentElement.classList.remove('dark')
  document.documentElement.removeAttribute('data-console-transparent-cards')
  document.documentElement.style.removeProperty('--console-card-alpha')
})

describe('record table appearance requirements', () => {
  it('bounds dialog tables locally without imposing a fixed height on short tables', () => {
    // Approved layout: modal instructions/forms keep their own document flow;
    // only a long table scrolls beneath its separate static header.
    const container = table()
    container.classList.add('record-table-dialog-scroll')
    const dialogPaint = paint(container, '', headerStylesheet)
    expect(dialogPaint.get('--record-table-max-height')).toBe('min(42vh, 420px)')
    expect(dialogPaint.get('max-height')).toBe('var(--record-table-max-height)')
    expect(dialogPaint.get('height')).toBeUndefined()
    expect(dialogPaint.get('overflow')).toBe('auto')
    expect(dialogPaint.get('overscroll-behavior')).toBe('contain')
    container.classList.add('static-table-body-viewport')
    expect(paint(container, '', headerStylesheet).get('max-height')).toBe('none')
  })

  it('keeps native table measurement with 8px row gaps and four 16px outside corners', () => {
    const container = table()
    expect(paint(container.querySelector('table')!).get('border-spacing')).toBe('0')
    expect(paint(container.querySelector('table')!).get('border-collapse')).toBe('separate')
    for (const row of container.querySelectorAll('tbody > tr')) {
      const first = paint(row.firstElementChild!)
      const last = paint(row.lastElementChild!)
      expect(first.get('border-block-width')).toBe('4px')
      expect(last.get('border-block-width')).toBe('4px')
      expect(first.get('border-top-left-radius')).toBe('16px 20px')
      expect(first.get('border-bottom-left-radius')).toBe('16px 20px')
      expect(last.get('border-top-right-radius')).toBe('16px 20px')
      expect(last.get('border-bottom-right-radius')).toBe('16px 20px')
      expect(paint(row).get('background-color')).toBe('transparent')
    }
    // No table geometry may be converted to a block/grid or clip menus.
    stylesheet.walkRules(rule => {
      if (!rule.selector.includes('table.record-table') || rule.selector.includes('::')) return
      rule.walkDecls(declaration => {
        expect(['display', 'overflow', 'transform', 'position']).not.toContain(declaration.prop)
      })
    })
  })

  it.each(['', 'dark'])('uses flat headers, transparent 12px gutters and a 12px header gap in %s mode', theme => {
    const container = table()
    container.classList.add(...theme.split(' ').filter(Boolean))
    const scroll = paint(container)
    expect(scroll.get('border-inline')).toBe('12px solid transparent')
    expect(scroll.get('background-color')).toBe('transparent')
    expect(scroll.get('padding')).toBe('8px 0')
    for (const layer of container.querySelectorAll('thead, thead > tr, thead > tr > th')) {
      expect(paint(layer).get('background-color')).toBe('transparent')
      expect(paint(layer).get('background-image')).toBe('none')
    }
    for (const header of container.querySelectorAll('th')) {
      const headerPaint = paint(header)
      expect(headerPaint.get('background-image')).toBe('none')
      expect(headerPaint.get('border-radius')).toBe('0')
      expect(headerPaint.get('box-shadow')).toBe('none')
      expect(headerPaint.get('border-bottom-width')).toBe('8px')
    }
    expect(paint(container.querySelector('td')!).get('border-block-width')).toBe('4px')
  })

  it.each(['light', 'dark'].flatMap(theme => [0, 50, 100].map(transparency => ({ theme, transparency }))))('lets the enclosing card alone paint static table fills in $theme mode at $transparency percent transparency', ({ theme, transparency }) => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    document.documentElement.style.setProperty('--console-card-alpha', String(1 - transparency / 100))
    document.documentElement.toggleAttribute('data-console-transparent-cards', transparency > 0)
    // Ordinary rows, footer totals and spanning business records must expose
    // the same enclosing card, including frozen edges and locally tinted cells.
    // This deliberately supplies the old platform-specific tint so that it
    // cannot restore a second fill layer after the shared fill is removed.
    const container = table(`<thead class="surface-inset"><tr class="surface-inset"><th rowspan="2" data-header-edge-bottom>Model</th><th colspan="3">Price</th></tr><tr><th>Input</th><th>Output</th><th>Actions</th></tr></thead>
      <tbody><tr><th scope="row" class="sticky-col sticky-col-left">Model</th><td>Input</td><td style="--record-row-bg: rgb(237 242 240)">Output</td><td class="sticky-col sticky-col-right">Actions</td></tr><tr aria-hidden="true"><td colspan="4" style="height:300px"></td></tr></tbody>
      <tbody class="record-group"><tr><th rowspan="2" data-record-edges="t b l tl bl">Pricing group</th><td colspan="3" data-record-edges="t r tr">Primary tier</td></tr><tr><td colspan="3" data-record-edges="b r br">Expanded tier</td></tr></tbody>
      <tfoot><tr><th scope="row" class="sticky-col sticky-col-left">Total</th><td colspan="2">Amount</td><td class="sticky-col sticky-col-right">Actions</td></tr></tfoot>`)
    for (const layer of container.querySelectorAll('thead, thead > tr, thead > tr > th')) {
      expect(paint(layer).get('background-color')).toBe('transparent')
      expect(paint(layer).get('background-image')).toBe('none')
    }
    expect(paint(container.querySelector('thead [rowspan]')!).get('border-bottom-width')).toBe('8px')
    for (const cell of container.querySelectorAll(':is(tbody, tfoot) > tr:not([aria-hidden="true"]) > :is(td, th)')) {
      expect(paint(cell).get('background-color')).toBe('transparent')
    }
    for (const cell of container.querySelectorAll('.sticky-col')) {
      const rounded = paint(cell, '::before')
      expect(rounded.get('background-color')).toBe('transparent')
      expect(rounded.get('background-image')).toContain('var(--record-row-tint, transparent)')
      expect(rounded.get('box-shadow')).toContain('var(--record-edge-top)')
    }
    const spacer = container.querySelector<HTMLElement>('[aria-hidden="true"] td')!
    expect(paint(spacer).get('background')).toBe('none')
    expect(paint(spacer).get('border')).toBe('0')
    expect(spacer.style.height).toBe('300px')
  })

  it.each(['', 'data-hover', 'aria-selected="true"'])('keeps row gaps transparent for %s, including pinned cells', state => {
    const container = table(`<tbody><tr ${state}><td class="sticky-col sticky-col-left">User</td><td class="sticky-col sticky-col-left">Account</td><td>Model</td><td class="sticky-col sticky-col-right">Actions</td></tr></tbody>`)
    const row = container.querySelector('tr')!
    expect(paint(row).get('background-color')).toBe('transparent')
    expect(paint(row).get('--record-row-tint')).toBe(state === 'data-hover' ? 'var(--surface-hover)' : state ? 'var(--surface-selected)' : undefined)
    for (const cell of [...row.children].slice(1, -1)) {
      const cellPaint = paint(cell)
      expect(cellPaint.get('border')).toBe('0 solid transparent')
      expect(cellPaint.get('background-color')).toBe('transparent')
      expect(cellPaint.get('background-image')).toContain('var(--record-row-tint, transparent)')
      expect(cellPaint.get('background-clip')).toBe('padding-box')
      expect(cellPaint.get('--record-edge-top')).toBe('inset 0 1px 0 var(--record-row-edge)')
      expect(cellPaint.get('--record-edge-bottom')).toBe('inset 0 -1px 0 var(--record-row-edge)')
      expect(cellPaint.get('--record-edge-left')).toBe('inset 0 0 0 transparent')
      expect(cellPaint.get('--record-edge-right')).toBe('inset 0 0 0 transparent')
      expect(cellPaint.get('box-shadow')).toContain('var(--record-edge-top)')
    }
    // Interior pinned cells keep ordinary record geometry. Actual outer
    // table edges use a rounded outline/tint layer without a static fill.
    expect(paint(row.children[1]!, '::before').get('content')).toBe('none')
    for (const cell of [row.firstElementChild!, row.lastElementChild!]) {
      const blocker = paint(cell)
      expect(blocker.get('border-radius')).toBe('0')
      expect(blocker.get('background-color')).toBe('transparent')
      expect(blocker.get('background-image')).toBe('none')
      expect(blocker.get('background-clip')).toBe('border-box')
      expect(blocker.get('border-block-width')).toBe('4px')
      expect(blocker.get('box-shadow')).toBe('none')
      const rounded = paint(cell, '::before')
      expect(rounded.get('content')).toBe("''")
      expect(rounded.get('position')).toBe('absolute')
      expect(rounded.get('inset')).toBe('0')
      expect(rounded.get('z-index')).toBe('-1')
      expect(rounded.get('pointer-events')).toBe('none')
      expect(rounded.get('background-color')).toBe('transparent')
      expect(rounded.get('background-image')).toContain('var(--record-row-tint, transparent)')
      expect(rounded.get('box-shadow')).toContain('var(--record-edge-left)')
      // A matching border-radius shorthand can reset these longhands through
      // specificity even when declared earlier. Forbid that competing reset.
      expect(rounded.has('border-radius')).toBe(false)
      const side = cell === row.firstElementChild ? 'left' : 'right'
      expect(rounded.get(`border-top-${side}-radius`)).toBe('16px')
      expect(rounded.get(`border-bottom-${side}-radius`)).toBe('16px')
      expect(paint(cell, '::after').get('content')).toBe('none')
    }
  })

  // Audited requirement: every record has a 1px outline even at 100% fill
  // transparency; hovering/selection changes its color, never its row geometry.
  it.each([
    ['', 'var(--surface-edge)'],
    ['data-hover', 'rgb(var(--console-accent-500, 20 184 166) / 80%)'],
    ['aria-selected="true"', 'rgb(var(--console-accent-500, 20 184 166) / 95%)'],
  ])('keeps independent record outlines in state %s', (state, edge) => {
    const container = table(`<tbody><tr ${state}><td>User</td><td>Account</td></tr></tbody>`)
    const row = container.querySelector('tr')!
    expect(paint(row).get('--record-row-edge')).toBe(edge)
    expect(edge).not.toContain('--console-card-alpha')
    const first = paint(row.firstElementChild!)
    const last = paint(row.lastElementChild!)
    expect(first.get('--record-edge-left')).toBe('inset 1px 0 0 var(--record-row-edge)')
    expect(last.get('--record-edge-right')).toBe('inset -1px 0 0 var(--record-row-edge)')
    expect(first.get('border-block-width')).toBe('4px')
    expect(last.get('border-block-width')).toBe('4px')
  })

  it('paints rowspan/expanded groups as one hovered record and leaves virtual spacers unpainted', () => {
    const container = table(`<tbody class="record-group" data-hover><tr><td rowspan="2" data-record-edges="t b l tl bl">Channel</td><td data-record-edges="t r tr">Platform</td></tr><tr><td data-record-edges="b r br">Details</td></tr></tbody><tbody><tr aria-hidden="true"><td colspan="2" style="height:300px"></td></tr></tbody>`)
    expect(paint(container.querySelector('.record-group')!).get('--record-row-tint')).toBe('var(--surface-hover)')
    const spanning = paint(container.querySelector('[rowspan]')!)
    expect(spanning.get('border-top-width')).toBe('4px')
    expect(spanning.get('border-bottom-width')).toBe('4px')
    expect(spanning.get('border-top-left-radius')).toBe('16px 20px')
    expect(spanning.get('border-bottom-left-radius')).toBe('16px 20px')
    expect(spanning.get('--record-edge-top')).toBe('inset 0 1px 0 var(--record-row-edge)')
    expect(spanning.get('--record-edge-bottom')).toBe('inset 0 -1px 0 var(--record-row-edge)')
    expect(spanning.get('--record-edge-left')).toBe('inset 1px 0 0 var(--record-row-edge)')
    const top = paint(container.querySelector('[data-record-edges="t r tr"]')!)
    expect(top.get('--record-edge-bottom')).toBe('inset 0 0 0 transparent')
    const bottom = paint(container.querySelector('[data-record-edges="b r br"]')!)
    expect(bottom.get('--record-edge-top')).toBe('inset 0 0 0 transparent')
    const spacer = container.querySelector('[aria-hidden] td')!
    expect(paint(spacer).get('border')).toBe('0')
    expect(paint(spacer).get('padding')).toBe('0')
    expect(paint(spacer).get('background')).toBe('none')
    expect(paint(spacer).get('box-shadow')).toBeUndefined()
    expect((spacer as HTMLElement).style.height).toBe('300px')
  })

  // Short statistic rows keep their existing modest corners when an outline is
  // added; sticky edge fills must use the same corner as the visible record.
  it.each([
    ['record-table-compact', '10px'],
    ['record-table-compact record-table-dense', '6px'],
  ])('preserves short-row corners for %s', (classes, radius) => {
    const container = table('<tbody><tr><td class="sticky-col sticky-col-left">Model</td><td class="sticky-col sticky-col-right">Usage</td></tr></tbody>')
    const recordTable = container.querySelector('table')!
    recordTable.classList.add(...classes.split(' '))
    expect(paint(recordTable).get('--record-compact-radius')).toBe(radius)
    const row = container.querySelector('tr')!
    expect(paint(row.firstElementChild!, '::before').get('border-top-left-radius')).toBe('var(--record-compact-radius)')
    expect(paint(row.lastElementChild!, '::before').get('border-bottom-right-radius')).toBe('var(--record-compact-radius)')
  })

  it('opts every authored Vue table into the shared record-card layout', () => {
    function visit(directory: string) {
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        const path = resolve(directory, entry.name)
        if (entry.isDirectory()) visit(path)
        else if (entry.name.endsWith('.vue')) {
          const source = readFileSync(path, 'utf8')
          for (const match of source.matchAll(/<table\b(?:[^"'>]|"[^"]*"|'[^']*')*>/g)) {
            expect(match[0], path).toMatch(/\bclass="[^"]*\brecord-table\b/)
          }
        }
      }
    }
    visit(sourceRoot)
  })
})
