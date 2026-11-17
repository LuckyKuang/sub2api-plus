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
    container.setAttribute('data-static-table-body-viewport', '')
    expect(paint(container, '', headerStylesheet).get('max-height')).toBe('none')
  })

  const ROW_CORNER = 'var(--record-row-radius) calc(var(--record-row-radius) + var(--record-row-gap))'
  const EDGE_TOP = 'inset 0 1px 0 var(--record-row-edge)'
  const EDGE_BOTTOM = 'inset 0 -1px 0 var(--record-row-edge)'
  const EDGE_LEFT = 'inset 1px 0 0 var(--record-row-edge)'
  const EDGE_RIGHT = 'inset -1px 0 0 var(--record-row-edge)'
  const CARD_SHADOW = 'var(--record-edge-top), var(--record-edge-bottom), var(--record-edge-left), var(--record-edge-right)'

  // Requirement (user audit): one table style everywhere, and every record is
  // its own card — soft fill, full hairline outline and item-radius corners,
  // with a small gap between cards — while keeping native table measurement.
  it('keeps native table measurement with card gaps and item-radius outside corners', () => {
    const container = table()
    const recordTable = paint(container.querySelector('table')!)
    expect(recordTable.get('border-spacing')).toBe('0')
    expect(recordTable.get('border-collapse')).toBe('separate')
    expect(recordTable.get('--record-row-radius')).toBe('var(--surface-radius-item)')
    expect(recordTable.get('--record-row-gap')).toBe('3px')
    expect(recordTable.get('--record-row-fill')).toBe('var(--record-card-fill)')
    // Hover and selection tints keep their strength at any card transparency.
    expect(recordTable.get('--record-row-hover')).toBe('rgb(var(--console-accent-500, 20 184 166) / 10%)')
    expect(recordTable.get('--record-row-selected')).toBe('rgb(var(--console-accent-500, 20 184 166) / 16%)')
    for (const row of container.querySelectorAll('tbody > tr')) {
      const first = paint(row.firstElementChild!)
      const last = paint(row.lastElementChild!)
      expect(first.get('border-block-width')).toBe('var(--record-row-gap)')
      expect(last.get('border-block-width')).toBe('var(--record-row-gap)')
      expect(first.get('border-top-left-radius')).toBe(ROW_CORNER)
      expect(first.get('border-bottom-left-radius')).toBe(ROW_CORNER)
      expect(last.get('border-top-right-radius')).toBe(ROW_CORNER)
      expect(last.get('border-bottom-right-radius')).toBe(ROW_CORNER)
      // The outline closes on both ends of the record.
      expect(first.get('--record-edge-left')).toBe(EDGE_LEFT)
      expect(last.get('--record-edge-right')).toBe(EDGE_RIGHT)
      expect(first.get('--record-edge-top')).toBe(EDGE_TOP)
      expect(first.get('--record-edge-bottom')).toBe(EDGE_BOTTOM)
      expect(first.get('box-shadow')).toBe(CARD_SHADOW)
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

  it('defines record card fills with a fixed strength, strongest for parents and lightest for leaves', () => {
    const consoleCss = postcss.parse(readFileSync(resolve(sourceRoot, 'styles/console.css'), 'utf8'))
    for (const scope of [':root', '.dark']) {
      const fills = new Map<string, string>()
      consoleCss.walkRules(rule => {
        if (rule.selector !== scope) return
        rule.walkDecls(/^--record-card-fill/, declaration => { fills.set(declaration.prop, declaration.value) })
      })
      const alpha = (token: string) => {
        const value = fills.get(token)
        expect(value, `${scope} ${token}`).toBeDefined()
        // Independent of the appearance setting that makes cards transparent.
        expect(value).not.toContain('--console-card-alpha')
        return Number.parseFloat(/\/\s*([\d.]+)%\)$/.exec(value!)![1])
      }
      expect(alpha('--record-card-fill-strong')).toBeGreaterThan(alpha('--record-card-fill'))
      expect(alpha('--record-card-fill')).toBeGreaterThan(alpha('--record-card-fill-leaf'))
    }
  })

  it.each(['', 'dark'])('uses flat headers, transparent 12px gutters and a 6px gap before the cards in %s mode', theme => {
    const container = table()
    container.classList.add(...theme.split(' ').filter(Boolean))
    const scroll = paint(container)
    expect(scroll.get('border-inline')).toBe('12px solid transparent')
    expect(scroll.get('background-color')).toBe('transparent')
    expect(scroll.get('padding')).toBe('4px 0 8px')
    for (const layer of container.querySelectorAll('thead, thead > tr')) {
      expect(paint(layer).get('background-color')).toBe('transparent')
      expect(paint(layer).get('background-image')).toBe('none')
    }
    for (const header of container.querySelectorAll('th')) {
      const headerPaint = paint(header)
      expect(headerPaint.get('background-color')).toBe('transparent')
      expect(headerPaint.get('background-image')).toBe('none')
      expect(headerPaint.get('border-radius')).toBe('0')
      expect(headerPaint.get('border-bottom-width')).toBe('6px')
      // Cards delimit themselves; the header carries no rule line.
      expect(headerPaint.get('box-shadow')).toBe('none')
    }
    expect(paint(container.querySelector('td')!).get('border-block-width')).toBe('var(--record-row-gap)')
  })

  it.each(['light', 'dark'].flatMap(theme => [0, 50, 100].map(transparency => ({ theme, transparency }))))('paints record cards as background layers, never as a cell background color, in $theme mode at $transparency percent transparency', ({ theme, transparency }) => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    document.documentElement.style.setProperty('--console-card-alpha', String(1 - transparency / 100))
    document.documentElement.toggleAttribute('data-console-transparent-cards', transparency > 0)
    // Ordinary rows, footer totals and spanning business records paint their
    // card through the same layers, including frozen edges. This deliberately
    // supplies the old platform-specific tint so that it cannot restore a
    // second fill layer.
    const container = table(`<thead class="surface-inset"><tr class="surface-inset"><th rowspan="2" data-header-edge-bottom>Model</th><th colspan="3">Price</th></tr><tr><th>Input</th><th>Output</th><th>Actions</th></tr></thead>
      <tbody><tr><th scope="row" class="sticky-col sticky-col-left">Model</th><td>Input</td><td style="--record-row-bg: rgb(237 242 240)">Output</td><td class="sticky-col sticky-col-right">Actions</td></tr><tr aria-hidden="true"><td colspan="4" style="height:300px"></td></tr></tbody>
      <tbody class="record-group"><tr><th rowspan="2" data-record-edges="t b l tl bl">Pricing group</th><td colspan="3" data-record-edges="t r tr">Primary tier</td></tr><tr><td colspan="3" data-record-edges="b r br">Expanded tier</td></tr></tbody>
      <tfoot><tr><th scope="row" class="sticky-col sticky-col-left">Total</th><td colspan="2">Amount</td><td class="sticky-col sticky-col-right">Actions</td></tr></tfoot>`)
    for (const layer of container.querySelectorAll('thead, thead > tr, thead > tr > th')) {
      expect(paint(layer).get('background-color')).toBe('transparent')
      expect(paint(layer).get('background-image')).toBe('none')
    }
    expect(paint(container.querySelector('thead [rowspan]')!).get('border-bottom-width')).toBe('6px')
    for (const cell of container.querySelectorAll(':is(tbody, tfoot) > tr:not([aria-hidden="true"]) > :is(td, th)')) {
      const cellPaint = paint(cell)
      expect(cellPaint.get('background-color')).toBe('transparent')
      expect(cellPaint.get('background-image')).toContain('var(--record-row-tint, transparent)')
      expect(cellPaint.get('background-image')).toContain('var(--record-row-fill, transparent)')
    }
    for (const cell of container.querySelectorAll('.sticky-col')) {
      const frozen = paint(cell)
      expect(frozen.get('background-color')).toBe('transparent')
      expect(frozen.get('box-shadow')).toContain('var(--record-edge-top), var(--record-edge-bottom)')
    }
    const spacer = container.querySelector<HTMLElement>('[aria-hidden="true"] td')!
    expect(paint(spacer).get('background')).toBe('none')
    expect(paint(spacer).get('border')).toBe('0')
    expect(spacer.style.height).toBe('300px')
  })

  // Hover and selection deepen the whole card, frozen cells included, and
  // switch its outline to the accent.
  it.each([
    ['', 'transparent', 'var(--record-row-edge-rest)'],
    ['data-hover', 'var(--record-row-hover)', 'var(--record-row-edge-active)'],
    ['aria-selected="true"', 'var(--record-row-selected)', 'var(--record-row-edge-active)'],
  ])('tints the card and its outline for state %s, including pinned cells', (state, tint, edge) => {
    const container = table(`<tbody><tr ${state}><td class="sticky-col sticky-col-left">User</td><td class="sticky-col sticky-col-left">Account</td><td>Model</td><td class="sticky-col sticky-col-right">Actions</td></tr></tbody>`)
    const row = container.querySelector('tr')!
    const rowPaint = paint(row)
    expect(rowPaint.get('background-color')).toBe('transparent')
    expect(rowPaint.get('--record-row-tint')).toBe(tint)
    expect(rowPaint.get('--record-row-edge')).toBe(edge)
    for (const cell of row.children) {
      const cellPaint = paint(cell)
      expect(cellPaint.get('border')).toBe('0 solid transparent')
      expect(cellPaint.get('background-color')).toBe('transparent')
      expect(cellPaint.get('background-image')).toContain('var(--record-row-tint, transparent)')
      expect(cellPaint.get('background-clip')).toBe('padding-box')
    }
    // Frozen columns must not depend on a negative layer behind a sticky cell.
    for (const cell of row.querySelectorAll('.sticky-col')) {
      expect(paint(cell, '::before').get('content')).toBe('none')
    }
    expect(paint(row.firstElementChild!).get('border-top-left-radius')).toBe(ROW_CORNER)
    expect(paint(row.lastElementChild!).get('border-top-right-radius')).toBe(ROW_CORNER)
  })

  it('marks the leading edge of a frozen actions column so scrolled columns read as continuing', () => {
    const container = table('<thead><tr><th>Name</th><th class="sticky-col sticky-col-right">Actions</th></tr></thead><tbody><tr><td>Name</td><td class="sticky-col sticky-col-right">Edit</td></tr></tbody>')
    const bodyActions = paint(container.querySelector('tbody .sticky-col-right')!)
    expect(bodyActions.get('box-shadow')).toBe('var(--record-edge-top), var(--record-edge-bottom), inset 1px 0 0 var(--surface-edge), var(--record-edge-right)')
    const headerActions = paint(container.querySelector('thead .sticky-col-right')!)
    expect(headerActions.get('box-shadow')).toBe('inset 1px 0 0 var(--surface-edge)')
  })

  // User audit: expanded or collapsed, a parent row and each of its children
  // are separate cards — parents stronger, children lighter and indented.
  it('paints hierarchical parent and leaf rows as separate cards with stepped fills', () => {
    const container = table(`<tbody><tr class="record-row-parent"><td>gpt-image-2</td><td>5</td></tr><tr class="record-row-leaf"><td>admin@example.com</td><td>5</td></tr></tbody>`)
    const [parent, leaf] = container.querySelectorAll('tr')
    expect(paint(parent).get('--record-row-fill')).toBe('var(--record-card-fill-strong)')
    expect(paint(leaf).get('--record-row-fill')).toBe('var(--record-card-fill-leaf)')
    for (const row of [parent, leaf]) {
      expect(paint(row.firstElementChild!).get('--record-edge-left')).toBe(EDGE_LEFT)
      expect(paint(row.lastElementChild!).get('--record-edge-right')).toBe(EDGE_RIGHT)
      expect(paint(row.firstElementChild!).get('border-block-width')).toBe('var(--record-row-gap)')
    }
    const leafStart = paint(leaf.firstElementChild!)
    expect(leafStart.get('border-left-width')).toBe('var(--record-row-indent)')
    // The indent is a transparent border, so the horizontal arc adds it back.
    expect(leafStart.get('border-top-left-radius')).toBe('calc(var(--record-row-radius) + var(--record-row-indent)) calc(var(--record-row-radius) + var(--record-row-gap))')
    expect(paint(parent.firstElementChild!).get('border-left-width')).toBeUndefined()
  })

  it('paints a rowspan group as one card and leaves virtual spacers unpainted', () => {
    const container = table(`<tbody class="record-group" data-hover><tr><td rowspan="2" data-record-edges="t b l tl bl">Channel</td><td data-record-edges="t r tr">Platform</td></tr><tr><td data-record-edges="b r br">Details</td></tr></tbody><tbody><tr aria-hidden="true"><td colspan="2" style="height:300px"></td></tr></tbody>`)
    expect(paint(container.querySelector('.record-group')!).get('--record-row-tint')).toBe('var(--record-row-hover)')
    const spanning = paint(container.querySelector('[rowspan]')!)
    expect(spanning.get('border-top-width')).toBe('var(--record-row-gap)')
    expect(spanning.get('border-bottom-width')).toBe('var(--record-row-gap)')
    expect(spanning.get('border-top-left-radius')).toBe(ROW_CORNER)
    expect(spanning.get('border-bottom-left-radius')).toBe(ROW_CORNER)
    expect(spanning.get('--record-edge-top')).toBe(EDGE_TOP)
    expect(spanning.get('--record-edge-bottom')).toBe(EDGE_BOTTOM)
    expect(spanning.get('--record-edge-left')).toBe(EDGE_LEFT)
    // Inner rows of the record carry no edge between them.
    const top = paint(container.querySelector('[data-record-edges="t r tr"]')!)
    expect(top.get('--record-edge-bottom')).toBe('inset 0 0 0 transparent')
    expect(top.get('--record-edge-right')).toBe(EDGE_RIGHT)
    expect(top.get('border-top-right-radius')).toBe(ROW_CORNER)
    const bottom = paint(container.querySelector('[data-record-edges="b r br"]')!)
    expect(bottom.get('--record-edge-top')).toBe('inset 0 0 0 transparent')
    expect(bottom.get('--record-edge-bottom')).toBe(EDGE_BOTTOM)
    expect(bottom.get('border-bottom-right-radius')).toBe(ROW_CORNER)
    const spacer = container.querySelector('[aria-hidden] td')!
    expect(paint(spacer).get('border')).toBe('0')
    expect(paint(spacer).get('padding')).toBe('0')
    expect(paint(spacer).get('background')).toBe('none')
    expect(paint(spacer).get('box-shadow')).toBeUndefined()
    expect((spacer as HTMLElement).style.height).toBe('300px')
  })

  // Short statistic rows step one rung down the radius ladder.
  it.each([
    ['record-table-compact', 'var(--surface-radius-tag)'],
    ['record-table-compact record-table-dense', 'var(--surface-radius-xs)'],
  ])('uses smaller record corners for %s', (classes, radius) => {
    const container = table('<tbody><tr><td class="sticky-col sticky-col-left">Model</td><td class="sticky-col sticky-col-right">Usage</td></tr></tbody>')
    const recordTable = container.querySelector('table')!
    recordTable.classList.add(...classes.split(' '))
    expect(paint(recordTable).get('--record-row-radius')).toBe(radius)
    const row = container.querySelector('tr')!
    expect(paint(row.firstElementChild!).get('border-top-left-radius')).toBe(ROW_CORNER)
    expect(paint(row.lastElementChild!).get('border-bottom-right-radius')).toBe(ROW_CORNER)
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
