import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss from 'postcss'

// Every stylesheet that owns shell geometry, in the application's import order.
const files = ['console.css', 'surfaces.css', 'tables.css', 'tableHeaders.css', 'workspace.css', 'product.css']
const stylesheet = postcss.parse(files
  .map(file => readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', file), 'utf8'))
  .join('\n'))

const ZONE_CONTAINERS = [
  '.workspace-page', '.workspace-fixed', '.workspace-scroll', '.workspace-stack',
  '.workspace-table-card', '.workspace-shell', '.workspace-clip', '.table-scroll-container',
  '.layout-section-fixed', '.layout-section-scrollable', '.pagination-card',
  '.table-page-layout', '.app-main', '.app-page',
]

interface Entry {
  selector: string
  value: string
  important: boolean
}

/** Every declaration of `property` from rules whose selector names `className`. */
function entries(className: string, property: string): Entry[] {
  const found: Entry[] = []
  stylesheet.walkRules(rule => {
    if (!rule.selectors.some(selector => selector.includes(className))) return
    rule.walkDecls(declaration => {
      if (declaration.prop === property) {
        found.push({ selector: rule.selector, value: declaration.value, important: declaration.important })
      }
    })
  })
  return found
}

function last(className: string, property: string): Entry | undefined {
  return entries(className, property).slice(-1)[0]
}

/** The declaration from a rule whose selector is exactly `selector`. */
function exact(selector: string, property: string): Entry | undefined {
  let found: Entry | undefined
  stylesheet.walkRules(rule => {
    if (!rule.selectors.includes(selector)) return
    rule.walkDecls(declaration => {
      if (declaration.prop === property) found = { selector: rule.selector, value: declaration.value, important: declaration.important }
    })
  })
  return found
}

function tokens(): Map<string, string> {
  const found = new Map<string, string>()
  stylesheet.walkDecls(declaration => {
    if (declaration.prop.startsWith('--surface-radius') && declaration.parent?.selector?.includes(':root')) {
      found.set(declaration.prop, declaration.value)
    }
  })
  return found
}

describe('console shell zones', () => {
  it('gives every zone container the shell page radius, never the card radius', () => {
    for (const selector of ZONE_CONTAINERS) {
      const radius = last(selector, 'border-radius')
      expect(radius?.value, selector).toBe('var(--surface-radius)')
    }
  })

  it('never lets a zone container declare a bare radius length', () => {
    // A hardcoded length is how a zone container silently outgrows the box
    // clipping it and gets its corner cut mid-arc.
    for (const selector of ZONE_CONTAINERS) {
      for (const entry of entries(selector, 'border-radius')) {
        expect(/^\d+(\.\d+)?(px|rem|em|%)$/.test(entry.value), `${selector}: ${entry.value}`).toBe(false)
      }
    }
  })

  // Five roles, outermost first; names in one role share one value, and every
  // role is strictly smaller than the one that encloses it.
  it('keeps the radius ladder strictly decreasing by role so no inner curve can be cut mid-arc', () => {
    const declared = tokens()
    const roles = [
      ['--surface-radius'],
      ['--surface-radius-card', '--surface-radius-panel'],
      ['--surface-radius-control', '--surface-radius-item'],
      ['--surface-radius-tag'],
      ['--surface-radius-xs'],
    ].map(names => names.map(token => ({ token, value: parseFloat(declared.get(token) ?? '') })))
    for (const role of roles) {
      for (const step of role) {
        expect(Number.isFinite(step.value), step.token).toBe(true)
        expect(step.value, step.token).toBe(role[0]!.value)
      }
    }
    for (let index = 1; index < roles.length; index += 1) {
      expect(roles[index]![0]!.value, roles[index]![0]!.token).toBeLessThan(roles[index - 1]![0]!.value)
    }
    // One container radius at every viewport: no breakpoint enlarges it.
    stylesheet.walkDecls('--surface-radius', declaration => {
      expect(declaration.value).toBe('12px')
    })
  })

  it('lets the static table frame own every outer corner while its scrollport owns none', () => {
    expect(exact('.static-table-frame', 'border-radius')?.value).toBe('var(--surface-radius)')
    // The scrollport's own radius must be zero, or its arc would exceed the
    // frame's and the visible corner would be cut where the two disagree.
    for (const selector of [':root [data-static-table-body-viewport]', ':root .static-table-frame > [data-static-table-body-viewport]']) {
      const radius = exact(selector, 'border-radius')
      expect(radius?.value, selector).toBe('0')
      expect(radius?.important, selector).toBe(true)
    }
  })

  it('sources every zone gap from the shared token', () => {
    for (const selector of ['.workspace-page', '.workspace-body', '.workspace-scroll', '.workspace-stack']) {
      const gaps = entries(selector, 'gap')
      expect(gaps.length, selector).toBeGreaterThan(0)
      for (const gap of gaps) {
        expect(gap.value, `${selector}: ${gap.selector}`).toBe('var(--shell-zone-gap)')
      }
    }
  })

  it('keeps the admin page full width and caps only the user page', () => {
    expect(last(':root', '--shell-page-width')?.value).toBe('100%')
    // The cap is an opt-in declared on exactly one rule, so the admin console
    // can never inherit a 1600px ceiling on ultra-wide screens.
    const caps: Entry[] = []
    stylesheet.walkRules(rule => {
      rule.walkDecls(declaration => {
        if (declaration.prop === '--shell-page-max-width') {
          caps.push({ selector: rule.selector, value: declaration.value, important: declaration.important })
        }
      })
    })
    expect(caps.map(entry => ({ selector: entry.selector, value: entry.value })))
      .toEqual([{ selector: '.app-layout-user', value: '100rem' }])
    expect(last('.app-layout-user', '--shell-page-width')?.value).toBe('94%')
    // Without an opt-in the page has no cap.
    expect(last('.app-page', 'max-width')?.value).toBe('var(--shell-page-max-width, none)')
  })

  it('no longer declares the dead inset radius token', () => {
    const declared: string[] = []
    stylesheet.walkDecls(declaration => {
      if (declaration.prop === '--surface-radius-inset') declared.push(declaration.prop)
    })
    expect(declared).toEqual([])
  })
})
