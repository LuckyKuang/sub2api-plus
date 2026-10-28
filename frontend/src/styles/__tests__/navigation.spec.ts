import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import postcss, { type Rule } from 'postcss'
import { describe, expect, it } from 'vitest'

const styles = postcss.parse(['console.css', 'workspace.css', 'tabs.css']
  .map(file => readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', file), 'utf8'))
  .join('\n'))

function rule(selector: string): Rule {
  let found: Rule | undefined
  styles.walkRules(selector, candidate => {
    let parent = candidate.parent
    while (parent) {
      if (parent.type === 'atrule' && parent.name === 'media') return
      parent = parent.parent
    }
    found = candidate
  })
  if (!found) throw new Error(`Missing navigation rule: ${selector}`)
  return found
}

function value(selector: string, property: string): string | undefined {
  let result: string | undefined
  rule(selector).walkDecls(property, declaration => { result = declaration.value })
  return result
}

// Reference: the approved Chrome styling in the local UI unification plan.
// Keep navigation shells transparent, compact tabs accessible, and the user's
// configured accent/transparency independent of foreground readability.
describe('lightweight shared navigation', () => {
  it.each([':root :is(.workspace-tabs, .pagination-card)', ':root .card-tabs'])('%s removes only the navigation frame', selector => {
    expect(value(selector, 'background-color')).toBe('transparent')
    expect(value(selector, 'background-image')).toBe('none')
    expect(value(selector, 'border')).toBe('0')
    expect(value(selector, 'box-shadow')).toBe('none')
    expect(value(selector, 'border-radius')).toBe('0')
    const card = document.createElement('article')
    card.className = 'card surface-card'
    expect(card.matches(selector)).toBe(false)
    const panel = document.createElement('div')
    panel.className = 'surface-panel'
    expect(panel.matches(selector)).toBe(false)
    rule(selector).walkDecls(declaration => { expect(declaration.important).toBeFalsy() })
  })

  it.each(['.workspace-tab', '.card-tab'])('%s uses the reference compact shape with transparent inactive paint', selector => {
    expect(value(selector, 'border-radius')).toBe('9px')
    expect(value(selector, 'min-height')).toBe('38px')
    expect(value(selector, 'font-size')).toBe('13.5px')
    expect(value(selector, 'font-weight')).toBe('600')
    expect(value(selector, 'border')).toBe('1px solid transparent')
    expect(value(selector, 'background-color')).toBe('transparent')
    expect(value(selector, 'box-shadow')).toBe('none')
    expect(value(selector, 'color')).toBe('var(--surface-muted)')
    expect(value(selector, 'height')).toBeUndefined()
  })

  it.each(['.workspace-tab', '.card-tab'])('%s keeps the inactive border transparent above theme base styles', selector => {
    const inactive = `${selector}[aria-selected="false"]`
    expect(value(inactive, 'border-color')).toBe('transparent')
    rule(inactive).walkDecls(declaration => { expect(declaration.important).toBeFalsy() })
    const tab = document.createElement('button')
    tab.className = selector.slice(1)
    tab.setAttribute('aria-selected', 'false')
    expect(tab.matches(inactive)).toBe(true)
    tab.setAttribute('aria-selected', 'true')
    expect(tab.matches(inactive)).toBe(false)
    expect(value(`${selector}[aria-selected="true"]`, 'border-color')).toBe('rgb(var(--console-accent-500, 20 184 166) / 45%)')
  })

  it('keeps long workspace tabs scrollable and long card-tab labels able to wrap', () => {
    expect(value('.workspace-tabs', 'overflow-x')).toBe('auto')
    expect(value('.workspace-tabs', 'padding')).toBe('4px')
    expect(value('.workspace-tab', 'white-space')).toBe('nowrap')
    expect(value('.workspace-tab', 'min-width')).toBe('max-content')
    expect(value(':where(.card-tabs)', 'flex-wrap')).toBe('wrap')
    expect(value(':where(.card-tabs)', 'padding')).toBe('4px')
    expect(value('.card-tab', 'max-width')).toBe('100%')
    expect(value('.card-tab', 'white-space')).toBe('normal')
    expect(value('.card-tab', 'overflow-wrap')).toBe('anywhere')
    for (const selector of ['.workspace-tabs', '.workspace-tab', ':where(.card-tabs)', '.card-tab']) {
      expect(value(selector, 'overflow')).not.toBe('hidden')
      expect(value(selector, 'overflow-y')).not.toBe('hidden')
    }
  })

  it.each(['.workspace-tab', '.card-tab'])('%s retains the selected accent and alpha without fading text or the outline', selector => {
    const selected = `${selector}[aria-selected="true"]`
    const fill = value(selected, selector === '.workspace-tab' ? 'background' : 'background-color')
    expect(fill).toBe('rgb(var(--console-accent-500, 20 184 166) / calc(.18 * var(--console-card-alpha, 1)))')
    expect(value(selected, 'border-color')).toBe('rgb(var(--console-accent-500, 20 184 166) / 45%)')
    expect(value(selected, 'color')).toBe('var(--console-accent-readable, var(--surface-text))')
    expect(value(selected, 'opacity')).toBeUndefined()
    expect(value(`${selector}:focus-visible`, 'outline')).toContain('--console-accent-500')
    expect(value(`${selector}:disabled`, 'cursor')).toBe('not-allowed')
  })

  it('removes card paint from sidebar links and footer controls without removing active or keyboard feedback', () => {
    const selector = '.sidebar .sidebar-link'
    expect(value(selector, 'border-radius')).toBe('8px')
    expect(value(selector, 'border')).toBe('0')
    expect(value(selector, 'background-color')).toBe('transparent')
    expect(value(selector, 'background-image')).toBe('none')
    expect(value(selector, 'box-shadow')).toBe('none')
    expect(value(selector, 'color')).toBe('var(--surface-muted)')
    expect(value('.sidebar-link', 'min-height')).toBe('44px')
    expect(value('.sidebar-nav .sidebar-link-active', 'background-color')).toBe('rgb(var(--console-accent-500, 20 184 166) / calc(.12 * var(--console-card-alpha, 1)))')
    expect(value('.sidebar-nav .sidebar-link-active', 'font-weight')).toBe('600')
    expect(value('.sidebar .sidebar-link:focus-visible', 'outline')).toContain('--console-accent-500')
    const container = document.createElement('aside')
    container.className = 'sidebar'
    container.innerHTML = '<nav class="sidebar-nav"><a class="sidebar-link"></a></nav><footer class="sidebar-footer"><button class="sidebar-link surface-control"></button></footer><a class="sidebar-brand-title"></a>'
    expect(container.querySelectorAll(selector)).toHaveLength(2)
    expect(container.querySelector('.sidebar-brand-title')!.matches(selector)).toBe(false)
    rule(selector).walkDecls(declaration => { expect(declaration.important).toBeFalsy() })
  })
})
