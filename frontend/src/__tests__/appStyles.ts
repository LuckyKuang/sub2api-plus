import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'

function appStylesCachePath(): string {
  return resolve(process.cwd(), 'node_modules/.cache/sub2api-app-styles/app-styles.css')
}

// Compiling the real application stylesheet runs Tailwind over the whole source
// tree. Layout suites share one compiled result per run instead of each worker
// rebuilding it inside a beforeAll hook, where parallel Go work already consumes
// the validation container's bounded CPU budget.
export async function compileAppStyles(): Promise<string> {
  const from = resolve(process.cwd(), 'src/style.css')
  const config = resolve(process.cwd(), 'tailwind.config.js')
  const result = await postcss([tailwindcss({ config })]).process(
    readFileSync(from, 'utf8'), { from },
  )
  return result.css
}

export async function ensureAppStylesCache(): Promise<string> {
  const css = await compileAppStyles()
  const cache = appStylesCachePath()
  mkdirSync(dirname(cache), { recursive: true })
  const temporary = `${cache}.${process.pid}.tmp`
  writeFileSync(temporary, css)
  renameSync(temporary, cache)
  return css
}

function readAppStylesCache(): string | undefined {
  try {
    const css = readFileSync(appStylesCachePath(), 'utf8')
    return css === '' ? undefined : css
  } catch {
    return undefined
  }
}

// Use the real application stylesheet/configuration, not a mock of utility
// classes. jsdom can verify computed declarations, but not pixel geometry.
export async function installAppStyles(): Promise<() => void> {
  const css = readAppStylesCache() ?? (await ensureAppStylesCache())
  const style = document.createElement('style')
  style.textContent = css
  document.head.appendChild(style)
  return () => style.remove()
}

export function cssPixels(value: string): number {
  if (value === '' || value === '0') return 0
  if (value.endsWith('px')) return Number.parseFloat(value)
  if (value.endsWith('rem')) {
    const rootSize = Number.parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    return Number.parseFloat(value) * rootSize
  }
  throw new Error(`Unsupported CSS length: ${value}`)
}
