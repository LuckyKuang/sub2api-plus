import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss from 'postcss'
import type { Declaration, Rule } from 'postcss'
import tailwindcss from 'tailwindcss'

// Test both shared stylesheets in the application's import order.
const stylesheet = postcss.parse(['console.css', 'surfaces.css']
  .map(file => readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', file), 'utf8'))
  .join('\n'))
const surfaces = ['card', 'glass', 'glass-card', 'card-glass', 'glass-surface', 'shell-surface', 'surface-card', 'surface-panel', 'modal-content', 'dialog-container', 'dropdown']
const backdropProperties = ['backdrop-filter', '-webkit-backdrop-filter']
const surfaceProperties = new Set([
  ...backdropProperties, 'filter', 'transform', 'perspective', 'contain',
  'isolation', 'overflow', 'z-index', 'position', 'content', 'inset',
  'border-radius', 'pointer-events', 'background-color',
])

function hasAtRule(rule: Rule, names: string[]): boolean {
  let parent = rule.parent
  while (parent) {
    if (parent.type === 'atrule' && names.includes(parent.name)) return true
    parent = parent.parent
  }
  return false
}

function matches(rule: Rule, element: Element, pseudo = ''): boolean {
  // Layout-only :has() selectors are outside this contract and expose jsdom's
  // selector parser limitations. All filter and overlay-safety rules stay in scope.
  if (!rule.nodes.some(node => node.type === 'decl'
    && (surfaceProperties.has(node.prop) || node.prop.startsWith('--surface-')))) return false
  return rule.selectors.some(selector => {
    if (pseudo) return selector.endsWith(pseudo) && element.matches(selector.slice(0, -pseudo.length))
    return !selector.includes('::') && element.matches(selector)
  })
}

function declarationsFor(element: Element, pseudo = ''): Declaration[] {
  const declarations: Declaration[] = []
  stylesheet.walkRules(rule => {
    if (!hasAtRule(rule, ['media', 'supports']) && matches(rule, element, pseudo)) {
      rule.walkDecls(declaration => { declarations.push(declaration) })
    }
  })
  return declarations
}

function lastValue(declarations: Declaration[], property: string): string | undefined {
  return declarations.filter(declaration => declaration.prop === property).slice(-1)[0]?.value
}

function fixture(markup: string): Element {
  const container = document.createElement('div')
  container.innerHTML = markup
  return container.firstElementChild!
}

describe('shared surface overlay safety', () => {
  it('keeps surface positioning below component and utility positioning after important variants are built', async () => {
    const compiled = await postcss([tailwindcss({
      content: [{ raw: `${surfaces.join(' ')} app-header fixed absolute !card !dropdown !surface-card !surface-panel`, extension: 'html' }],
    })]).process(`@tailwind components;\n@tailwind utilities;\n${stylesheet.toString()}`, { from: undefined })
    const rules = postcss.parse(compiled.css)
    for (const classes of ['app-header shell-surface', 'card fixed', 'surface-card fixed', 'dropdown absolute', 'surface-panel absolute', 'dialog-container fixed']) {
      const element = document.createElement('div')
      element.className = classes
      const defaults: { selector: string; important: boolean }[] = []
      rules.walkRules(rule => {
        if (!rule.nodes.some(node => node.type === 'decl' && node.prop === 'position' && node.value === 'relative')) return
        for (const selector of rule.selectors.filter(selector => element.matches(selector))) {
          rule.walkDecls('position', declaration => {
            if (declaration.value === 'relative') defaults.push({ selector, important: Boolean(declaration.important) })
          })
        }
      })
      expect(defaults.length, classes).toBeGreaterThan(0)
      for (const rule of defaults) {
        expect(rule.important, `${classes}: ${rule.selector}`).toBe(false)
        expect(rule.selector.replace(/:where\([^)]*\)/g, '').trim(), `${classes}: fallback specificity`).toBe('')
      }
    }
  })

  // Inline menus can overlap later cards and fixed dialogs stay viewport-bound.
  it.each(surfaces)('%s does not trap descendant overlays', surface => {
    const declarations = declarationsFor(fixture(`<div class="${surface}"></div>`))
    expect(declarations.length).toBeGreaterThan(0)
    for (const declaration of declarations) {
      if ([...backdropProperties, 'filter', 'transform', 'perspective', 'contain'].includes(declaration.prop)) {
        expect(declaration.value, `${surface}: ${declaration.prop}`).toBe('none')
      }
      if (declaration.prop === 'isolation') expect(declaration.value).toBe('auto')
      if (declaration.prop === 'z-index') {
        expect(declaration.value).toBe(['modal-content', 'dialog-container'].includes(surface) ? '0' : 'auto')
      }
      if (declaration.prop === 'overflow') expect(declaration.value).toBe('visible')
    }
  })
})

