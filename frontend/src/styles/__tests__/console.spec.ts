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
  'border', 'border-top', 'border-bottom', 'box-shadow',
  'border-top-left-radius', 'border-top-right-radius',
  'border-bottom-left-radius', 'border-bottom-right-radius',
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

describe('shared transparent surface rendering', () => {
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

  // Audited global appearance contract: fill alpha reaches zero at 100%
  // transparency, while the 1px frame remains visible in both themes.
  it('shares fill transparency across surface roles without fading their outlines', () => {
    const root = declarationsFor(document.documentElement)
    const fills = ['--surface-fill', '--surface-shell', '--surface-field', '--surface-panel', '--surface-sticky']
    for (const property of fills) {
      expect(lastValue(root, property), property).toBe('var(--surface-solid)')
    }
    expect(lastValue(root, '--surface-solid')).toBe('rgb(248 250 249 / var(--console-card-alpha, 1))')
    expect(lastValue(root, '--surface-soft')).toBe('rgb(237 242 240 / var(--console-card-alpha, 1))')
    expect(lastValue(root, '--surface-edge')).toBe('rgb(100 116 139 / 34%)')
    const dark = declarationsFor(fixture('<div class="dark"></div>'))
    expect(lastValue(dark, '--surface-solid')).toBe('rgb(38 44 45 / var(--console-card-alpha, 1))')
    expect(lastValue(dark, '--surface-soft')).toBe('rgb(32 38 39 / var(--console-card-alpha, 1))')
    expect(lastValue(dark, '--surface-edge')).toBe('rgb(203 213 225 / 28%)')

    // Check every override, including media queries and custom appearance rules,
    // so a page-specific preference cannot restore an opaque surface or fade
    // an outline when the user requests fully transparent cards.
    stylesheet.walkDecls(declaration => {
      if (fills.includes(declaration.prop)) expect(declaration.value).toBe('var(--surface-solid)')
      expect(`${declaration.prop}: ${declaration.value}`).not.toMatch(/--surface-(?:inset|field|sticky)-alpha\b/)
      if (['--surface-solid', '--surface-soft', '--surface-glint', '--surface-hover', '--surface-selected'].includes(declaration.prop)) {
        expect(declaration.value).toContain('--console-card-alpha')
      }
      if (declaration.prop === '--surface-edge') expect(declaration.value).not.toContain('--console-card-alpha')
    })
  })

  it('lets the grid monitor header share its scrollport fill without adding another paint layer', () => {
    const header = fixture('<div class="matrix-header surface-inset"></div>')
    const declarations = declarationsFor(header)
    expect(lastValue(declarations, 'background-color')).toBe('transparent')
    expect(lastValue(declarations, 'background-image')).toBe('none')
    expect(lastValue(declarations, 'position')).toBeUndefined()
    expect(lastValue(declarationsFor(header, '::before'), 'content')).toBe('none')
  })

  // Audited requirement: page title/filter/tab/footer shells have no outlines,
  // while independent business cards and controls retain their own borders.
  it('removes page chrome frames without removing nested business-card or control edges', () => {
    const container = fixture('<main class="app-main"><header class="card workspace-fixed"></header><nav class="card workspace-tabs"><button class="workspace-tab"></button></nav><div class="settings-tabs-shell"></div><section class="card workspace-shell ring-1"><header class="card-header border-b"></header><section class="card"><header class="card-header border-b"></header></section><button class="surface-control"></button></section><footer class="card pagination-card"></footer><div class="workspace-page"><section class="glass-surface"><header class="border-b"></header></section><div class="grid"><article class="card"></article></div></div><div class="table-page-layout"><div class="layout-section-fixed"><div class="card"></div></div></div></main>')
    document.body.append(container)
    const shell = container.querySelector('.workspace-shell')!
    const declarations = declarationsFor(shell)
    for (const element of container.querySelectorAll('.workspace-shell, .workspace-fixed, .workspace-tabs, .settings-tabs-shell, .pagination-card, .workspace-page > .glass-surface, .layout-section-fixed, .layout-section-fixed > .card')) {
      const paint = declarationsFor(element)
      for (const property of ['border', 'box-shadow']) {
        const shellRule = paint.filter(declaration => declaration.prop === property).at(-1)!
        expect(shellRule.value).toBe(property === 'border' ? '0' : 'none')
        // Utility rings and component-specific frame styles cannot restore it.
        expect(shellRule.important).toBe(true)
      }
      expect(lastValue(paint, '--tw-ring-shadow')).toBe('0 0 #0000')
    }
    for (const card of container.querySelectorAll('.workspace-shell > section.card, .grid > article.card')) {
      expect(lastValue(declarationsFor(card), 'border')).toBe('1px solid var(--surface-edge)')
      expect(lastValue(declarationsFor(card), 'box-shadow')).toBe('var(--surface-shadow)')
    }
    const mainHeader = container.querySelector('.workspace-shell > .card-header')!
    expect(lastValue(declarationsFor(mainHeader), 'border-bottom')).toBe('0')
    expect(lastValue(declarationsFor(container.querySelector('.workspace-page > .glass-surface > header')!), 'border-bottom')).toBe('0')
    expect(lastValue(declarationsFor(container.querySelector('.workspace-shell > section.card > .card-header')!), 'border-bottom')).toBeUndefined()
    expect(lastValue(declarationsFor(container.querySelector('.workspace-tab')!), 'border')).toBeUndefined()
    expect(lastValue(declarations, 'border-radius')).toBe('var(--surface-radius)')
    expect(lastValue(declarations, 'background-color')).toBe('var(--surface-fill)')
    expect(lastValue(declarationsFor(container.querySelector('.surface-control')!), 'box-shadow')).toContain('inset 0 1px 0 var(--surface-glint)')
    container.remove()
  })
})

describe('rounded card surfaces', () => {
  // User requirement: popup and modal paints must retain rounded corners, with
  // the same geometry in light/dark and no clipping of descendant overlays.
  it('gives standalone cards and hover panels explicit shared shapes', () => {
    const card = fixture('<div class="surface-card"></div>')
    const panel = fixture('<div class="surface-panel rounded"></div>')
    document.body.append(card, panel)
    expect(lastValue(declarationsFor(card), 'border-radius')).toBe('var(--surface-radius)')
    expect(lastValue(declarationsFor(panel), 'border-radius')).toBe('var(--surface-radius-panel)')
    card.remove()
    panel.remove()
    expect(lastValue(declarationsFor(document.documentElement), '--surface-radius-panel')).toBe('16px')
    expect(lastValue(declarationsFor(document.documentElement), '--surface-radius-item')).toBe('10px')
  })

  it.each(['modal', 'dialog', 'card'])('%s header and footer paints follow their parent corners', type => {
    const element = fixture(`<div class="${type}-content"><div class="${type}-header"></div><div class="${type}-body"></div><div class="${type}-footer"></div></div>`)
    const header = declarationsFor(element.querySelector(`.${type}-header`)!)
    const footer = declarationsFor(element.querySelector(`.${type}-footer`)!)
    for (const corner of ['left', 'right']) {
      expect(lastValue(header, `border-top-${corner}-radius`)).toBe('inherit')
      expect(lastValue(footer, `border-bottom-${corner}-radius`)).toBe('inherit')
    }
  })
})
