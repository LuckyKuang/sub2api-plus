import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import postcss, { type Root } from 'postcss'
import consoleSurfaces from '../../../scripts/consoleSurfaces.js'

const onboardingStyles = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../onboarding.css'), 'utf8')

// Requirements: docs/UI_INTERACTION.md. Global transparency changes only
// surface fills; outlines, text, images and color-picker references stay clear.
async function transform(source: string): Promise<Root> {
  const result = await postcss([consoleSurfaces()]).process(source, { from: undefined })
  return result.root
}
function value(root: Root, selector: string, property: string): string | undefined {
  let output: string | undefined
  root.walkRules(selector, rule => {
    rule.walkDecls(property, decl => { output = decl.value.trim() })
  })
  return output
}

const alpha = 'calc(var(--console-card-alpha, 1) * 100%)'

describe('build-time surface transparency', () => {
  it('fades surface fills once without changing borders, text or DOM opacity', async () => {
    const root = await transform('.card { background-color: #fff; color: #fff; border: 1px solid #fff; opacity: .8; box-shadow: inset 0 0 0 1px #fff }')
    expect(value(root, '.card', 'background-color')).toBe(`color-mix(in srgb, #fff ${alpha}, transparent)`)
    expect(value(root, '.card', 'color')).toBe('#fff')
    expect(value(root, '.card', 'border')).toBe('1px solid #fff')
    expect(value(root, '.card', 'opacity')).toBe('.8')
    expect(value(root, '.card', 'box-shadow')).toBe('inset 0 0 0 1px #fff')
  })

  it('preserves transparent surface tokens and already bound alpha without applying a second fade', async () => {
    const source = '.card { background: var(--surface-fill) } .cell { background: var(--record-row-bg, var(--surface-soft)) } .tab { background: rgb(var(--console-accent-500) / calc(.24 * var(--console-card-alpha, 1))) }'
    const root = await transform(source)
    expect(value(root, '.card', 'background')).toBe('var(--surface-fill)')
    expect(value(root, '.cell', 'background')).toBe('var(--record-row-bg, var(--surface-soft))')
    expect(value(root, '.tab', 'background')).toBe('rgb(var(--console-accent-500) / calc(.24 * var(--console-card-alpha, 1)))')
  })

  it('fades gradients and independent colors inside mixes while retaining images and positions', async () => {
    const root = await transform('.card { background: url("/icons/white-#fff.png") left/cover no-repeat, linear-gradient(135deg, rgba(20,184,166,.2), white 65%); background-color: color-mix(in srgb, var(--surface-panel) 80%, #fff) }')
    expect(value(root, '.card', 'background')).toBe(`url("/icons/white-#fff.png") left/cover no-repeat, linear-gradient(135deg, color-mix(in srgb, rgba(20,184,166,.2) ${alpha}, transparent), color-mix(in srgb, white ${alpha}, transparent) 65%)`)
    expect(value(root, '.card', 'background-color')).toBe(`color-mix(in srgb, var(--surface-panel) 80%, color-mix(in srgb, #fff ${alpha}, transparent))`)
  })

  it('keeps quoted and unquoted image URLs, escaped parentheses and image-valued variables valid', async () => {
    const root = await transform(String.raw`.image { background-image: var(--custom-image); background: url(/images/a\(white\).png) center/cover no-repeat #ff00ff; content: "white #fff" }`)
    expect(value(root, '.image', 'background-image')).toBe('var(--custom-image)')
    expect(value(root, '.image', 'background')).toBe(String.raw`url(/images/a\(white\).png) center/cover no-repeat color-mix(in srgb, #ff00ff ${alpha}, transparent)`)
    expect(value(root, '.image', 'content')).toBe('"white #fff"')
  })

  it('protects canvas backgrounds, QR quiet zones, preset swatches and gradient text', async () => {
    const source = '.app-layout::before { background: radial-gradient(circle, white, #fff) } .surface-opaque { background: white } .appearance-accent-swatch { background: #f59e0b } .appearance-preview-light { background: #fff } .text-gradient { --tw-gradient-from: #14b8a6; background-image: linear-gradient(white, black) } .heading { background-image: linear-gradient(white, black); background-clip: text }'
    const root = await transform(source)
    expect(value(root, '.app-layout::before', 'background')).toBe('radial-gradient(circle, white, #fff)')
    expect(value(root, '.surface-opaque', 'background')).toBe('white')
    expect(value(root, '.appearance-accent-swatch', 'background')).toBe('#f59e0b')
    expect(value(root, '.appearance-preview-light', 'background')).toBe('#fff')
    expect(value(root, '.text-gradient', '--tw-gradient-from')).toBe('#14b8a6')
    expect(value(root, '.text-gradient', 'background-image')).toBe('linear-gradient(white, black)')
    expect(value(root, '.heading', 'background-image')).toBe('linear-gradient(white, black)')
  })

  it('fades all generated gradient stops while keeping the same utility usable for visible text', async () => {
    const root = await transform('.from-primary-500 { --tw-gradient-from: rgb(20 184 166) var(--tw-gradient-from-position); --tw-gradient-to: rgb(20 184 166 / 0) var(--tw-gradient-to-position) } .via-white { --tw-gradient-stops: var(--tw-gradient-from), white var(--tw-gradient-via-position), var(--tw-gradient-to) }')
    expect(value(root, '.from-primary-500', '--tw-gradient-from')).toBe(`color-mix(in srgb, rgb(20 184 166) ${alpha}, transparent) var(--tw-gradient-from-position)`)
    expect(value(root, ':is(.from-primary-500).bg-clip-text', '--tw-gradient-from')).toBe('rgb(20 184 166) var(--tw-gradient-from-position)')
    expect(value(root, '.via-white', '--tw-gradient-stops')).toBe(`var(--tw-gradient-from), color-mix(in srgb, white ${alpha}, transparent) var(--tw-gradient-via-position), var(--tw-gradient-to)`)
    expect(value(root, ':is(.via-white).bg-clip-text', '--tw-gradient-stops')).toBe('var(--tw-gradient-from), white var(--tw-gradient-via-position), var(--tw-gradient-to)')
  })

  it('handles named colors, nested modern color functions and local color variables', async () => {
    const root = await transform('.card { background-color: var(--chip-accent); background: linear-gradient(rebeccapurple, color-mix(in oklab, red 40%, color(display-p3 1 0 0))) }')
    expect(value(root, '.card', 'background-color')).toBe(`color-mix(in srgb, var(--chip-accent) ${alpha}, transparent)`)
    expect(value(root, '.card', 'background')).toBe(`linear-gradient(color-mix(in srgb, rebeccapurple ${alpha}, transparent), color-mix(in srgb, color-mix(in oklab, red 40%, color(display-p3 1 0 0)) ${alpha}, transparent))`)
  })

  it('retains monitor history, health matrix colors and loading indicators as visible data', async () => {
    const root = await transform('.history-bar { background: var(--status-color) } .legend-dot.status-unknown { background: #a3a3a34d } .health-score10 { background: #16a34a } .health-unknown { background: #9ca3af } .score-legend { background: linear-gradient(red, green) } .navigation-progress-bar { background: linear-gradient(transparent, #14b8a6) } .card { background: white }')
    expect(value(root, '.history-bar', 'background')).toBe('var(--status-color)')
    expect(value(root, '.legend-dot.status-unknown', 'background')).toBe('#a3a3a34d')
    expect(value(root, '.health-score10', 'background')).toBe('#16a34a')
    expect(value(root, '.health-unknown', 'background')).toBe('#9ca3af')
    expect(value(root, '.score-legend', 'background')).toBe('linear-gradient(red, green)')
    expect(value(root, '.navigation-progress-bar', 'background')).toBe('linear-gradient(transparent, #14b8a6)')
    expect(value(root, '.card', 'background')).toBe(`color-mix(in srgb, white ${alpha}, transparent)`)
  })

  it('keeps progress fills and selection indicators visible while ordinary uses of their generated background colors fade', async () => {
    const root = await transform('.bg-primary-500 { background-color: rgb(20 184 166) } .bg-white { background-color: #fff } .from-blue-500 { --tw-gradient-from: #3b82f6 } .console-data-mark { background: white }')
    expect(value(root, '.bg-primary-500', 'background-color')).toBe(`color-mix(in srgb, rgb(20 184 166) ${alpha}, transparent)`)
    expect(value(root, ':is(.bg-primary-500).console-data-mark', 'background-color')).toBe('rgb(20 184 166)')
    expect(value(root, '.bg-white', 'background-color')).toBe(`color-mix(in srgb, #fff ${alpha}, transparent)`)
    expect(value(root, ':is(.bg-white).console-data-mark', 'background-color')).toBe('#fff')
    expect(value(root, ':is(.from-blue-500).console-data-mark', '--tw-gradient-from')).toBe('#3b82f6')
    expect(value(root, '.console-data-mark', 'background')).toBe('white')
  })

  it('gives inline localized tour help shared transparent fills and a complete thin rounded outline', async () => {
    const root = await transform(onboardingStyles)
    const selector = '.theme-tour-popover .driver-popover-description :is(div, p)[style*="background"]'
    expect(value(root, selector, 'background')).toBe('var(--surface-soft)')
    expect(value(root, selector, 'border')).toBe('1px solid var(--surface-edge)')
    expect(value(root, selector, 'border-radius')).toBe('var(--surface-radius-item)')
  })

  it('keeps tour actions in the selected accent with a visible outline when fills become transparent', async () => {
    const root = await transform(onboardingStyles)
    const actionSelector = '.theme-tour-popover .driver-popover-description [style*="color: #10b981"]'
    expect(value(root, actionSelector, 'color')).toBe('rgb(var(--console-accent-600, 13 148 136))')
    expect(value(root, `.dark ${actionSelector}`, 'color')).toBe('rgb(var(--console-accent-400, 45 212 191))')
    expect(value(root, '.theme-tour-popover .driver-popover-next-btn', 'border-color')).toBe('rgb(var(--console-accent-500, 20 184 166) / 65%)')
    expect(value(root, '.theme-tour-popover .driver-popover-next-btn', 'color')).toBe('var(--surface-text)')
  })

  it('is idempotent when a stylesheet passes through the plugin a second time', async () => {
    const first = await transform('.card { background: linear-gradient(white, #fff) }')
    const second = await transform(first.toString())
    expect(second.toString()).toBe(first.toString())
  })

  it('keeps scrollbar tracks and draggable thumbs visible independently of card transparency', async () => {
    const root = await transform('.table-wrapper::-webkit-scrollbar-track { background-color: rgba(0, 0, 0, 0.03) } .table-wrapper::-webkit-scrollbar-thumb { background-color: rgba(115, 115, 115, 0.75); border: 2px solid transparent } .table-wrapper::-webkit-scrollbar-thumb:hover { background-color: rgba(82, 82, 82, 0.9) } .dark .table-wrapper::-webkit-scrollbar-thumb { background-color: rgba(163, 163, 163, 0.75) } .card { background-color: white }')
    expect(value(root, '.table-wrapper::-webkit-scrollbar-track', 'background-color')).toBe('rgba(0, 0, 0, 0.03)')
    expect(value(root, '.table-wrapper::-webkit-scrollbar-thumb', 'background-color')).toBe('rgba(115, 115, 115, 0.75)')
    expect(value(root, '.table-wrapper::-webkit-scrollbar-thumb', 'border')).toBe('2px solid transparent')
    expect(value(root, '.table-wrapper::-webkit-scrollbar-thumb:hover', 'background-color')).toBe('rgba(82, 82, 82, 0.9)')
    expect(value(root, '.dark .table-wrapper::-webkit-scrollbar-thumb', 'background-color')).toBe('rgba(163, 163, 163, 0.75)')
    expect(value(root, '.card', 'background-color')).toBe(`color-mix(in srgb, white ${alpha}, transparent)`)
  })
})
