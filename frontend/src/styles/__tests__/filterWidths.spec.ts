import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss from 'postcss'

// The stylesheets that own the filter contract, in the application's import order.
const files = ['console.css', 'surfaces.css', 'tables.css', 'tableHeaders.css', 'workspace.css', 'product.css']
const stylesheet = postcss.parse(files
  .map(file => readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', file), 'utf8'))
  .join('\n'))

const CONTROLS = ['.input', '.select-trigger', '.date-picker-trigger', '.date-time-picker', '.date-picker-input']

interface Declaration {
  selector: string
  prop: string
  value: string
}

function declarations(predicate: (selector: string) => boolean): Declaration[] {
  const found: Declaration[] = []
  stylesheet.walkRules(rule => {
    if (!rule.selectors.some(predicate)) return
    rule.walkDecls(declaration => {
      found.push({ selector: rule.selector, prop: declaration.prop, value: declaration.value })
    })
  })
  return found
}

function rootToken(token: string): string | undefined {
  let value: string | undefined
  stylesheet.walkDecls(declaration => {
    if (declaration.prop === token && declaration.parent?.selector?.includes(':root')) {
      value = declaration.value
    }
  })
  return value
}

/** Declarations from rules that live inside the given media query. */
function insideMedia(query: string): Declaration[] {
  const found: Declaration[] = []
  stylesheet.walkAtRules('media', atRule => {
    if (!atRule.params.includes(query)) return
    atRule.walkRules(rule => {
      rule.walkDecls(declaration => {
        found.push({ selector: rule.selector, prop: declaration.prop, value: declaration.value })
      })
    })
  })
  return found
}

const desktop = insideMedia('min-width: 1024px')
const narrow = insideMedia('max-width: 1023px')
const all = declarations(() => true)

const find = (list: Declaration[], selector: string, prop: string) =>
  list.find(entry => entry.selector === selector && entry.prop === prop)

describe('data-table filter widths', () => {
  it('declares one control width and one wider search width', () => {
    expect(rootToken('--filter-control-width')).toBe('200px')
    expect(rootToken('--filter-search-width')).toBe('240px')
    expect(rootToken('--filter-gap')).toBe('12px')
  })

  it('gives every filter control the same fixed width on desktop', () => {
    expect(find(desktop, '.filter-field', 'width')?.value).toBe('var(--filter-control-width)')
    // The search field is the only deliberately wider sibling.
    expect(find(desktop, '.filter-field--search', 'width')?.value).toBe('var(--filter-search-width)')
  })

  it('fills the field with the control instead of letting the field follow content', () => {
    // A select wrapper in a flex row otherwise takes its max-content width,
    // which is the longest option label.
    const selector = `.filter-field :where(${CONTROLS.join(', ')})`
    expect(find(desktop, selector, 'width')?.value).toBe('100%')
  })

  it('lays the row out as one wrapping flex row with a shared gap', () => {
    expect(find(all, '.filter-bar', 'display')?.value).toBe('flex')
    expect(find(all, '.filter-bar', 'flex-wrap')?.value).toBe('wrap')
    expect(find(all, '.filter-bar', 'gap')?.value).toBe('var(--filter-gap)')
    expect(find(all, '.filter-field', 'flex')?.value).toBe('0 0 auto')
    expect(find(all, '.filter-field', 'min-width')?.value).toBe('0')
  })

  it('stacks full-width controls on narrow screens', () => {
    expect(find(narrow, '.filter-field', 'width')?.value).toBe('100%')
    expect(narrow.some(entry => entry.selector === '.filter-field' && entry.value === 'var(--filter-control-width)')).toBe(false)
  })

  it('never applies the field width outside a filter field', () => {
    // Forms, dialogs and settings pages keep their own control sizing.
    const sizedRules = all.filter(entry => entry.prop === 'width' && [
      'var(--filter-control-width)', 'var(--filter-search-width)',
    ].includes(entry.value))
    expect(sizedRules.length).toBeGreaterThan(0)
    for (const entry of sizedRules) {
      expect(entry.selector.split(',').every(selector => /^\.filter-field(?:--[\w-]+)?$/.test(selector.trim())), entry.selector).toBe(true)
    }
  })

  it('keeps the expanded select menu no narrower than the unified control', () => {
    const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../../components/common/SearchableSelect.vue')
    const source = readFileSync(componentPath, 'utf8')
    const floor = Number.parseFloat(rootToken('--filter-control-width') ?? '')
    expect(Number.isFinite(floor)).toBe(true)
    // The teleported menu is content-sized (`w-max`) and floored by the same
    // width the trigger takes, so expanding never shrinks below the control.
    expect(source).toContain(`const dropdownMinimumWidth = ${floor}`)
    expect(source).toContain('min-w-[200px]')
    // Long labels ellipsize rather than stretching the panel across the viewport.
    expect(source).toContain('Math.min(dropdownMaximumWidth, availableWidth)')
  })
})
