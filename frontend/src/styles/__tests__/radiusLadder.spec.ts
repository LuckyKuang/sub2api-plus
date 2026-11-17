import { readFileSync, readdirSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { describe, expect, it } from 'vitest'

// Requirement: every rounded element in the console takes one step of the
// radius ladder (container 16 > card 12 > control 8 > tag 6 > xs 4); fully
// round shapes are reserved for avatars, dots, switches, tracks and spinners,
// never for a box of text.
const sourceRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

function sourceFiles(directory = sourceRoot, found: string[] = []): string[] {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== '__tests__') sourceFiles(path, found)
    } else if (/\.(vue|ts|css)$/.test(entry.name)) {
      found.push(path)
    }
  }
  return found
}

const files = sourceFiles().map(path => ({ path: relative(sourceRoot, path), text: readFileSync(path, 'utf8') }))

// Static class attributes, string literals and @apply lists that may hold utility classes.
function classLists(text: string): string[][] {
  const lists: string[][] = []
  for (const match of text.matchAll(/class="([^"]*)"|'([^'\n]*)'|`([^`]*)`|@apply ([^;]+);/g)) {
    const body = match[1] ?? match[2] ?? match[3] ?? match[4] ?? ''
    if (body.includes('rounded')) lists.push(body.split(/\s+/).filter(Boolean))
  }
  return lists
}

const base = (token: string) => token.split(':').pop() ?? token

function isSquare(tokens: string[]): boolean {
  if (tokens.some(token => /^size-/.test(base(token)))) return true
  const height = tokens.map(base).find(token => /^h-[\d.]+$/.test(token))?.slice(2)
  const width = tokens.map(base).find(token => /^w-[\d.]+$/.test(token))?.slice(2)
  return height !== undefined && height === width
}

describe('radius ladder', () => {
  it('maps every Tailwind rounded utility onto a ladder token', async () => {
    const configUrl = pathToFileURL(resolve(sourceRoot, '../tailwind.config.js')).href
    const tailwindConfig = (await import(/* @vite-ignore */ configUrl)).default as {
      theme: { borderRadius: Record<string, string>; extend: Record<string, unknown> }
    }
    expect(tailwindConfig.theme.borderRadius).toEqual({
      none: '0px',
      sm: 'var(--surface-radius-xs)',
      DEFAULT: 'var(--surface-radius-xs)',
      md: 'var(--surface-radius-tag)',
      lg: 'var(--surface-radius-control)',
      xl: 'var(--surface-radius-card)',
      '2xl': 'var(--surface-radius)',
      full: '9999px',
    })
    expect(tailwindConfig.theme.extend).not.toHaveProperty('borderRadius')
  })

  it('never puts text in a capsule', () => {
    const capsules: string[] = []
    for (const file of files) {
      for (const tokens of classLists(file.text)) {
        const round = tokens.some(token => base(token) === 'rounded-full')
        const padded = tokens.some(token => /^(px|pl|pr)-/.test(base(token)))
        if (round && padded && !isSquare(tokens)) capsules.push(`${file.path}: ${tokens.join(' ')}`)
      }
    }
    expect(capsules).toEqual([])
  })

  it('leaves the container radius to shared container roles', () => {
    // Pages and components pick card, control, tag or xs utilities; zone
    // containers, dialogs and shell chrome take the container radius from
    // their shared classes. Arbitrary radius lengths bypass the ladder.
    const offenders: string[] = []
    for (const file of files) {
      for (const tokens of classLists(file.text)) {
        for (const token of tokens) {
          if (/^rounded(-[trblse]{1,2})?-(2xl|3xl|4xl)$/.test(base(token)) || /^rounded(-[trblse]{1,2})?-\[(?!var\()/.test(base(token))) {
            offenders.push(`${file.path}: ${token}`)
          }
        }
      }
    }
    expect(offenders).toEqual([])
  })

  it('declares stylesheet radii only through ladder tokens', () => {
    // Split a shorthand into its corner values, keeping calc() expressions whole.
    const corners = (value: string) => {
      const parts: string[] = []
      let depth = 0
      let current = ''
      for (const character of value) {
        if (character === '(') depth += 1
        if (character === ')') depth -= 1
        if (/\s/.test(character) && depth === 0) {
          if (current) parts.push(current)
          current = ''
        } else current += character
      }
      if (current) parts.push(current)
      return parts
    }
    const allowed = (part: string) => /^(0|inherit|9999px|50%)$/.test(part)
      || /^var\(--(surface-radius(-[a-z]+)?|record-row-radius)\)$/.test(part)
      || /^calc\(var\(--(surface-radius|record-row)[^]*\)$/.test(part)
    const offenders: string[] = []
    for (const file of files) {
      if (file.path.endsWith('.ts')) continue
      for (const match of file.text.matchAll(/border(?:-(?:top|bottom)-(?:left|right))?-radius:\s*([^;}]+?)\s*(?:!important)?\s*[;}]/g)) {
        const value = match[1]!.trim()
        if (!corners(value).every(allowed)) offenders.push(`${file.path}: ${value}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
