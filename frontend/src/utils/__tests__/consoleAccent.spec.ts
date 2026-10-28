import { describe, expect, it } from 'vitest'
import { consolePrimaryColors } from '../consoleAccent'

// Requirement: primary labels remain readable for any valid configured accent.
// Expected black/white choices follow WCAG's relative-luminance/contrast formula:
// https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html
// Fixtures list independent known colors and literal palette stops; expected
// results are never derived by calling the palette/foreground implementation.
describe('primary button colors', () => {
  it.each([
    { accent: '#ffffff', foreground: '#000000', end: '255 255 255' },
    { accent: '#ffff00', foreground: '#000000', end: '255 255 82' },
    { accent: '#000000', foreground: '#ffffff', end: '0 0 0' },
    { accent: '#000080', foreground: '#ffffff', end: '0 0 105' },
    { accent: '#14b8a6', foreground: '#000000', end: '45 212 191' },
    { accent: '#757575', foreground: '#ffffff', end: '96 96 96' },
    { accent: '#767676', foreground: '#000000', end: '162 162 162' },
    { accent: '#0000FF', foreground: '#ffffff', end: '0 0 209' },
  ])('uses a readable foreground and safe gradient direction for $accent', ({ accent, foreground, end }) => {
    expect(consolePrimaryColors(accent)).toEqual({ foreground, end })
  })

  // Literal choices include cases whose readable color reverses when the
  // accent becomes translucent. They use the documented light/dark surfaces.
  it.each([
    { accent: '#ffffff', dark: false, foreground: '#000000', end: '255 255 255' },
    { accent: '#ffffff', dark: true, foreground: '#000000', end: '255 255 255' },
    { accent: '#ffff00', dark: false, foreground: '#000000', end: '255 255 82' },
    { accent: '#ffff00', dark: true, foreground: '#000000', end: '255 255 82' },
    { accent: '#000000', dark: false, foreground: '#000000', end: '82 82 82' },
    { accent: '#000000', dark: true, foreground: '#ffffff', end: '0 0 0' },
    { accent: '#000080', dark: false, foreground: '#000000', end: '82 82 169' },
    { accent: '#000080', dark: true, foreground: '#ffffff', end: '0 0 105' },
    { accent: '#14b8a6', dark: false, foreground: '#000000', end: '45 212 191' },
    { accent: '#14b8a6', dark: true, foreground: '#ffffff', end: '13 148 136' },
  ])('keeps labels opaque at 50% alpha for $accent (dark: $dark)', ({ accent, dark, foreground, end }) => {
    expect(consolePrimaryColors(accent, { alpha: .5, dark })).toEqual({ foreground, end })
  })

  it.each(['#ffffff', '#ffff00', '#000000', '#000080', '#14b8a6'])(
    'uses the theme surface when %s becomes fully transparent', accent => {
      expect(consolePrimaryColors(accent, { alpha: 0 }).foreground).toBe('#000000')
      expect(consolePrimaryColors(accent, { alpha: 0, dark: true }).foreground).toBe('#ffffff')
    },
  )

  it.each([null, undefined, '', 'white', '#fff', '#12zz89', 'url(javascript:invalid)', 123])(
    'falls back atomically to the default accent for %s', value => {
      expect(consolePrimaryColors(value)).toEqual({ foreground: '#000000', end: '45 212 191' })
    },
  )
})
