import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'

const stylesheet = postcss.parse(readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../console.css'), 'utf8'))
const surfaces = ['card', 'glass', 'glass-card', 'card-glass', 'glass-surface', 'shell-surface', 'modal-content', 'dialog-container', 'dropdown']

describe('shared surface overlay safety', () => {
  it('keeps surface positioning below component and utility positioning after important variants are built', async () => {
    // The toolbar must stay sticky and menus/dialogs keep their explicit positioning,
    // even when unrelated cards or dropdowns request Tailwind's important variants.
    const compiled = await postcss([tailwindcss({
      content: [{ raw: `${surfaces.join(' ')} app-header fixed absolute !card !dropdown`, extension: 'html' }],
    })]).process(`@tailwind components;\n@tailwind utilities;\n${stylesheet.toString()}`, { from: undefined })
    const rules = postcss.parse(compiled.css)
    for (const classes of ['app-header shell-surface', 'card fixed', 'dropdown absolute', 'dialog-container fixed']) {
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

  // UI requirement: inline menus can overlap later cards and fixed dialogs remain viewport-bound.
  it.each(surfaces)('%s does not trap descendant overlays in a blur stacking context', surface => {
    let found = false
    stylesheet.walkRules(rule => {
      if (!rule.selectors.includes(`.${surface}`)) return
      found = true
      rule.walkDecls(declaration => {
        if (['backdrop-filter', '-webkit-backdrop-filter', 'filter', 'transform', 'perspective', 'contain'].includes(declaration.prop)) {
          expect(declaration.value).toBe('none')
        }
        if (declaration.prop === 'isolation') expect(declaration.value).toBe('auto')
        if (declaration.prop === 'z-index') expect(declaration.value).toBe('auto')
        if (declaration.prop === 'overflow') expect(declaration.value).toBe('visible')
      })
    })
    expect(found).toBe(true)
  })
})