describe('shared solid surface rendering', () => {
  it('never applies backdrop filtering or retains the obsolete filter tokens', () => {
    stylesheet.walkDecls(declaration => {
      if (backdropProperties.includes(declaration.prop)) {
        expect(declaration.value, `${declaration.parent?.toString()}`).toBe('none')
      }
      expect(`${declaration.prop}: ${declaration.value}`).not.toMatch(/--surface-(?:(?:panel|table)-)?filter\b/)
    })
  })

  it.each([...surfaces, 'surface-control', 'select-trigger', 'btn', 'input'])('%s has no dedicated decorative pseudo-element layer', surface => {
    const element = fixture(`<div class="${surface}"></div>`)
    for (const pseudo of ['::before', '::after']) {
      const content = declarationsFor(element, pseudo).filter(declaration => declaration.prop === 'content')
      expect(content.every(declaration => ['none', 'normal'].includes(declaration.value)), `${surface}${pseudo}`).toBe(true)
    }
  })

  it('uses opaque theme fills independently of custom card transparency', () => {
    const root = declarationsFor(document.documentElement)
    const fills = ['--surface-fill', '--surface-shell', '--surface-field', '--surface-panel', '--surface-sticky']
    for (const property of fills) {
      expect(lastValue(root, property), property).toBe('var(--surface-solid)')
    }
    expect(lastValue(root, '--surface-solid')).toBe('#f8faf9')
    expect(lastValue(root, '--surface-soft')).toBe('#edf2f0')
    const dark = declarationsFor(fixture('<div class="dark"></div>'))
    expect(lastValue(dark, '--surface-solid')).toBe('#262c2d')
    expect(lastValue(dark, '--surface-soft')).toBe('#202627')

    // Check every override, including media queries and custom appearance rules,
    // so an old translucent dark-mode or preference override cannot restore it.
    stylesheet.walkDecls(declaration => {
      if (fills.includes(declaration.prop)) expect(declaration.value).toBe('var(--surface-solid)')
      expect(`${declaration.prop}: ${declaration.value}`).not.toMatch(/--surface-(?:inset|field|sticky)-alpha\b/)
      if (fills.includes(declaration.prop) || declaration.prop === '--surface-soft') {
        expect(declaration.value).not.toContain('--console-card-alpha')
      }
    })
  })

  it('gives every table and pinned region an opaque fill without changing its positioning', () => {
    for (const parentClass of ['', 'usage-table']) {
      const table = fixture(`<div class="${parentClass}"><div class="table-wrapper"><table><thead class="sticky"><tr><th class="sticky-header-cell sticky-col"></th></tr></thead><tbody><tr><td class="sticky-col-left"></td><td class="sticky-col-left-first"></td><td class="sticky-col-left-second"></td><td class="sticky-col-right"></td></tr></tbody></table></div></div>`)
      expect(lastValue(declarationsFor(table.querySelector('.table-wrapper')!), 'background-color')).toBe('var(--surface-solid)')
      const pinnedSurfaces: [Element, string][] = [
        [table.querySelector('thead')!, '::before'],
        [table.querySelector('th')!, ''],
        ...['sticky-col-left', 'sticky-col-left-first', 'sticky-col-left-second']
          .map(name => [table.querySelector(`.${name}`)!, '::before'] as [Element, string]),
        [table.querySelector('.sticky-col-right')!, '::after'],
        [fixture('<div class="matrix-header sticky"></div>'), '::before'],
      ]
      for (const [element, pseudo] of pinnedSurfaces) {
        const declarations = declarationsFor(element, pseudo)
        expect(lastValue(declarations, 'background-color'), `${element.className}${pseudo}`).toBe('var(--surface-sticky)')
        if (pseudo) {
          expect(lastValue(declarations, 'position')).toBe('absolute')
          expect(lastValue(declarations, 'pointer-events')).toBe('none')
        }
      }
    }
  })
})
