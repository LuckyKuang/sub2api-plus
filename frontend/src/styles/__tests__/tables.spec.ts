import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it } from 'vitest'
import postcss from 'postcss'

const sourceRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const stylesheet = postcss.parse(readFileSync(resolve(sourceRoot, 'styles/tables.css'), 'utf8'))

// Evaluate matching rules against real table structures, including logical
// group boundaries. State attributes stand in for browser hover in jsdom.
function paint(element: Element, pseudo = '') {
  const result = new Map<string, string>()
  stylesheet.walkRules(rule => {
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

afterEach(() => document.body.replaceChildren())

describe('record table appearance requirements', () => {
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

  it.each(['', 'dark'])('uses opaque flat headers, 12px gutters and a 12px header gap in %s mode', theme => {
    const container = table()
    container.classList.add(...theme.split(' ').filter(Boolean))
    const scroll = paint(container)
    expect(scroll.get('border-inline')).toBe('12px solid var(--record-table-bg)')
    expect(scroll.get('padding')).toBe('8px 0')
    expect(paint(container.querySelector('thead')!).get('background-color')).toBe('var(--record-table-bg, var(--surface-solid))')
    for (const header of container.querySelectorAll('th')) {
      const headerPaint = paint(header)
      expect(headerPaint.get('background-image')).toBe('none')
      expect(headerPaint.get('border-radius')).toBe('0')
      expect(headerPaint.get('box-shadow')).toBe('none')
      expect(headerPaint.get('border-bottom-width')).toBe('8px')
    }
    expect(paint(container.querySelector('td')!).get('border-block-width')).toBe('4px')
  })

  it.each(['', 'data-hover', 'aria-selected="true"'])('keeps row gaps transparent for %s, including pinned cells', state => {
    const container = table(`<tbody><tr ${state}><td class="sticky-col sticky-col-left">User</td><td class="sticky-col sticky-col-left">Account</td><td>Model</td><td class="sticky-col sticky-col-right">Actions</td></tr></tbody>`)
    const row = container.querySelector('tr')!
    expect(paint(row).get('background-color')).toBe('transparent')
    expect(paint(row).get('--record-row-tint')).toBe(state === 'data-hover' ? 'var(--surface-hover)' : state ? 'var(--surface-selected)' : undefined)
    for (const cell of [...row.children].slice(1, -1)) {
      const cellPaint = paint(cell)
      expect(cellPaint.get('border')).toBe('0 solid transparent')
      expect(cellPaint.get('background-color')).toBe('var(--record-row-bg, var(--surface-soft))')
      expect(cellPaint.get('background-clip')).toBe('padding-box')
      expect(cellPaint.get('box-shadow')).toBe('none')
    }
    // Interior pinned cells keep ordinary record paint. Only actual outer
    // table edges need a square blocker and an independently rounded fill.
    expect(paint(row.children[1]!, '::before').get('content')).toBe('none')
    for (const cell of [row.firstElementChild!, row.lastElementChild!]) {
      const blocker = paint(cell)
      expect(blocker.get('border-radius')).toBe('0')
      expect(blocker.get('background-color')).toBe('var(--record-table-bg, var(--surface-solid))')
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
      expect(rounded.get('background-color')).toBe('var(--record-row-bg, var(--surface-soft))')
      expect(rounded.get('background-image')).toContain('var(--record-row-tint, transparent)')
      // A matching border-radius shorthand can reset these longhands through
      // specificity even when declared earlier. Forbid that competing reset.
      expect(rounded.has('border-radius')).toBe(false)
      const side = cell === row.firstElementChild ? 'left' : 'right'
      expect(rounded.get(`border-top-${side}-radius`)).toBe('16px')
      expect(rounded.get(`border-bottom-${side}-radius`)).toBe('16px')
      expect(paint(cell, '::after').get('content')).toBe('none')
    }
  })

  it('paints rowspan/expanded groups as one hovered record and leaves virtual spacers unpainted', () => {
    const container = table(`<tbody class="record-group" data-hover><tr><td rowspan="2" data-record-edges="t b l tl bl">Channel</td><td data-record-edges="t r tr">Platform</td></tr><tr><td data-record-edges="b r br">Details</td></tr></tbody><tbody><tr aria-hidden="true"><td colspan="2" style="height:300px"></td></tr></tbody>`)
    expect(paint(container.querySelector('.record-group')!).get('--record-row-tint')).toBe('var(--surface-hover)')
    const spanning = paint(container.querySelector('[rowspan]')!)
    expect(spanning.get('border-top-width')).toBe('4px')
    expect(spanning.get('border-bottom-width')).toBe('4px')
    expect(spanning.get('border-top-left-radius')).toBe('16px 20px')
    expect(spanning.get('border-bottom-left-radius')).toBe('16px 20px')
    const spacer = container.querySelector('[aria-hidden] td')!
    expect(paint(spacer).get('border')).toBe('0')
    expect(paint(spacer).get('padding')).toBe('0')
    expect(paint(spacer).get('background')).toBe('none')
    expect((spacer as HTMLElement).style.height).toBe('300px')
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
