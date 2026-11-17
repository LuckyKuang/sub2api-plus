import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss, { type Root } from 'postcss'

// Requirement: controls that share a row line up. Buttons, fields, select and
// date triggers, tabs and pagers read one height per size and the control
// radius; status labels are 20px tags, never capsules.
const sourceRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const parse = (path: string) => postcss.parse(readFileSync(resolve(sourceRoot, path), 'utf8'))
const sheets: Record<string, Root> = {
  style: parse('style.css'),
  console: parse('styles/console.css'),
  tables: parse('styles/tables.css'),
  workspace: parse('styles/workspace.css'),
  datePicker: postcss.parse(readFileSync(resolve(sourceRoot, 'components/common/DateRangePicker.vue'), 'utf8').split('<style scoped>')[1]!.split('</style>')[0]!),
}

function declarations(sheet: Root, selector: string): Map<string, string> {
  const found = new Map<string, string>()
  sheet.walkRules(rule => {
    if (rule.selectors.includes(selector)) rule.walkDecls(declaration => { found.set(declaration.prop, declaration.value) })
  })
  return found
}

describe('shared control sizes', () => {
  it('declares one height per control size', () => {
    const root = declarations(sheets.console, ':root')
    expect(root.get('--control-height')).toBe('36px')
    expect(root.get('--control-height-sm')).toBe('32px')
    expect(root.get('--control-height-xs')).toBe('26px')
    expect(root.get('--control-height-lg')).toBe('44px')
  })

  it.each([
    ['style', '.btn', 'var(--control-height)', 'var(--surface-radius-control)'],
    ['style', '.btn-sm', 'var(--control-height-sm)', 'var(--surface-radius-control)'],
    ['style', '.btn-xs', 'var(--control-height-xs)', 'var(--surface-radius-tag)'],
    ['style', '.input', 'var(--control-height)', 'var(--surface-radius-control)'],
    ['style', '.select-trigger', 'var(--control-height)', 'var(--surface-radius-control)'],
    ['datePicker', '.date-picker-trigger', 'var(--control-height)', 'var(--surface-radius-control)'],
    ['workspace', '.workspace-tab', 'var(--control-height)', 'var(--surface-radius-control)'],
  ] as const)('%s %s takes the shared height and control radius', (sheet, selector, height, radius) => {
    const paint = declarations(sheets[sheet], selector)
    expect(paint.get('min-height')).toBe(height)
    expect(paint.get('border-radius')).toBe(radius)
  })

  it('sizes the large call-to-action button from the large control height', () => {
    expect(declarations(sheets.style, '.btn-lg').get('min-height')).toBe('var(--control-height-lg)')
    // Size modifiers never re-introduce their own corner.
    for (const modifier of ['.btn-md', '.btn-lg', '.btn-icon']) {
      expect(declarations(sheets.style, modifier).get('border-radius'), modifier).toBeUndefined()
    }
  })

  it('breaks button labels only between words and never lets them overflow', () => {
    const button = declarations(sheets.style, '.btn')
    expect(button.get('word-break')).toBe('keep-all')
    expect(button.get('overflow-wrap')).toBe('break-word')
    expect(button.get('white-space')).toBeUndefined()
    expect(button.get('max-width')).toBe('100%')
  })

  it('renders status labels as 20px tags', () => {
    const badge = declarations(sheets.style, '.badge')
    expect(badge.get('border-radius')).toBe('var(--surface-radius-tag)')
    expect(badge.get('padding')).toBe('2px 8px')
    expect(badge.get('line-height')).toBe('16px')
    expect(badge.get('font-size')).toBe('12px')
  })

  it('gives every pager control the small control height with only the current page tinted', () => {
    const button = declarations(sheets.tables, '.pagination-button')
    expect(button.get('height')).toBe('var(--control-height-sm)')
    expect(button.get('min-width')).toBe('var(--control-height-sm)')
    expect(button.get('border-radius')).toBe('var(--surface-radius-control)')
    expect(button.get('background')).toBe('transparent')
    const current = declarations(sheets.tables, '.pagination-button--current')
    expect(current.get('background-color')).toContain('--console-accent-500')
  })

  it('shapes row action menus like dropdowns', () => {
    const menu = declarations(sheets.tables, '.action-menu-content')
    expect(menu.get('border-radius')).toBe('var(--surface-radius-panel)')
    expect(menu.get('padding')).toBe('4px')
    expect(declarations(sheets.tables, '.action-menu-item').get('min-height')).toBe('var(--control-height-sm)')
  })
})
